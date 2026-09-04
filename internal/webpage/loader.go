package webpage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const BrowserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Browser interface {
	Load(context.Context, string) (string, error)
}

type Loader struct {
	Client  HTTPClient
	Browser Browser
}

func NewLoader(progress io.Writer) *Loader {
	if progress == nil {
		progress = os.Stderr
	}
	return &Loader{
		Client:  &http.Client{},
		Browser: &ChromeBrowser{Progress: progress},
	}
}

func (l *Loader) Load(ctx context.Context, rawURL string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		return "", errors.New("page timeout must be greater than zero")
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create page request: %w", err)
	}
	request.Header.Set("User-Agent", BrowserUserAgent)

	response, err := l.Client.Do(request)
	if err != nil {
		return "", fmt.Errorf("request page: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read page response: %w", err)
	}
	html := string(body)
	if IsDDoSGuardChallenge(html) || isDDoSGuardRedirect(request.URL, response.Request) {
		if l.Browser == nil {
			return "", errors.New("DDoS-Guard verification required but no browser loader is configured")
		}
		browserHTML, browserErr := l.Browser.Load(requestCtx, rawURL)
		if browserErr != nil {
			return "", fmt.Errorf("complete DDoS-Guard verification: %w", browserErr)
		}
		if IsDDoSGuardChallenge(browserHTML) {
			return "", errors.New("DDoS-Guard verification did not complete")
		}
		return browserHTML, nil
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("page request returned HTTP %d", response.StatusCode)
	}
	return html, nil
}

func IsDDoSGuardChallenge(html string) bool {
	lower := strings.ToLower(html)
	return strings.Contains(lower, "<title>ddos-guard") ||
		strings.Contains(lower, "check.ddos-guard.net/check.js") ||
		strings.Contains(lower, "/.well-known/ddos-guard/js-challenge/")
}

func isDDoSGuardRedirect(original *url.URL, finalRequest *http.Request) bool {
	if original == nil || finalRequest == nil || finalRequest.URL == nil || finalRequest.URL.String() == original.String() {
		return false
	}
	final := finalRequest.URL
	host := strings.ToLower(final.Hostname())
	path := strings.ToLower(final.EscapedPath())
	return host == "check.ddos-guard.net" ||
		strings.HasSuffix(host, ".ddos-guard.net") ||
		strings.Contains(path, "/.well-known/ddos-guard/")
}

type ChromeBrowser struct {
	Progress    io.Writer
	ProfilePath func() (string, error)
	DisplayOK   func() bool
}

func (b *ChromeBrowser) Load(ctx context.Context, rawURL string) (string, error) {
	displayOK := b.DisplayOK
	if displayOK == nil {
		displayOK = interactiveDisplayAvailable
	}
	if !displayOK() {
		return "", errors.New("a graphical display is required for interactive browser verification")
	}

	profilePath := b.ProfilePath
	if profilePath == nil {
		profilePath = defaultBrowserProfilePath
	}
	profile, err := prepareBrowserProfile(profilePath)
	if err != nil {
		return "", err
	}

	progress := b.Progress
	if progress == nil {
		progress = os.Stderr
	}
	_, _ = fmt.Fprintln(progress, "Anna's Archive requires browser verification; complete the check in the opened Chrome window.")

	allocatorOptions := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocatorOptions = append(allocatorOptions,
		chromedp.Flag("headless", false),
		chromedp.UserDataDir(profile),
	)
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	return loadVerifiedPage(browserCtx, rawURL)
}

func loadVerifiedPage(browserCtx context.Context, rawURL string) (string, error) {
	// Associate document responses with their frame and navigation, so redirects
	// and failed subresources cannot supply the final page's status.
	type documentResponse struct {
		loader cdp.LoaderID
		status int64
	}
	var responseMu sync.Mutex
	responses := make(map[cdp.FrameID]documentResponse)
	listenCtx, stopListening := context.WithCancel(browserCtx)
	defer stopListening()
	chromedp.ListenTarget(listenCtx, func(event any) {
		if response, ok := event.(*network.EventResponseReceived); ok && response.Type == network.ResourceTypeDocument {
			responseMu.Lock()
			responses[response.FrameID] = documentResponse{response.LoaderID, response.Response.Status}
			responseMu.Unlock()
		}
	})

	if err := chromedp.Run(browserCtx, chromedp.Navigate(rawURL)); err != nil {
		return "", fmt.Errorf("launch interactive Chrome: %w", err)
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var before, after *page.FrameTree
		var document struct {
			Ready bool   `json:"ready"`
			HTML  string `json:"html"`
		}
		err := chromedp.Run(browserCtx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				before, err = page.GetFrameTree().Do(ctx)
				return err
			}),
			chromedp.Evaluate(`({ready: document.readyState === "complete", html: document.documentElement.outerHTML})`, &document),
			chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				after, err = page.GetFrameTree().Do(ctx)
				return err
			}),
		)
		if err == nil && document.Ready && before.Frame.LoaderID == after.Frame.LoaderID && !IsDDoSGuardChallenge(document.HTML) {
			responseMu.Lock()
			response, found := responses[after.Frame.ID]
			responseMu.Unlock()
			if found && response.loader == after.Frame.LoaderID {
				if response.status < 200 || response.status >= 300 {
					return "", fmt.Errorf("browser page returned HTTP %d", response.status)
				}
				return document.HTML, nil
			}
		}

		select {
		case <-browserCtx.Done():
			if errors.Is(browserCtx.Err(), context.DeadlineExceeded) {
				return "", errors.New("browser verification timed out")
			}
			return "", fmt.Errorf("browser verification canceled or Chrome closed: %w", browserCtx.Err())
		case <-ticker.C:
		}
	}
}

func defaultBrowserProfilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user configuration directory: %w", err)
	}
	return filepath.Join(configDir, "annas-cli", "browser-profile"), nil
}

func prepareBrowserProfile(profilePath func() (string, error)) (string, error) {
	profile, err := profilePath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return "", fmt.Errorf("create browser profile %s: %w", profile, err)
	}
	if err := os.Chmod(profile, 0o700); err != nil {
		return "", fmt.Errorf("secure browser profile %s: %w", profile, err)
	}
	return profile, nil
}

func interactiveDisplayAvailable() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}
