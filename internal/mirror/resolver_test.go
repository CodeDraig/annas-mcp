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

	html := `<script>
window.preloadData = {'config':{'slug':'slum'},'publicGroupList':[
  {'id':1,'name':'Anna\'s Archive','monitorList':[
    {'id':900,'name':'Unsafe','url':'https://annas-archive.fake@attacker.example/'}
  ]}
]};
</script>`
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://status.example/" {
			return nil, errors.New("unexpected request: " + req.URL.String())
		}
		return stringResponse(req, http.StatusOK, html), nil
	})}
	probeCalls := 0
	resolver := NewResolver(client, "https://status.example/", func(ctx context.Context, baseURL string) error {
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

func TestResolveBestBaseURLPrefersHealthyFastCandidateAndSkipsFailedProbe(t *testing.T) {
	t.Parallel()

	html := `
<script>
window.preloadData = {'config':{'slug':'slum'},'incident':null,'publicGroupList':[
  {'id':1,'name':'Anna\'s Archive','monitorList':[
    {'id':10,'name':'Slow','sendUrl':1,'type':'keyword','url':'https://annas-archive.slow/'},
    {'id':20,'name':'FastButProbeFails','sendUrl':1,'type':'keyword','url':'https://annas-archive.fail/'},
    {'id':30,'name':'FastHealthy','sendUrl':1,'type':'keyword','url':'https://annas-archive.good/'}
  ]}
],'maintenanceList':[]};
</script>`
	heartbeatJSON := `{
		"heartbeatList": {
			"10": [{"status": 1, "time": "2026-03-23 10:00:00", "ping": 900}, {"status": 1, "time": "2026-03-23 10:01:00", "ping": 800}],
			"20": [{"status": 1, "time": "2026-03-23 10:00:00", "ping": 100}, {"status": 1, "time": "2026-03-23 10:01:00", "ping": 110}],
			"30": [{"status": 1, "time": "2026-03-23 10:00:00", "ping": 150}, {"status": 1, "time": "2026-03-23 10:01:00", "ping": 140}]
		},
		"uptimeList": {}
	}`
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://status.example/":
			return stringResponse(req, http.StatusOK, html), nil
		case "https://status.example/api/status-page/heartbeat/slum":
			return stringResponse(req, http.StatusOK, heartbeatJSON), nil
		default:
			return nil, errors.New("unexpected request: " + req.URL.String())
		}
	})}

	probeCalls := make([]string, 0)
	resolver := NewResolver(client, "https://status.example/", func(ctx context.Context, baseURL string) error {
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
		t.Fatalf("expected annas-archive.good, got %q", baseURL)
	}
	if len(probeCalls) == 0 {
		t.Fatal("expected probe to be called")
	}
}

func TestResolveProbesURLFallbackCandidatesWithoutHeartbeatData(t *testing.T) {
	t.Parallel()

	html := `<html><body><a href="https://annas-archive.fallback/">mirror</a></body></html>`
	heartbeatJSON := `{"heartbeatList":{},"uptimeList":{}}`
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://status.example/":
			return stringResponse(req, http.StatusOK, html), nil
		case "https://status.example/api/status-page/heartbeat/slum":
			return stringResponse(req, http.StatusOK, heartbeatJSON), nil
		default:
			return nil, errors.New("unexpected request: " + req.URL.String())
		}
	})}
	resolver := NewResolver(client, "https://status.example/", func(ctx context.Context, baseURL string) error {
		if baseURL != "annas-archive.fallback" {
			return errors.New("unexpected probe: " + baseURL)
		}
		return nil
	})

	baseURL, err := resolver.Resolve(context.Background(), ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if baseURL != "annas-archive.fallback" {
		t.Fatalf("expected annas-archive.fallback, got %q", baseURL)
	}
}

func TestResolveFallsBackWhenStatusPageFails(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
	resolver := NewResolver(client, "https://status.example/", func(ctx context.Context, baseURL string) error { return nil })
	baseURL, err := resolver.Resolve(context.Background(), ResolveOptions{FallbackBaseURL: "fallback.example"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if baseURL != "fallback.example" {
		t.Fatalf("expected fallback.example, got %q", baseURL)
	}
}

func TestResolveReturnsErrorWhenNoCandidatesAndNoFallback(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return stringResponse(req, http.StatusOK, "<html></html>"), nil
	})}
	resolver := NewResolver(client, "https://status.example/", func(ctx context.Context, baseURL string) error { return nil })
	_, err := resolver.Resolve(context.Background(), ResolveOptions{})
	if err == nil || err.Error() != "no Anna mirror candidates discovered" {
		t.Fatalf("Resolve returned %v", err)
	}
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
