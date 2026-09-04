package mirror

import "testing"

func TestRankCandidatesUsesStatusThenLatestLatency(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{
		{BaseURL: "annas-archive.protected", Status: StatusProtected, LatencyMillis: 10},
		{BaseURL: "annas-archive.degraded", Status: StatusDegraded, LatencyMillis: 20},
		{BaseURL: "annas-archive.slow", Status: StatusUp, LatencyMillis: 500},
		{BaseURL: "annas-archive.fast", Status: StatusUp, LatencyMillis: 100},
		{BaseURL: "annas-archive.unknown-latency", Status: StatusUp},
		{BaseURL: "annas-archive.down", Status: StatusDown, LatencyMillis: 1},
		{BaseURL: "annas-archive.unknown", Status: StatusUnknown, LatencyMillis: 1},
		{BaseURL: "annas-archive.timeout", Status: StatusTimeout, LatencyMillis: 1},
	}

	ranked := rankCandidates(candidates)
	want := []string{
		"annas-archive.fast",
		"annas-archive.slow",
		"annas-archive.unknown-latency",
		"annas-archive.degraded",
		"annas-archive.protected",
	}
	if len(ranked) != len(want) {
		t.Fatalf("rankCandidates returned %d candidates, want %d", len(ranked), len(want))
	}
	for index, baseURL := range want {
		if ranked[index].BaseURL != baseURL {
			t.Fatalf("ranked[%d] = %q, want %q", index, ranked[index].BaseURL, baseURL)
		}
	}
}
