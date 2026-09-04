package webpage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

type httpClientFunc func(*http.Request) (*http.Response, error)

func (f httpClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

type browserFunc func(context.Context, string) (string, error)

func (f browserFunc) Load(ctx context.Context, rawURL string) (string, error) {
	return f(ctx, rawURL)
}

func TestLoaderReturnsOrdinaryHTTPPageWithoutBrowser(t *testing.T) {
	t.Parallel()

	browserCalls := 0
	loader := &Loader{
		Client: httpClientFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("User-Agent") != BrowserUserAgent {
				t.Fatalf("unexpected User-Agent: %q", request.Header.Get("User-Agent"))
			}
			return pageResponse(request, http.StatusOK, `<html><title>Results</title></html>`), nil
		}),
		Browser: browserFunc(func(context.Context, string) (string, error) {
			browserCalls++
			return "", nil
		}),
	}

	html, err := loader.Load(context.Background(), "https://annas-archive.test/search", time.Second)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !strings.Contains(html, "Results") || browserCalls != 0 {
		t.Fatalf("unexpected result %q or browser calls %d", html, browserCalls)
	}
}

func TestLoaderUsesBrowserOnlyForDDoSGuardChallenge(t *testing.T) {
	t.Parallel()

	browserURL := ""
	loader := &Loader{
		Client: httpClientFunc(func(request *http.Request) (*http.Response, error) {
			return pageResponse(request, http.StatusForbidden, ddosGuardHTML()), nil
		}),
		Browser: browserFunc(func(_ context.Context, rawURL string) (string, error) {
			browserURL = rawURL
			return `<html><title>Search</title><a href="/md5/hash">result</a></html>`, nil
		}),
	}

	html, err := loader.Load(context.Background(), "https://annas-archive.test/search?q=test&check=1", time.Second)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if browserURL == "" || !strings.Contains(html, "/md5/hash") {
		t.Fatalf("browser fallback did not return verified HTML: url=%q html=%q", browserURL, html)
	}
}

func TestLoaderDoesNotUseBrowserForOrdinaryForbiddenResponse(t *testing.T) {
	t.Parallel()

	browserCalls := 0
	loader := &Loader{
		Client: httpClientFunc(func(request *http.Request) (*http.Response, error) {
			return pageResponse(request, http.StatusForbidden, `<html><title>Forbidden</title></html>`), nil
		}),
		Browser: browserFunc(func(context.Context, string) (string, error) {
			browserCalls++
			return "", nil
		}),
	}

	_, err := loader.Load(context.Background(), "https://annas-archive.test/search", time.Second)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("Load returned %v, want HTTP 403 error", err)
	}
	if browserCalls != 0 {
		t.Fatalf("ordinary 403 launched browser %d times", browserCalls)
	}
}

func TestLoaderUsesBrowserForDDoSGuardRedirect(t *testing.T) {
	t.Parallel()

	browserCalls := 0
	loader := &Loader{
		Client: httpClientFunc(func(request *http.Request) (*http.Response, error) {
			response := pageResponse(request, http.StatusForbidden, `<html><title>Checking</title></html>`)
			redirected, err := http.NewRequest(http.MethodGet, "https://check.ddos-guard.net/check.js", nil)
			if err != nil {
				t.Fatalf("create redirected request: %v", err)
			}
			response.Request = redirected
			return response, nil
		}),
		Browser: browserFunc(func(_ context.Context, rawURL string) (string, error) {
			browserCalls++
			return `<html><title>Search</title></html>`, nil
		}),
	}

	_, err := loader.Load(context.Background(), "https://annas-archive.test/search", time.Second)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if browserCalls != 1 {
		t.Fatalf("DDoS-Guard redirect launched browser %d times, want 1", browserCalls)
	}
}

func TestLoaderAppliesTimeoutToBrowserVerification(t *testing.T) {
	t.Parallel()

	loader := &Loader{
		Client: httpClientFunc(func(request *http.Request) (*http.Response, error) {
			return pageResponse(request, http.StatusForbidden, ddosGuardHTML()), nil
		}),
		Browser: browserFunc(func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}),
	}

	_, err := loader.Load(context.Background(), "https://annas-archive.test/search", 10*time.Millisecond)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Load returned %v, want deadline exceeded", err)
	}
}

func TestLoaderPropagatesBrowserTimeoutAndClosure(t *testing.T) {
	t.Parallel()

	tests := []string{"browser verification timed out", "interactive Chrome closed before verification completed"}
	for _, message := range tests {
		message := message
		t.Run(message, func(t *testing.T) {
			t.Parallel()
			loader := &Loader{
				Client: httpClientFunc(func(request *http.Request) (*http.Response, error) {
					return pageResponse(request, http.StatusForbidden, ddosGuardHTML()), nil
				}),
				Browser: browserFunc(func(context.Context, string) (string, error) {
					return "", errors.New(message)
				}),
			}
			_, err := loader.Load(context.Background(), "https://annas-archive.test/search", time.Second)
			if err == nil || !strings.Contains(err.Error(), message) {
				t.Fatalf("Load returned %v, want %q", err, message)
			}
		})
	}
}

func TestLoaderRejectsBrowserHTMLThatStillContainsChallenge(t *testing.T) {
	t.Parallel()

	loader := &Loader{
		Client: httpClientFunc(func(request *http.Request) (*http.Response, error) {
			return pageResponse(request, http.StatusForbidden, ddosGuardHTML()), nil
		}),
		Browser: browserFunc(func(context.Context, string) (string, error) {
			return ddosGuardHTML(), nil
		}),
	}
	_, err := loader.Load(context.Background(), "https://annas-archive.test/search", time.Second)
	if err == nil || !strings.Contains(err.Error(), "did not complete") {
		t.Fatalf("Load returned %v", err)
	}
}

func TestChromeBrowserRequiresDisplayBeforeCreatingProfile(t *testing.T) {
	t.Parallel()

	profileCalls := 0
	browser := &ChromeBrowser{
		Progress: io.Discard,
		DisplayOK: func() bool {
			return false
		},
		ProfilePath: func() (string, error) {
			profileCalls++
			return t.TempDir(), nil
		},
	}
	_, err := browser.Load(context.Background(), "https://annas-archive.test/search")
	if err == nil || !strings.Contains(err.Error(), "graphical display") {
		t.Fatalf("Load returned %v", err)
	}
	if profileCalls != 0 {
		t.Fatalf("profile path called %d times without a display", profileCalls)
	}
}

func TestPrepareBrowserProfileUsesPrivatePermissions(t *testing.T) {
	t.Parallel()

	profile := filepath.Join(t.TempDir(), "profile")
	got, err := prepareBrowserProfile(func() (string, error) { return profile, nil })
	if err != nil {
		t.Fatalf("prepareBrowserProfile returned error: %v", err)
	}
	if got != profile {
		t.Fatalf("prepareBrowserProfile returned %q, want %q", got, profile)
	}
	info, err := os.Stat(profile)
	if err != nil {
		t.Fatalf("stat profile: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("profile mode = %o, want 700", info.Mode().Perm())
	}
}

func pageResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    request,
	}
}

func ddosGuardHTML() string {
	return `<!doctype html><html><head><title>DDoS-Guard</title><script src="https://check.ddos-guard.net/check.js"></script></head></html>`
}

func TestChromeFinalDocumentStatus(t *testing.T) {
	if _, err := exec.LookPath("google-chrome"); err != nil {
		t.Skip("Chrome is not installed")
	}
	for _, status := range []int{200, 403, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/challenge":
					w.WriteHeader(403)
					fmt.Fprint(w, `<html><title>DDoS-Guard</title><script>setTimeout(() => location.href='/redirect', 100)</script></html>`)
				case "/redirect":
					http.Redirect(w, r, "/final", http.StatusFound)
				case "/child":
					w.WriteHeader(503)
					fmt.Fprint(w, "failed iframe")
				default:
					w.WriteHeader(status)
					fmt.Fprint(w, `<html><title>Final page</title><body><iframe src="/child"></iframe>Result</body></html>`)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			allocator, stop := chromedp.NewExecAllocator(ctx, chromedp.DefaultExecAllocatorOptions[:]...)
			defer stop()
			browser, closeBrowser := chromedp.NewContext(allocator)
			defer closeBrowser()
			html, err := loadVerifiedPage(browser, server.URL+"/challenge")
			if status == 200 {
				if err != nil || !strings.Contains(html, "Final page") {
					t.Fatalf("successful page: %q, %v", html, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "HTTP "+strconv.Itoa(status)) || html != "" {
				t.Fatalf("error page: %q, %v", html, err)
			}
		})
	}
}
