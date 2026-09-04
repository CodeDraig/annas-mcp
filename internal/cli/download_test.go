package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iosifache/annas-mcp/internal/anna"
	"github.com/iosifache/annas-mcp/internal/testutil"
)

func TestArticleDownloadCancellationAndFallback(t *testing.T) {
	if !testutil.InFreshProcess(t) {
		return
	}
	var mode atomic.Value
	mode.Store("cancel")
	var fallbackCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "fast_download.json") {
			if mode.Load() == "cancel" {
				<-r.Context().Done()
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"download_url": server.URL + "/file"})
			return
		}
		if r.URL.Path == "/file" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><title>Error</title></html>")
			return
		}
		fallbackCalls.Add(1)
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, "%PDF-1.7\nfallback")
	}))
	defer server.Close()
	previous := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = previous }()
	config := t.TempDir()
	folder := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := os.MkdirAll(filepath.Join(config, "annas-cli"), 0700); err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf("ANNAS_SECRET_KEY=test-key\nANNAS_DOWNLOAD_PATH=%s\nANNAS_BASE_URL=%s\nANNAS_AUTO_BASE_URL=false\n", folder, strings.TrimPrefix(server.URL, "https://"))
	if err := os.WriteFile(filepath.Join(config, "annas-cli", ".env"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"cancel", "html"} {
		t.Run(scenario, func(t *testing.T) {
			mode.Store(scenario)
			command := newCommand(operations{lookupDOI: func(context.Context, string, time.Duration) (*anna.Paper, error) {
				return &anna.Paper{Title: "Article", Hash: "hash", DownloadURL: server.URL + "/scidb"}, nil
			}})
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&bytes.Buffer{})
			command.SetArgs([]string{"article-download", "10.1/test"})
			ctx := context.Background()
			if scenario == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
			}
			err := command.ExecuteContext(ctx)
			if scenario == "cancel" {
				if !errors.Is(err, context.DeadlineExceeded) || output.Len() != 0 || fallbackCalls.Load() != 0 {
					t.Fatalf("cancellation: err=%v stdout=%s fallback=%d", err, output.String(), fallbackCalls.Load())
				}
				files, _ := os.ReadDir(folder)
				if len(files) != 0 {
					t.Fatal("cancel created file")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				var result response[downloadData]
				if err := json.Unmarshal(output.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Data.Method != "scidb" || fallbackCalls.Load() != 1 {
					t.Fatalf("fallback result: %+v calls=%d", result, fallbackCalls.Load())
				}
				body, err := os.ReadFile(result.Data.Path)
				if err != nil || string(body) != "%PDF-1.7\nfallback" {
					t.Fatalf("fallback body %q %v", body, err)
				}
			}
		})
	}
}
