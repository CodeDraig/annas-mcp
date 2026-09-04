package anna

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iosifache/annas-mcp/internal/testutil"
)

func TestDownloadHTTP(t *testing.T) {
	if !testutil.InFreshProcess(t) {
		return
	}
	var mode atomic.Value
	mode.Store("valid")
	var entered chan struct{}
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := mode.Load().(string)
		api := strings.Contains(r.URL.Path, "fast_download.json")
		if (api && current == "api-stall") || (!api && current == "headers-stall") {
			close(entered)
			<-r.Context().Done()
			return
		}
		if api {
			json.NewEncoder(w).Encode(map[string]string{"download_url": server.URL + "/file"})
			return
		}
		if current == "html" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<!doctype html><html><title>Error</title></html>")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		if current == "body-stall" {
			fmt.Fprint(w, "%PDF-1.7\n"+strings.Repeat("x", 8192))
			w.(http.Flusher).Flush()
			close(entered)
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, "%PDF-1.7\nvalid document")
	}))
	defer server.Close()
	previous := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = previous }()
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := os.MkdirAll(filepath.Join(config, "annas-cli"), 0700); err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf("ANNAS_SECRET_KEY=test-key\nANNAS_DOWNLOAD_PATH=%s\nANNAS_BASE_URL=%s\nANNAS_AUTO_BASE_URL=false\n", t.TempDir(), strings.TrimPrefix(server.URL, "https://"))
	if err := os.WriteFile(filepath.Join(config, "annas-cli", ".env"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"book", "paper"} {
		download := func(ctx context.Context, folder string) (string, error) {
			if kind == "book" {
				return (&Book{Hash: "testhash", Title: "Document", Format: "pdf"}).Download(ctx, "test-key", folder, time.Second)
			}
			return (&Paper{Title: "Document", DownloadURL: server.URL + "/file"}).Download(ctx, folder, time.Second)
		}
		for _, scenario := range []string{"valid", "html", "api-stall", "headers-stall", "body-stall"} {
			if kind == "paper" && scenario == "api-stall" {
				continue
			}
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				mode.Store(scenario)
				entered = make(chan struct{})
				folder := t.TempDir()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if strings.HasSuffix(scenario, "stall") {
					done := make(chan error, 1)
					go func() { _, err := download(ctx, folder); done <- err }()
					select {
					case <-entered:
					case <-ctx.Done():
						t.Fatal("request never entered stall")
					}
					if scenario == "body-stall" {
						deadline := time.Now().Add(time.Second)
						for {
							files, _ := os.ReadDir(folder)
							if len(files) > 0 {
								break
							}
							if time.Now().After(deadline) {
								t.Fatal("destination never created")
							}
							time.Sleep(time.Millisecond)
						}
					}
					cancel()
					select {
					case err := <-done:
						if !errors.Is(err, context.Canceled) {
							t.Fatalf("cancel returned %v", err)
						}
					case <-time.After(time.Second):
						t.Fatal("download ignored cancellation")
					}
					files, _ := os.ReadDir(folder)
					if len(files) != 0 {
						t.Fatal("canceled download left a file")
					}
				} else {
					path, err := download(ctx, folder)
					if scenario == "html" {
						if err == nil || path != "" {
							t.Fatalf("HTML accepted: %s %v", path, err)
						}
						files, _ := os.ReadDir(folder)
						if len(files) != 0 {
							t.Fatal("HTML created a destination")
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						content, err := os.ReadFile(path)
						if err != nil || string(content) != "%PDF-1.7\nvalid document" {
							t.Fatalf("payload changed: %q %v", content, err)
						}
					}
				}
			})
		}
	}
}

func TestValidateDownloadPayloadFormats(t *testing.T) {
	for _, tc := range []struct {
		name, format, mime, body string
		reject                   bool
	}{
		{"PDF", "pdf", "application/octet-stream", "%PDF-1.7\n<html>quoted example</html>", false},
		{"EPUB", "epub", "application/octet-stream", "PK\x03\x04epub<html>stored chapter</html>", false},
		{"HTML disguised as binary", "pdf", "application/octet-stream", "<!doctype html><html>Error</html>", true},
		{"HTML MIME", "epub", "text/html; charset=utf-8", "Access denied", true},
		{"explicit text", "txt", "text/plain", "<html>example source</html>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &http.Response{Header: make(http.Header), Body: http.NoBody}
			response.Header.Set("Content-Type", tc.mime)
			response.Body = io.NopCloser(strings.NewReader(tc.body))
			reader, err := validateDownloadBody(response, tc.format)
			if tc.reject {
				if err == nil {
					t.Fatal("accepted error HTML")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			if err != nil || string(got) != tc.body {
				t.Fatalf("changed payload: %q %v", got, err)
			}
		})
	}
}
