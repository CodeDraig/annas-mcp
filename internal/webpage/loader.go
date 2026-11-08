package webpage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const BrowserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var errPersistentChallenge = errors.New("browser remained on DDoS-Guard verification")

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

	recoveryAfter time.Duration
}

func (b *ChromeBrowser) Load(ctx context.Context, rawURL string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
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
	entries, err := os.ReadDir(profile)
	if err != nil {
		return "", fmt.Errorf("inspect browser profile %s: %w", profile, err)
	}
	var challengeLimit time.Duration
	if len(entries) > 0 {
		challengeLimit = b.recoveryAfter
		if challengeLimit <= 0 {
			challengeLimit = 45 * time.Second
		}
		if deadline, ok := ctx.Deadline(); ok {
			if remaining := time.Until(deadline) / 2; remaining < challengeLimit {
				challengeLimit = remaining
			}
		}
	}

	progress := b.Progress
	if progress == nil {
		progress = os.Stderr
	}
	_, _ = fmt.Fprintln(progress, "Anna's Archive requires browser verification; waiting for Chrome. Complete the check in the opened window if prompted.")
	if challengeLimit > 0 {
		_, _ = fmt.Fprintln(progress, "If verification stays pending, the CLI will preserve this profile and retry once with a fresh profile.")
	}
	html, err := loadChromeProfile(ctx, rawURL, profile, challengeLimit)
	if !errors.Is(err, errPersistentChallenge) {
		return html, err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	archive, err := archiveBrowserProfile(profile)
	if err != nil {
		return "", fmt.Errorf("recover browser profile after pending verification: %w", err)
	}
	_, _ = fmt.Fprintf(progress, "Verification stayed pending; previous browser profile preserved at %s. Retrying once with a fresh profile.\n", archive)
	if _, err := prepareBrowserProfile(func() (string, error) { return profile, nil }); err != nil {
		return "", fmt.Errorf("previous profile preserved at %s: %w", archive, err)
	}
	html, err = loadChromeProfile(ctx, rawURL, profile, 0)
	if err != nil {
		return "", fmt.Errorf("fresh-profile verification failed (previous profile preserved at %s): %w", archive, err)
	}
	return html, nil
}

func archiveBrowserProfile(profile string) (string, error) {
	info, err := os.Lstat(profile)
	if err != nil {
		return "", fmt.Errorf("inspect browser profile before recovery: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing to archive browser profile %s: expected a directory, not a symlink", profile)
	}
	if _, err := os.Lstat(filepath.Join(profile, "SingletonLock")); err == nil {
		return "", fmt.Errorf("browser profile %s is locked; close the other Chrome window before retrying", profile)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect browser profile lock: %w", err)
	}
	container, err := os.MkdirTemp(filepath.Dir(profile), filepath.Base(profile)+".stale-")
	if err != nil {
		return "", fmt.Errorf("create browser profile archive: %w", err)
	}
	archive := filepath.Join(container, "profile")
	if err := os.Rename(profile, archive); err != nil {
		return "", fmt.Errorf("archive browser profile at %s: %w", archive, err)
	}
	return archive, nil
}

func loadChromeProfile(ctx context.Context, rawURL, profile string, challengeLimit time.Duration) (string, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("select Chrome debugging port: %w", err)
	}
	debuggingPort := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("release Chrome debugging port: %w", err)
	}

	allocatorOptions := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocatorOptions = append(allocatorOptions,
		chromedp.Flag("headless", false),
		// Both enable-automation and debugging port zero expose webdriver.
		// Select an available nonzero loopback port for each browser launch.
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("remote-debugging-address", "127.0.0.1"),
		chromedp.Flag("remote-debugging-port", strconv.Itoa(debuggingPort)),
		chromedp.UserDataDir(profile),
	)
	// Keep Chrome alive long enough to flush its profile even when the request
	// times out. Startup and page work still observe the request cancellation.
	lifetimeCtx, endLifetime := context.WithCancel(context.WithoutCancel(ctx))
	defer endLifetime()
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(lifetimeCtx, allocatorOptions...)
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	initialized := make(chan error, 1)
	go func() { initialized <- chromedp.Run(browserCtx) }()
	select {
	case err := <-initialized:
		if err != nil {
			return "", fmt.Errorf("launch interactive Chrome: %w", err)
		}
	case <-ctx.Done():
		endLifetime()
		<-initialized
		return "", ctx.Err()
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(browserCtx, 2*time.Second)
		defer cancel()
		_ = chromedp.Cancel(shutdownCtx)
	}()
	workCtx, cancelWork := context.WithCancel(browserCtx)
	defer cancelWork()
	stopCancellation := context.AfterFunc(ctx, cancelWork)
	defer stopCancellation()
	html, err := loadVerifiedPageWithChallengeLimit(workCtx, rawURL, challengeLimit)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("browser verification timed out: %w", ctx.Err())
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("browser verification canceled: %w", ctx.Err())
	}
	return html, err
}

func loadVerifiedPage(browserCtx context.Context, rawURL string) (string, error) {
	return loadVerifiedPageWithChallengeLimit(browserCtx, rawURL, 0)
}

func loadVerifiedPageWithChallengeLimit(browserCtx context.Context, rawURL string, challengeLimit time.Duration) (string, error) {
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

	// A stalled challenge resource must not block the recovery timer while
	// waiting for the page's load event. The loop below checks completion.
	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, errorText, err := page.Navigate(rawURL).Do(ctx)
		if err != nil {
			return err
		}
		if errorText != "" {
			return fmt.Errorf("page load error %s", errorText)
		}
		return nil
	})); err != nil {
		return "", fmt.Errorf("launch interactive Chrome: %w", err)
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var challengeSince time.Time
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
		if err == nil && before.Frame.LoaderID == after.Frame.LoaderID {
			if IsDDoSGuardChallenge(document.HTML) {
				if challengeSince.IsZero() {
					challengeSince = time.Now()
				}
				if challengeLimit > 0 && time.Since(challengeSince) >= challengeLimit {
					return "", errPersistentChallenge
				}
			} else {
				challengeSince = time.Time{}
				if document.Ready {
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
