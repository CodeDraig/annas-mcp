package mirror

import (
	"strings"
	"testing"
)

func TestParseStatusPageExtractsAnnaCandidatesWithoutHardcodedIDs(t *testing.T) {
	t.Parallel()

	html := `
<html><body>
<script>
window.preloadData = {'config':{'slug':'slum'},'incident':null,'publicGroupList':[
  {'id':1,'name':'Anna\'s Archive','monitorList':[
    {'id':900,'name':'Anna A','sendUrl':1,'type':'keyword','url':'https://annas-archive.zz/'},
    {'id':901,'name':'Anna B','sendUrl':1,'type':'keyword','url':'https://annas-archive.yy/'}
  ]},
  {'id':2,'name':'Others','monitorList':[
    {'id':999,'name':'Not Anna','sendUrl':1,'type':'keyword','url':'https://example.org/'}
  ]}
],'maintenanceList':[]};
</script>
</body></html>`

	candidates, err := ParseStatusPageHTML(html)
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 Anna candidates, got %d", len(candidates))
	}
	if candidates[0].MonitorID != 900 || candidates[0].BaseURL != "annas-archive.zz" {
		t.Fatalf("unexpected first candidate: %+v", candidates[0])
	}
	if candidates[1].MonitorID != 901 || candidates[1].BaseURL != "annas-archive.yy" {
		t.Fatalf("unexpected second candidate: %+v", candidates[1])
	}
}

func TestParseStatusPageUsesCurrentURLFallback(t *testing.T) {
	t.Parallel()

	candidates, err := ParseStatusPageHTML(`<a href="https://annas-archive.fallback/">mirror</a>`)
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	if len(candidates) != 1 || candidates[0].BaseURL != "annas-archive.fallback" || candidates[0].MonitorID != 0 {
		t.Fatalf("unexpected fallback candidates: %+v", candidates)
	}
}

func TestParseStatusPageRejectsUnsafeAnnaCandidateURLs(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"userinfo":       "https://annas-archive.fake@attacker.example/",
		"alternate host": "https://attacker.example/",
		"port":           "https://annas-archive.fake:8443/",
		"path":           "https://annas-archive.fake/search",
		"query":          "https://annas-archive.fake/?redirect=attacker.example",
		"fragment":       "https://annas-archive.fake/#attacker",
		"insecure":       "http://annas-archive.fake/",
	}

	for name, candidateURL := range tests {
		name, candidateURL := name, candidateURL
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			html := strings.ReplaceAll(`
<script>
window.preloadData = {'config':{'slug':'slum'},'publicGroupList':[
  {'id':1,'name':'Anna\'s Archive','monitorList':[
    {'id':900,'name':'Unsafe','url':'CANDIDATE_URL'}
  ]}
]};
</script>`, "CANDIDATE_URL", candidateURL)

			candidates, err := ParseStatusPageHTML(html)
			if err != nil {
				t.Fatalf("ParseStatusPageHTML returned error: %v", err)
			}
			if len(candidates) != 0 {
				t.Fatalf("expected unsafe candidate to be rejected, got %+v", candidates)
			}
		})
	}
}
