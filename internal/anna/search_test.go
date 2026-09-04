package anna

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"go.uber.org/zap"
)

type loaderResponse struct {
	html string
	err  error
}

type fakePageLoader struct {
	responses []loaderResponse
	urls      []string
	timeouts  []time.Duration
}

func (f *fakePageLoader) Load(ctx context.Context, rawURL string, timeout time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.urls = append(f.urls, rawURL)
	f.timeouts = append(f.timeouts, timeout)
	if len(f.responses) == 0 {
		return "", errors.New("unexpected page load")
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response.html, response.err
}

func TestExtractMetaInformation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		meta         string
		wantLanguage string
		wantFormat   string
		wantSize     string
	}{
		{name: "single language", meta: "✅ English [en] · EPUB · 0.7MB · 2015", wantLanguage: "English", wantFormat: "EPUB", wantSize: "0.7MB"},
		{name: "multiple languages", meta: "✅ English [en] · Hindi [hi] · PDF · 2 MB", wantLanguage: "English", wantFormat: "PDF", wantSize: "2 MB"},
		{name: "incomplete", meta: "English", wantLanguage: "", wantFormat: "", wantSize: ""},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			language, format, size := extractMetaInformation(test.meta)
			if language != test.wantLanguage || format != test.wantFormat || size != test.wantSize {
				t.Fatalf("extractMetaInformation(%q) = (%q, %q, %q), want (%q, %q, %q)", test.meta, language, format, size, test.wantLanguage, test.wantFormat, test.wantSize)
			}
		})
	}
}

func TestParseBookElement(t *testing.T) {
	t.Parallel()

	element := searchElement(t, searchResultHTML("bookhash", "Book Title", "Book Author", "Book Publisher", "✅ English [en] · EPUB · 0.7MB"))
	book, ok := parseBookElement(element, mustBaseURL(t), zap.NewNop())
	if !ok {
		t.Fatal("parseBookElement rejected a complete result")
	}
	if book.Title != "Book Title" || book.Authors != "Book Author" || book.Publisher != "Book Publisher" {
		t.Fatalf("unexpected text fields: %+v", book)
	}
	if book.Language != "English" || book.Format != "EPUB" || book.Size != "0.7MB" || book.Hash != "bookhash" {
		t.Fatalf("unexpected metadata fields: %+v", book)
	}
	if book.URL != "https://annas-archive.test/md5/bookhash" {
		t.Fatalf("unexpected URL: %q", book.URL)
	}
}

func TestParsePaperElement(t *testing.T) {
	t.Parallel()

	element := searchElement(t, searchResultHTML("paperhash", "Paper Title", "Paper Author", "Journal Name", "✅ English [en] · PDF · 1.2MB"))
	paper, ok := parsePaperElement(element, mustBaseURL(t), zap.NewNop())
	if !ok {
		t.Fatal("parsePaperElement rejected a complete result")
	}
	if paper.Title != "Paper Title" || paper.Authors != "Paper Author" || paper.Journal != "Journal Name" {
		t.Fatalf("unexpected text fields: %+v", paper)
	}
	if paper.Size != "1.2MB" || paper.Hash != "paperhash" || paper.PageURL != "https://annas-archive.test/md5/paperhash" {
		t.Fatalf("unexpected metadata fields: %+v", paper)
	}
}

func TestSearchElementParsersRejectMissingInfoContainer(t *testing.T) {
	t.Parallel()

	element := searchElement(t, `<div><a id="target" href="/md5/hash"></a></div>`)
	if book, ok := parseBookElement(element, mustBaseURL(t), zap.NewNop()); ok || book != nil {
		t.Fatalf("parseBookElement accepted incomplete result: %+v", book)
	}
	if paper, ok := parsePaperElement(element, mustBaseURL(t), zap.NewNop()); ok || paper != nil {
		t.Fatalf("parsePaperElement accepted incomplete result: %+v", paper)
	}
}

func TestFindBooksLoadsAndParsesVerifiedHTML(t *testing.T) {
	t.Parallel()

	loader := &fakePageLoader{responses: []loaderResponse{{html: searchResultHTML("bookhash", "Book Title", "Book Author", "Book Publisher", "✅ English [en] · EPUB · 0.7MB")}}}
	books, err := findBooksAtBaseURL(context.Background(), loader, "annas-archive.test", "machine learning", 3*time.Second)
	if err != nil {
		t.Fatalf("findBooksAtBaseURL returned error: %v", err)
	}
	if len(books) != 1 || books[0].Hash != "bookhash" {
		t.Fatalf("unexpected books: %+v", books)
	}
	if len(loader.urls) != 1 || loader.urls[0] != "https://annas-archive.test/search?q=machine+learning&content=book_any" {
		t.Fatalf("unexpected loader URLs: %v", loader.urls)
	}
	if loader.timeouts[0] != 3*time.Second {
		t.Fatalf("unexpected timeout: %s", loader.timeouts[0])
	}
}

func TestFindArticlesLoadsAndParsesVerifiedHTML(t *testing.T) {
	t.Parallel()

	loader := &fakePageLoader{responses: []loaderResponse{{html: searchResultHTML("paperhash", "Paper Title", "Paper Author", "Journal Name", "✅ English [en] · PDF · 1.2MB")}}}
	papers, err := findArticlesAtBaseURL(context.Background(), loader, "annas-archive.test", "graph theory", time.Second)
	if err != nil {
		t.Fatalf("findArticlesAtBaseURL returned error: %v", err)
	}
	if len(papers) != 1 || papers[0].Hash != "paperhash" {
		t.Fatalf("unexpected papers: %+v", papers)
	}
	if len(loader.urls) != 1 || !strings.Contains(loader.urls[0], "content=journal") {
		t.Fatalf("unexpected loader URLs: %v", loader.urls)
	}
}

func TestSearchReturnsEmptyOnlyAfterSuccessfulPageLoad(t *testing.T) {
	t.Parallel()

	emptyLoader := &fakePageLoader{responses: []loaderResponse{{html: `<html><title>No results</title></html>`}}}
	books, err := findBooksAtBaseURL(context.Background(), emptyLoader, "annas-archive.test", "missing", time.Second)
	if err != nil || len(books) != 0 {
		t.Fatalf("successful empty search returned books=%+v err=%v", books, err)
	}

	failingLoader := &fakePageLoader{responses: []loaderResponse{{err: errors.New("HTTP 403")}}}
	books, err = findBooksAtBaseURL(context.Background(), failingLoader, "annas-archive.test", "missing", time.Second)
	if err == nil || books != nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("failed search returned books=%+v err=%v", books, err)
	}
}

func searchElement(t *testing.T, html string) *goquery.Selection {
	t.Helper()
	document, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	element := document.Find("#target")
	if element.Length() == 0 {
		t.Fatal("fixture did not contain #target")
	}
	return element
}

func searchResultHTML(hash, title, author, publisher, meta string) string {
	return `<div>
<a id="target" href="/md5/` + hash + `"></a>
<div class="max-w-full">
  <a href="/md5/` + hash + `"> ` + title + ` </a>
  <a href="/search"><span class="icon-[mdi--user-edit]"></span> ` + author + ` </a>
  <a href="/search"><span class="icon-[mdi--company]"></span> ` + publisher + ` </a>
  <div class="text-gray-800">` + meta + `</div>
</div>
</div>`
}

func mustBaseURL(t *testing.T) *url.URL {
	t.Helper()
	baseURL, err := url.Parse("https://annas-archive.test/")
	if err != nil {
		t.Fatalf("parse base URL: %v", err)
	}
	return baseURL
}
