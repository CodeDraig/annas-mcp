package anna

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iosifache/annas-mcp/internal/testutil"
)

type redirectTransport func(*http.Request) (*http.Response, error)

func (f redirectTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAuthenticatedRedirectBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		blocked      bool
	}{
		{"same origin", "https://api.example/canonical", false},
		{"other host", "https://files.example/file", true},
		{"other port", "https://api.example:444/file", true},
		{"downgrade", "http://api.example/file", true},
		{"key copied to other host", "https://files.example/file?key=canary", true},
		{"redirect userinfo", "https://user:password@api.example/file", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := authenticatedDownloadClient(time.Second)
			calls := 0
			client.Transport = redirectTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {tc.target}}, Body: http.NoBody, Request: r}, nil
				}
				if got := r.Header.Get("Referer"); got != "" {
					t.Errorf("redirect exposed Referer: %q", got)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
			})
			resp, err := downloadRequest(context.Background(), client, "https://api.example/start?key=canary")
			if resp != nil {
				resp.Body.Close()
			}
			if tc.blocked {
				if err == nil || calls != 1 {
					t.Fatalf("cross-boundary request: calls=%d err=%v", calls, err)
				}
			} else if err != nil || calls != 2 {
				t.Fatalf("same-origin redirect: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestBookRedirects(t *testing.T) {
	if !testutil.InFreshProcess(t) {
		return
	}
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := os.MkdirAll(filepath.Join(config, "annas-cli"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "annas-cli", ".env"), []byte("ANNAS_SECRET_KEY=canary\nANNAS_DOWNLOAD_PATH="+t.TempDir()+"\nANNAS_BASE_URL=api.example\nANNAS_AUTO_BASE_URL=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous }()
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "API and CDN redirects", true: "API boundary rejection"}[blocked], func(t *testing.T) {
			calls := 0
			http.DefaultTransport = redirectTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: r}
				redirect := func(target string) (*http.Response, error) {
					response.StatusCode = http.StatusFound
					response.Header.Set("Location", target)
					return response, nil
				}
				if strings.Contains(r.Header.Get("Referer"), "canary") {
					t.Error("API key in Referer")
				}
				switch r.URL.Host + r.URL.Path {
				case "api.example/dyn/api/fast_download.json":
					if blocked {
						return redirect("https://untrusted.example/api?key=canary")
					}
					return redirect("https://api.example/api-final?key=canary")
				case "api.example/api-final":
					payload, _ := json.Marshal(map[string]string{"download_url": "https://files.example/start"})
					response.Body = io.NopCloser(strings.NewReader(string(payload)))
				case "files.example/start":
					return redirect("https://cdn.example/document")
				case "cdn.example/document":
					response.Header.Set("Content-Type", "application/pdf")
					response.Body = io.NopCloser(strings.NewReader("%PDF-1.7\ncanary document"))
				default:
					t.Errorf("unexpected destination: %s", r.URL)
				}
				return response, nil
			})
			folder := t.TempDir()
			path, err := (&Book{Hash: "hash", Title: "Document", Format: "pdf"}).Download(context.Background(), "canary", folder, time.Second)
			if blocked {
				if err == nil || calls != 1 || path != "" {
					t.Fatalf("rejection: calls=%d path=%q err=%v", calls, path, err)
				}
				if strings.Contains(err.Error(), "canary") {
					t.Fatal("error exposed key")
				}
				files, _ := os.ReadDir(folder)
				if len(files) != 0 {
					t.Fatal("rejected API created a file")
				}
			} else {
				if err != nil || calls != 4 {
					t.Fatalf("delivery: calls=%d err=%v", calls, err)
				}
				body, err := os.ReadFile(path)
				if err != nil || string(body) != "%PDF-1.7\ncanary document" {
					t.Fatalf("payload %q: %v", body, err)
				}
			}
		})
	}
}

func TestAuthenticatedRedirectLoop(t *testing.T) {
	client := authenticatedDownloadClient(time.Second)
	calls := 0
	client.Transport = redirectTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"/loop"}}, Body: http.NoBody, Request: r}, nil
	})
	resp, err := downloadRequest(context.Background(), client, "https://api.example/loop")
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || calls != 10 {
		t.Fatalf("redirect loop: calls=%d err=%v", calls, err)
	}
}
