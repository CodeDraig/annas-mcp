package anna

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func searchPaginationHTML(page int, next string) string {
	result := fmt.Sprintf(`<nav aria-label="Pagination"><a aria-current="page">%d</a>`, page)
	if next != "" {
		result += `<a class="js-pagination-next-page" href="` + html.EscapeString(next) + `">Next</a>`
	}
	return result + `</nav>`
}

func TestBookSearchPaginationCombinesUniqueResults(t *testing.T) {
	t.Parallel()
	query := "logic & 100% λ"
	filters := url.Values{"content": {"book_nonfiction", "book_fiction", "book_unknown"}}
	second := searchStartURL("annas-archive.test", query, filters, 3)
	firstHTML := searchResultHTML("one", "First", "Author", "Publisher", "✅ English [en] · EPUB · 1MB") +
		searchPaginationHTML(2, second)
	secondHTML := searchResultHTML("one", "First duplicate", "Author", "Publisher", "✅ English [en] · EPUB · 1MB") +
		searchResultHTML("two", "Second", "Author", "Publisher", "✅ English [en] · PDF · 2MB") +
		searchPaginationHTML(3, "")
	loader := &fakePageLoader{responses: []loaderResponse{{html: firstHTML}, {html: secondHTML}}}
	books, err := findBookPagesAtBaseURL(context.Background(), loader, "annas-archive.test", query, time.Second, SearchOptions{Page: 2, Pages: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 || books[0].Hash != "one" || books[0].Title != "First" || books[1].Hash != "two" {
		t.Fatalf("unexpected combined books: %+v", books)
	}
	if len(loader.urls) != 2 {
		t.Fatalf("requested %d pages, want two before the site's last page", len(loader.urls))
	}
	for i, raw := range loader.urls {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Query().Get("q") != query || parsed.Query().Get("page") != fmt.Sprint(i+2) {
			t.Fatalf("URL did not preserve query/page: %q, %v", raw, err)
		}
		if loader.timeouts[i] != time.Second {
			t.Fatalf("page timeout=%s, want one second", loader.timeouts[i])
		}
	}
}

func TestArticleSearchPaginationStopsAtRequestedCount(t *testing.T) {
	t.Parallel()
	filters := url.Values{"index": {"journals"}}
	page := func(n int) string {
		return searchResultHTML(fmt.Sprint(n), "Paper", "Author", "Journal", "✅ English [en] · PDF · 2MB") +
			searchPaginationHTML(n, searchStartURL("annas-archive.test", "graphs", filters, n+1))
	}
	loader := &fakePageLoader{responses: []loaderResponse{{html: page(1)}, {html: page(2)}}}
	papers, err := findArticlePagesAtBaseURL(context.Background(), loader, "annas-archive.test", "graphs", time.Second, SearchOptions{Page: 1, Pages: 2})
	if err != nil || len(papers) != 2 || len(loader.urls) != 2 || papers[1].Hash != "2" {
		t.Fatalf("papers=%+v URLs=%v err=%v", papers, loader.urls, err)
	}
	if !strings.Contains(loader.urls[0], "index=journals") {
		t.Fatalf("article search did not use journals index: %v", loader.urls)
	}
}

func TestPaginatedSearchFailureDoesNotReturnPartialSuccess(t *testing.T) {
	t.Parallel()
	next := searchStartURL("annas-archive.test", "books", url.Values{"content": {"book_nonfiction", "book_fiction", "book_unknown"}}, 2)
	loader := &fakePageLoader{responses: []loaderResponse{
		{html: searchResultHTML("one", "Book", "Author", "Publisher", "✅ English [en] · PDF · 1MB") + searchPaginationHTML(1, next)},
		{err: errors.New("HTTP 503")},
	}}
	books, err := findBookPagesAtBaseURL(context.Background(), loader, "annas-archive.test", "books", time.Second, SearchOptions{Page: 1, Pages: 2})
	if err == nil || books != nil || !strings.Contains(err.Error(), "page 2") || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("partial failure: books=%+v err=%v", books, err)
	}
}

func TestSearchPaginationValidatesPageReturnedBySite(t *testing.T) {
	t.Parallel()
	loader := &fakePageLoader{responses: []loaderResponse{{html: searchPaginationHTML(1, "")}}}
	books, err := findBookPagesAtBaseURL(context.Background(), loader, "annas-archive.test", "books", time.Second, SearchOptions{Page: 2, Pages: 1})
	if err == nil || books != nil || !strings.Contains(err.Error(), "instead of requested page 2") {
		t.Fatalf("site ignored requested page: books=%+v err=%v", books, err)
	}
}

func TestNextSearchPageRejectsChangedTargetAndLoops(t *testing.T) {
	t.Parallel()
	current := "https://annas-archive.test/search?q=books&content=book_fiction"
	for _, href := range []string{
		"https://other.test/search?q=books&content=book_fiction&page=2",
		"http://annas-archive.test/search?q=books&content=book_fiction&page=2",
		"/other?q=books&content=book_fiction&page=2",
		"/search?q=books&content=book_fiction&page=1",
		"/search?q=books&content=book_fiction&page=3",
		"/search?q=changed&content=book_fiction&page=2",
		"/search?q=books&content=book_unknown&page=2",
		"/search?q=books&content=book_fiction&index=journals&page=2",
	} {
		document, err := goquery.NewDocumentFromReader(strings.NewReader(searchPaginationHTML(1, href)))
		if err != nil {
			t.Fatal(err)
		}
		if next, err := nextSearchPage(document, current, 1); err == nil || next != "" {
			t.Errorf("accepted changed next page %q: next=%q err=%v", href, next, err)
		}
	}
}

func TestSearchPaginationRejectsInvalidOptionsBeforeLoading(t *testing.T) {
	t.Parallel()
	for _, options := range []SearchOptions{{Page: 0, Pages: 1}, {Page: -1, Pages: 1}, {Page: 101, Pages: 1}, {Page: 1, Pages: 0}, {Page: 1, Pages: -1}} {
		loader := &fakePageLoader{}
		if _, err := findBookPagesAtBaseURL(context.Background(), loader, "annas-archive.test", "books", time.Second, options); err == nil {
			t.Errorf("accepted options %+v", options)
		}
		if len(loader.urls) != 0 {
			t.Errorf("invalid options loaded URLs: %v", loader.urls)
		}
	}
}

func TestSearchPaginationCancellationDoesNotLoadAnotherPage(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := "https://annas-archive.test/search?q=books"
	loader := &fakePageLoader{responses: []loaderResponse{{html: searchPaginationHTML(1, first+"&page=2")}}}
	err := loadSearchPages(ctx, loader, first, time.Second, SearchOptions{Page: 1, Pages: 3}, func(string) (int, error) {
		cancel()
		return 1, nil
	})
	if !errors.Is(err, context.Canceled) || len(loader.urls) != 1 {
		t.Fatalf("cancellation err=%v URLs=%v", err, loader.urls)
	}
}
