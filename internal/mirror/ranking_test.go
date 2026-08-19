package mirror

import (
	"testing"
	"time"
)

func TestCandidateScoreUsesRecentHeartbeatQuality(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 23, 11, 0, 0, 0, time.UTC)
	candidate := Candidate{
		BaseURL: "annas-archive.good",
		Heartbeats: []Heartbeat{
			{Status: 1, Ping: 200, Time: now.Add(-2 * time.Minute)},
			{Status: 1, Ping: 100, Time: now.Add(-1 * time.Minute)},
			{Status: 0, Ping: 0, Time: now},
		},
	}

	score := candidate.Score()
	if score.SuccessCount != 2 {
		t.Fatalf("expected 2 successful heartbeats, got %d", score.SuccessCount)
	}
	if score.LastStatus != 0 {
		t.Fatalf("expected last status 0, got %d", score.LastStatus)
	}
	if score.AveragePing != 150 {
		t.Fatalf("expected average ping 150, got %d", score.AveragePing)
	}
}

func TestRankCandidatesFiltersDownAndOrdersHealthyCandidates(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{
		{BaseURL: "annas-archive.down", Heartbeats: []Heartbeat{{Status: 0, Ping: 0}}},
		{BaseURL: "annas-archive.slow", Heartbeats: []Heartbeat{{Status: 1, Ping: 500}}},
		{BaseURL: "annas-archive.fast", Heartbeats: []Heartbeat{{Status: 1, Ping: 100}}},
		{BaseURL: "annas-archive.unknown"},
	}
	ranked := rankCandidates(candidates)
	if len(ranked) != 3 {
		t.Fatalf("rankCandidates returned %d candidates", len(ranked))
	}
	want := []string{"annas-archive.fast", "annas-archive.slow", "annas-archive.unknown"}
	for index, baseURL := range want {
		if ranked[index].BaseURL != baseURL {
			t.Fatalf("ranked[%d] = %q, want %q", index, ranked[index].BaseURL, baseURL)
		}
	}
}
