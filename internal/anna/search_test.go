package anna

import (
	"net/http"
	"net/http/httptest"
	"testing"

	colly "github.com/gocolly/colly/v2"
	"go.uber.org/zap"
)

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

	element := searchElement(t, `<div>
<a id="target" href="/md5/bookhash"></a>
<div class="max-w-full">
  <a href="/md5/bookhash"> Book Title </a>
  <a href="/search"><span class="icon-[mdi--user-edit]"></span> Book Author </a>
  <a href="/search"><span class="icon-[mdi--company]"></span> Book Publisher </a>
  <div class="text-gray-800">✅ English [en] · EPUB · 0.7MB</div>
</div>
</div>`)

	book, ok := parseBookElement(element, zap.NewNop())
	if !ok {
		t.Fatal("parseBookElement rejected a complete result")
	}
	if book.Title != "Book Title" || book.Authors != "Book Author" || book.Publisher != "Book Publisher" {
		t.Fatalf("unexpected text fields: %+v", book)
	}
	if book.Language != "English" || book.Format != "EPUB" || book.Size != "0.7MB" || book.Hash != "bookhash" {
		t.Fatalf("unexpected metadata fields: %+v", book)
	}
	if book.URL == "" {
		t.Fatal("expected an absolute book URL")
	}
}

func TestParsePaperElement(t *testing.T) {
	t.Parallel()

	element := searchElement(t, `<div>
<a id="target" href="/md5/paperhash"></a>
<div class="max-w-full">
  <a href="/md5/paperhash"> Paper Title </a>
  <a href="/search"><span class="icon-[mdi--user-edit]"></span> Paper Author </a>
  <a href="/search"><span class="icon-[mdi--company]"></span> Journal Name </a>
  <div class="text-gray-800">✅ English [en] · PDF · 1.2MB</div>
</div>
</div>`)

	paper, ok := parsePaperElement(element, zap.NewNop())
	if !ok {
		t.Fatal("parsePaperElement rejected a complete result")
	}
	if paper.Title != "Paper Title" || paper.Authors != "Paper Author" || paper.Journal != "Journal Name" {
		t.Fatalf("unexpected text fields: %+v", paper)
	}
	if paper.Size != "1.2MB" || paper.Hash != "paperhash" || paper.PageURL == "" {
		t.Fatalf("unexpected metadata fields: %+v", paper)
	}
}

func TestSearchElementParsersRejectMissingInfoContainer(t *testing.T) {
	t.Parallel()

	element := searchElement(t, `<div><a id="target" href="/md5/hash"></a></div>`)
	if book, ok := parseBookElement(element, zap.NewNop()); ok || book != nil {
		t.Fatalf("parseBookElement accepted incomplete result: %+v", book)
	}
	if paper, ok := parsePaperElement(element, zap.NewNop()); ok || paper != nil {
		t.Fatalf("parsePaperElement accepted incomplete result: %+v", paper)
	}
}

func searchElement(t *testing.T, html string) *colly.HTMLElement {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(server.Close)

	collector := colly.NewCollector()
	var element *colly.HTMLElement
	collector.OnHTML("#target", func(found *colly.HTMLElement) {
		element = found
	})
	if err := collector.Visit(server.URL); err != nil {
		t.Fatalf("collect fixture: %v", err)
	}
	if element == nil {
		t.Fatal("fixture did not contain #target")
	}
	return element
}
