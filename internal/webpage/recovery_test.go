package webpage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func requireVisibleChrome(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("google-chrome"); err != nil {
		t.Skip("Chrome is not installed")
	}
	if !interactiveDisplayAvailable() {
		t.Skip("a graphical display is required")
	}
}

func TestChromeBrowserRecoversStaleProfile(t *testing.T) {
	requireVisibleChrome(t)
	var staleRequests, freshRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pending-script" {
			<-r.Context().Done()
			return
		}
		if r.URL.Path == "/seed" {
			http.SetCookie(w, &http.Cookie{Name: "stale", Value: "yes", Path: "/", MaxAge: 3600})
			fmt.Fprint(w, `<!doctype html><html><title>Seed profile</title></html>`)
			return
		}
		if r.URL.Path == "/search" {
			if _, err := r.Cookie("stale"); err == nil {
				staleRequests.Add(1)
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<!doctype html><html><title>DDoS-Guard</title><script src="/pending-script"></script><body>Pending verification</body></html>`)
				return
			}
			freshRequests.Add(1)
		}
		fmt.Fprint(w, `<!doctype html><html><title>Recovered results</title><a href="/md5/result">Result</a></html>`)
	}))
	defer server.Close()
	profile := filepath.Join(t.TempDir(), "profile")
	var progress bytes.Buffer
	browser := &ChromeBrowser{
		Progress:      &progress,
		ProfilePath:   func() (string, error) { return profile, nil },
		recoveryAfter: 250 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if _, err := browser.Load(ctx, server.URL+"/seed"); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profile, "preserve-me"), []byte("original state"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		html, err := browser.Load(ctx, server.URL+"/search")
		if err != nil || !strings.Contains(html, "Recovered results") {
			t.Fatalf("load after stale cookie: html=%q err=%v", html, err)
		}
	}
	if staleRequests.Load() != 1 || freshRequests.Load() != 2 {
		t.Fatalf("requests with stale=%d fresh=%d, want 1 and 2", staleRequests.Load(), freshRequests.Load())
	}
	archives, err := filepath.Glob(profile + ".stale-*/profile")
	if err != nil || len(archives) != 1 {
		t.Fatalf("archives=%v err=%v, want one", archives, err)
	}
	contents, err := os.ReadFile(filepath.Join(archives[0], "preserve-me"))
	if err != nil || string(contents) != "original state" {
		t.Fatalf("previous state was not preserved: contents=%q err=%v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(profile, "preserve-me")); !os.IsNotExist(err) {
		t.Fatalf("replacement profile contains prior marker: %v", err)
	}
	if !strings.Contains(progress.String(), archives[0]) {
		t.Fatalf("progress did not report archive path: %s", progress.String())
	}
	info, err := os.Stat(filepath.Dir(archives[0]))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("archive permissions=%o, want 700", info.Mode().Perm())
	}
}

func TestChromeBrowserLimitsProfileRecovery(t *testing.T) {
	requireVisibleChrome(t)
	for _, test := range []struct {
		name        string
		existing    bool
		wantLoads   int32
		wantArchive int
		limit       time.Duration
	}{
		{name: "fresh profile is left for manual verification", wantLoads: 1, limit: 250 * time.Millisecond},
		{name: "existing profile is retried only once", existing: true, wantLoads: 2, wantArchive: 1, limit: 250 * time.Millisecond},
		{name: "short request reserves time for recovery", existing: true, wantLoads: 2, wantArchive: 1, limit: time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/search" {
					requests.Add(1)
				}
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<!doctype html><html><title>DDoS-Guard</title></html>`)
			}))
			defer server.Close()
			profile := filepath.Join(t.TempDir(), "profile")
			if test.existing {
				if err := os.Mkdir(profile, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(profile, "old-state"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			browser := &ChromeBrowser{
				Progress:      io.Discard,
				ProfilePath:   func() (string, error) { return profile, nil },
				recoveryAfter: test.limit,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			started := time.Now()
			_, err := browser.Load(ctx, server.URL+"/search")
			if err == nil {
				t.Fatal("pending challenge returned success")
			}
			if time.Since(started) > 6*time.Second {
				t.Fatal("recovery extended the original request deadline")
			}
			if requests.Load() != test.wantLoads {
				t.Fatalf("requests=%d, want %d; err=%v", requests.Load(), test.wantLoads, err)
			}
			archives, globErr := filepath.Glob(profile + ".stale-*/profile")
			if globErr != nil || len(archives) != test.wantArchive {
				t.Fatalf("archives=%v err=%v, want %d", archives, globErr, test.wantArchive)
			}
			if test.existing && !strings.Contains(err.Error(), "previous profile preserved at "+archives[0]) {
				t.Fatalf("retry failure did not report preserved profile: %v", err)
			}
		})
	}
}

func TestChromeBrowserDoesNotRecoverOrdinaryHTTPError(t *testing.T) {
	requireVisibleChrome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `<!doctype html><html><title>Forbidden</title></html>`)
	}))
	defer server.Close()
	profile := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile, "old-state"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	browser := &ChromeBrowser{
		Progress:      io.Discard,
		ProfilePath:   func() (string, error) { return profile, nil },
		recoveryAfter: time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := browser.Load(ctx, server.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("Load returned %v, want HTTP 403", err)
	}
	archives, err := filepath.Glob(profile + ".stale-*/profile")
	if err != nil || len(archives) != 0 {
		t.Fatalf("ordinary forbidden response archived profile: %v, %v", archives, err)
	}
}

func TestChromeBrowserCancellationDoesNotTouchProfile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	profileCalled := false
	browser := &ChromeBrowser{ProfilePath: func() (string, error) {
		profileCalled = true
		return t.TempDir(), nil
	}}
	if _, err := browser.Load(ctx, "https://annas-archive.test/search"); err != context.Canceled {
		t.Fatalf("Load returned %v, want cancellation", err)
	}
	if profileCalled {
		t.Fatal("canceled request touched browser profile")
	}
}

func TestChromeBrowserCancellationDuringNavigationDoesNotRecover(t *testing.T) {
	requireVisibleChrome(t)
	ctx, deadlineCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer deadlineCancel()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	requested := make(chan struct{})
	var observed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `<!doctype html><html><title>DDoS-Guard</title></html>`)
		if r.URL.Path == "/search" && observed.CompareAndSwap(false, true) {
			close(requested)
		}
	}))
	defer server.Close()
	go func() {
		select {
		case <-requested:
			cancel()
		case <-ctx.Done():
		}
	}()
	profile := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile, "old-state"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	browser := &ChromeBrowser{
		Progress:      io.Discard,
		ProfilePath:   func() (string, error) { return profile, nil },
		recoveryAfter: time.Millisecond,
	}
	_, err := browser.Load(ctx, server.URL+"/search")
	if !errors.Is(err, context.Canceled) || !observed.Load() {
		t.Fatalf("Load returned %v, request observed=%v; want in-flight cancellation", err, observed.Load())
	}
	archives, globErr := filepath.Glob(profile + ".stale-*/profile")
	if globErr != nil || len(archives) != 0 {
		t.Fatalf("canceled navigation archived profile: %v, %v", archives, globErr)
	}
}

func TestChromeBrowserStartupFailureDoesNotRecover(t *testing.T) {
	requireVisibleChrome(t)
	if runtime.GOOS != "linux" {
		t.Skip("this fixture simulates an unavailable Linux display")
	}
	t.Setenv("DISPLAY", ":987654")
	t.Setenv("WAYLAND_DISPLAY", "")
	profile := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile, "old-state"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	browser := &ChromeBrowser{
		Progress:    io.Discard,
		ProfilePath: func() (string, error) { return profile, nil },
		DisplayOK:   func() bool { return true },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := browser.Load(ctx, "https://annas-archive.test/search")
	if err == nil || !strings.Contains(err.Error(), "launch interactive Chrome") {
		t.Fatalf("Load returned %v, want Chrome startup error", err)
	}
	archives, globErr := filepath.Glob(profile + ".stale-*/profile")
	if globErr != nil || len(archives) != 0 {
		t.Fatalf("startup failure archived profile: %v, %v", archives, globErr)
	}
}

func TestArchiveBrowserProfileRefusesLockedProfile(t *testing.T) {
	profile := t.TempDir()
	lock := filepath.Join(profile, "SingletonLock")
	if err := os.WriteFile(lock, []byte("owned by Chrome"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := archiveBrowserProfile(profile); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("archive returned %v, want locked error", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("locked profile was moved: %v", err)
	}
}

func TestArchiveBrowserProfileRefusesSymlink(t *testing.T) {
	target := t.TempDir()
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.Symlink(target, profile); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if _, err := archiveBrowserProfile(profile); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("archive returned %v, want symlink error", err)
	}
	if _, err := os.Lstat(profile); err != nil {
		t.Fatalf("profile symlink was moved: %v", err)
	}
}
