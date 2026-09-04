package anna

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLookupDOIPreservesIdentifierInURLs(t *testing.T) {
	t.Parallel()
	for _, doi := range []string{
		"10.1234/plain",
		"10.1234/a#b",
		"10.1234/a?b",
		"10.1234/a&other=value",
		"10.1234/a+b",
		"10.1234/100%",
		"10.1234/literal%23",
		"10.1234/nested/path",
		"10.1234/café",
		"10.1002/(SICI)1521-3951(199911)216:1<135::AID-PSSB135>3.0.CO;2-#",
	} {
		t.Run(doi, func(t *testing.T) {
			loader := &fakePageLoader{responses: []loaderResponse{
				{html: `<a href="/md5/paperhash">Paper</a>`},
				{html: `<title>Paper - Anna's Archive</title>`},
			}}
			paper, err := lookupDOIAtBaseURL(context.Background(), loader, "annas-archive.test", doi, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if paper.DOI != doi {
				t.Fatalf("metadata DOI = %q, want %q", paper.DOI, doi)
			}
			lookup, err := url.Parse(loader.urls[0])
			if err != nil {
				t.Fatal(err)
			}
			if lookup.Scheme != "https" || lookup.Host != "annas-archive.test" || lookup.Path != "/scidb/"+doi || lookup.RawQuery != "" || lookup.Fragment != "" {
				t.Errorf("lookup changed identifier: %+v", lookup)
			}
			if paper.PageURL != loader.urls[0] {
				t.Error("page URL differs from requested URL")
			}
			fallback, err := url.Parse(paper.DownloadURL)
			if err != nil {
				t.Fatal(err)
			}
			query, err := url.ParseQuery(fallback.RawQuery)
			if err != nil {
				t.Fatal(err)
			}
			if fallback.Path != "/scidb" || fallback.Fragment != "" || len(query) != 1 || len(query["doi"]) != 1 || query.Get("doi") != doi {
				t.Errorf("fallback changed identifier: URL=%q query=%v", paper.DownloadURL, query)
			}
		})
	}
}

func TestLookupDOILoadsSearchAndDetailPages(t *testing.T) {
	t.Parallel()

	loader := &fakePageLoader{responses: []loaderResponse{
		{html: `<html><a href="/md5/paperhash">paper</a></html>`},
		{html: `<html><head><title>A Paper - Anna's Archive</title><meta name="description" content="Authors&#10;&#10;Publisher&#10;&#10;Journal Name"></head><body><a href="/search"><span class="icon-[mdi--user-edit]"></span> Paper Author</a><div class="text-gray-500">1.2 MB</div></body></html>`},
	}}
	paper, err := lookupDOIAtBaseURL(context.Background(), loader, "annas-archive.test", "10.1234/example", time.Second)
	if err != nil {
		t.Fatalf("lookupDOIAtBaseURL returned error: %v", err)
	}
	if paper.Hash != "paperhash" || paper.Title != "A Paper" || paper.Authors != "Paper Author" || paper.Journal != "Journal Name" || paper.Size != "1.2 MB" {
		t.Fatalf("unexpected paper: %+v", paper)
	}
	if paper.DownloadURL != "/scidb?doi=10.1234%2Fexample" || len(loader.urls) != 2 {
		t.Fatalf("unexpected lookup state: paper=%+v urls=%v", paper, loader.urls)
	}
}

func TestLookupDOIPropagatesSearchFailure(t *testing.T) {
	t.Parallel()

	loader := &fakePageLoader{responses: []loaderResponse{{err: errors.New("browser verification timed out")}}}
	paper, err := lookupDOIAtBaseURL(context.Background(), loader, "annas-archive.test", "10.1234/example", time.Second)
	if err == nil || paper != nil || !strings.Contains(err.Error(), "browser verification timed out") {
		t.Fatalf("lookup returned paper=%+v err=%v", paper, err)
	}
}

func TestLookupDOIPreservesHashWhenDetailEnrichmentFails(t *testing.T) {
	t.Parallel()

	loader := &fakePageLoader{responses: []loaderResponse{
		{html: `<html><a href="/md5/paperhash">paper</a></html>`},
		{err: errors.New("detail unavailable")},
	}}
	paper, err := lookupDOIAtBaseURL(context.Background(), loader, "annas-archive.test", "10.1234/example", time.Second)
	if err != nil {
		t.Fatalf("lookupDOIAtBaseURL returned error: %v", err)
	}
	if paper.Hash != "paperhash" || paper.Title != "" {
		t.Fatalf("unexpected partial paper: %+v", paper)
	}
}

func TestLookupDOIReturnsNotFoundAfterSuccessfulPage(t *testing.T) {
	t.Parallel()

	loader := &fakePageLoader{responses: []loaderResponse{{html: `<html><title>No paper</title></html>`}}}
	paper, err := lookupDOIAtBaseURL(context.Background(), loader, "annas-archive.test", "10.1234/missing", time.Second)
	if err == nil || paper != nil || !strings.Contains(err.Error(), "no paper found") {
		t.Fatalf("lookup returned paper=%+v err=%v", paper, err)
	}
}

func TestPaperTitleFromPageTitle(t *testing.T) {
	t.Parallel()

	if got := paperTitleFromPageTitle("A Paper - Anna's Archive"); got != "A Paper" {
		t.Fatalf("paperTitleFromPageTitle returned %q", got)
	}
	if got := paperTitleFromPageTitle("Unrecognized title"); got != "" {
		t.Fatalf("paperTitleFromPageTitle returned %q for unmatched title", got)
	}
}

func TestJournalFromDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		description string
		want        string
	}{
		{description: "Authors\n\nPublisher\n\nJournal, volume, year", want: "Journal, volume, year"},
		{description: "Authors\n\nPublisher", want: "Publisher"},
		{description: "Journal only", want: "Journal only"},
	}
	for _, test := range tests {
		if got := journalFromDescription(test.description); got != test.want {
			t.Errorf("journalFromDescription(%q) = %q, want %q", test.description, got, test.want)
		}
	}
}
