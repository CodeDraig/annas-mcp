package mirror

import (
	"strings"
	"testing"
)

func TestParseStatusPageExtractsFlatMonitorCandidates(t *testing.T) {
	t.Parallel()

	html := flatMonitorPage(
		flatMonitorCard("https://annas-archive.gl", "UP", "900", "120"),
		flatMonitorCard("https://annas-archive.pk", "PROTECTED", "", "530"),
		flatMonitorCard("https://annas-archive.gd", "DEGRADED", "800", "640"),
	)
	candidates, err := ParseStatusPageHTML(html)
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}
	if candidates[0].BaseURL != "annas-archive.gl" || candidates[0].Status != StatusUp || candidates[0].LatencyMillis != 120 {
		t.Fatalf("unexpected first candidate: %+v", candidates[0])
	}
	if candidates[1].BaseURL != "annas-archive.pk" || candidates[1].Status != StatusProtected || candidates[1].LatencyMillis != 530 {
		t.Fatalf("unexpected second candidate: %+v", candidates[1])
	}
	if candidates[2].Status != StatusDegraded || candidates[2].LatencyMillis != 640 {
		t.Fatalf("unexpected third candidate: %+v", candidates[2])
	}
}

func TestParseStatusPageRejectsMalformedAndUnsafeCandidates(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"missing link":   `<div class="card"><span class="status-badge up">UP</span></div>`,
		"userinfo":       flatMonitorCard("https://annas-archive.fake@attacker.example/", "UP", "", "100"),
		"alternate host": flatMonitorCard("https://attacker.example/", "UP", "", "100"),
		"software host":  flatMonitorCard("https://software.annas-archive.gl/", "UP", "", "100"),
		"port":           flatMonitorCard("https://annas-archive.fake:8443/", "UP", "", "100"),
		"path":           flatMonitorCard("https://annas-archive.fake/search", "UP", "", "100"),
		"query":          flatMonitorCard("https://annas-archive.fake/?redirect=attacker.example", "UP", "", "100"),
		"fragment":       flatMonitorCard("https://annas-archive.fake/#attacker", "UP", "", "100"),
		"insecure":       flatMonitorCard("http://annas-archive.fake/", "UP", "", "100"),
	}

	for name, card := range tests {
		name, card := name, card
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			candidates, err := ParseStatusPageHTML(flatMonitorPage(card))
			if err != nil {
				t.Fatalf("ParseStatusPageHTML returned error: %v", err)
			}
			if len(candidates) != 0 {
				t.Fatalf("expected candidate to be rejected, got %+v", candidates)
			}
		})
	}
}

func TestParseStatusPageDeduplicatesAndMarksUnknownStatus(t *testing.T) {
	t.Parallel()

	html := flatMonitorPage(
		flatMonitorCard("https://annas-archive.gl", "MYSTERY", "", "invalid"),
		flatMonitorCard("https://annas-archive.gl", "UP", "", "100"),
	)
	candidates, err := ParseStatusPageHTML(html)
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Status != StatusUnknown || candidates[0].LatencyMillis != 0 {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}
}

func flatMonitorPage(cards ...string) string {
	return `<html><body>` + strings.Join(cards, "") + `</body></html>`
}

func flatMonitorCard(rawURL, status, firstLatency, latestLatency string) string {
	latencies := ""
	if firstLatency != "" {
		latencies += `<span data-status="` + status + `" data-latency="` + firstLatency + `"></span>`
	}
	if latestLatency != "" {
		latencies += `<span data-status="` + status + `" data-latency="` + latestLatency + `"></span>`
	}
	return `<div class="card">
<div class="card-header"><a class="card-title" href="` + rawURL + `">mirror</a><span class="status-badge">` + status + `</span></div>
<div class="timeline">` + latencies + `</div>
</div>`
}
