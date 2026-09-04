package mirror

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestResolveNeverProbesOrReturnsUnsafeCandidate(t *testing.T) {
	t.Parallel()

	html := flatMonitorPage(flatMonitorCard("https://annas-archive.fake@attacker.example/", "UP", "", "10"))
	client := statusPageClient(t, html, nil)
	probeCalls := 0
	resolver := NewResolver(client, "https://status.example/annas.html", func(context.Context, string) error {
		probeCalls++
		return nil
	})

	baseURL, err := resolver.Resolve(context.Background(), ResolveOptions{FallbackBaseURL: "fallback.example"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if baseURL != "fallback.example" {
		t.Fatalf("Resolve returned unsafe candidate %q", baseURL)
	}
	if probeCalls != 0 {
		t.Fatalf("expected unsafe candidate not to be probed, got %d probes", probeCalls)
	}
}

func TestResolvePrefersFastHealthyCandidateAndSkipsFailedProbe(t *testing.T) {
	t.Parallel()

	html := flatMonitorPage(
		flatMonitorCard("https://annas-archive.slow", "UP", "", "900"),
		flatMonitorCard("https://annas-archive.fail", "UP", "", "100"),
		flatMonitorCard("https://annas-archive.good", "UP", "", "150"),
		flatMonitorCard("https://annas-archive.degraded", "DEGRADED", "", "10"),
	)
	client := statusPageClient(t, html, nil)
	probeCalls := make([]string, 0)
	resolver := NewResolver(client, "https://status.example/annas.html", func(_ context.Context, baseURL string) error {
		probeCalls = append(probeCalls, baseURL)
		if baseURL == "annas-archive.fail" {
			return errors.New("probe failed")
		}
		return nil
	})

	baseURL, err := resolver.Resolve(context.Background(), ResolveOptions{FallbackBaseURL: "fallback.example"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if baseURL != "annas-archive.good" {
		t.Fatalf("Resolve returned %q, want annas-archive.good", baseURL)
	}
	if len(probeCalls) != 2 || probeCalls[0] != "annas-archive.fail" || probeCalls[1] != "annas-archive.good" {
		t.Fatalf("unexpected probes: %v", probeCalls)
	}
}

func TestResolveSelectsConfiguredProtectedMirrorWithoutHeartbeatRequest(t *testing.T) {
	t.Parallel()

	html := flatMonitorPage(
		flatMonitorCard("https://annas-archive.gl", "PROTECTED", "", "100"),
		flatMonitorCard("https://annas-archive.pk", "PROTECTED", "", "500"),
	)
	requests := make([]string, 0)
	client := statusPageClient(t, html, &requests)
	resolver := NewResolver(client, "https://status.example/annas.html", func(context.Context, string) error {
		t.Fatal("protected mirror must not be HTTP-probed")
		return nil
	})

	baseURL, err := resolver.Resolve(context.Background(), ResolveOptions{FallbackBaseURL: "annas-archive.pk"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if baseURL != "annas-archive.pk" {
		t.Fatalf("Resolve returned %q, want configured protected mirror", baseURL)
	}
	if len(requests) != 1 || requests[0] != "https://status.example/annas.html" {
		t.Fatalf("resolver made obsolete or unexpected requests: %v", requests)
	}
}

func TestResolveSelectsFastestProtectedMirrorWithoutConfiguredMatch(t *testing.T) {
	t.Parallel()

	html := flatMonitorPage(
		flatMonitorCard("https://annas-archive.slow", "PROTECTED", "", "500"),
		flatMonitorCard("https://annas-archive.fast", "PROTECTED", "", "100"),
	)
	resolver := NewResolver(statusPageClient(t, html, nil), "https://status.example/annas.html", func(context.Context, string) error {
		t.Fatal("protected mirror must not be HTTP-probed")
		return nil
	})

	baseURL, err := resolver.Resolve(context.Background(), ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if baseURL != "annas-archive.fast" {
		t.Fatalf("Resolve returned %q, want fastest protected mirror", baseURL)
	}
}

func TestResolveFallsBackWhenStatusPageFails(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
	resolver := NewResolver(client, "https://status.example/annas.html", func(context.Context, string) error { return nil })
	baseURL, err := resolver.Resolve(context.Background(), ResolveOptions{FallbackBaseURL: "fallback.example"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if baseURL != "fallback.example" {
		t.Fatalf("Resolve returned %q, want fallback.example", baseURL)
	}
}

func TestResolveReturnsErrorWhenNoCandidatesAndNoFallback(t *testing.T) {
	t.Parallel()

	resolver := NewResolver(statusPageClient(t, "<html></html>", nil), "https://status.example/annas.html", func(context.Context, string) error { return nil })
	_, err := resolver.Resolve(context.Background(), ResolveOptions{})
	if err == nil || err.Error() != "no Anna mirror candidates discovered" {
		t.Fatalf("Resolve returned %v", err)
	}
}

func statusPageClient(t *testing.T, html string, requests *[]string) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if requests != nil {
			*requests = append(*requests, req.URL.String())
		}
		if req.URL.String() != "https://status.example/annas.html" {
			return nil, errors.New("unexpected request: " + req.URL.String())
		}
		return stringResponse(req, http.StatusOK, html), nil
	})}
}

func stringResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}
}

func TestResolveCancellationStopsFetchAndProbes(t *testing.T) {
	for _, stage := range []string{"status", "probe"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if stage == "status" {
					cancel()
					return nil, req.Context().Err()
				}
				return stringResponse(req, 200, flatMonitorPage(flatMonitorCard("https://annas-archive.one", "UP", "", "1"), flatMonitorCard("https://annas-archive.two", "UP", "", "2"))), nil
			})}
			resolver := NewResolver(client, "https://status.example", func(ctx context.Context, _ string) error { calls++; cancel(); return ctx.Err() })
			_, err := resolver.Resolve(ctx, ResolveOptions{FallbackBaseURL: "fallback.example"})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled discovery returned %v", err)
			}
			if calls > 1 || (stage == "status" && calls != 0) {
				t.Fatalf("continued probing: %d", calls)
			}
		})
	}
}
