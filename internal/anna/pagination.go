package anna

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const maxSearchPage = 100

// SearchOptions selects a starting page and either a page count or all pages.
type SearchOptions struct {
	Page     int
	Pages    int
	AllPages bool
}

func (o SearchOptions) Validate() error {
	// The site's search route clamps page numbers to this range.
	if o.Page < 1 || o.Page > maxSearchPage {
		return fmt.Errorf("page must be between 1 and %d", maxSearchPage)
	}
	if o.AllPages {
		if o.Pages != 0 && o.Pages != 1 {
			return fmt.Errorf("all-pages cannot be combined with a page count")
		}
		return nil
	}
	if o.Pages < 1 {
		return fmt.Errorf("pages must be greater than zero")
	}
	return nil
}

func loadSearchPages(ctx context.Context, loader pageLoader, firstURL string, timeout time.Duration, options SearchOptions, consume func(string) (int, error)) error {
	if err := options.Validate(); err != nil {
		return err
	}
	currentURL := firstURL
	pageNumber := options.Page
	pageLimit := options.Pages
	if options.AllPages {
		pageLimit = maxSearchPage - options.Page + 1
	}
	for loaded := 0; loaded < pageLimit; loaded++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		html, err := loader.Load(ctx, currentURL, timeout)
		if err != nil {
			return fmt.Errorf("load search page %d: %w", pageNumber, err)
		}
		document, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			return fmt.Errorf("parse search page %d: %w", pageNumber, err)
		}
		// The hidden form field always says page 1, including on later pages.
		// The pagination control identifies the page that was actually returned.
		if marker := document.Find("nav[aria-label='Pagination'] a[aria-current='page']").First(); marker.Length() > 0 {
			returned, err := strconv.Atoi(strings.TrimSpace(marker.Text()))
			if err != nil || returned != pageNumber {
				return fmt.Errorf("search returned page %q instead of requested page %d", strings.TrimSpace(marker.Text()), pageNumber)
			}
		}
		count, err := consume(html)
		if err != nil {
			return fmt.Errorf("parse search results on page %d: %w", pageNumber, err)
		}
		if count == 0 || loaded == pageLimit-1 {
			return nil
		}
		next, err := nextSearchPage(document, currentURL, pageNumber)
		if err != nil {
			return fmt.Errorf("search page %d: %w", pageNumber, err)
		}
		if next == "" {
			return nil
		}
		currentURL = next
		pageNumber++
	}
	return nil
}

func nextSearchPage(document *goquery.Document, currentURL string, currentPage int) (string, error) {
	link := document.Find("a.js-pagination-next-page, a[rel~='next']").First()
	if link.Length() == 0 {
		return "", nil
	}
	href, ok := link.Attr("href")
	if !ok || href == "" {
		return "", fmt.Errorf("next-page control has no URL")
	}
	current, err := url.Parse(currentURL)
	if err != nil {
		return "", err
	}
	next, err := current.Parse(href)
	if err != nil {
		return "", fmt.Errorf("invalid next-page URL: %w", err)
	}
	if next.User != nil || next.Scheme != current.Scheme || !strings.EqualFold(next.Host, current.Host) || strings.TrimSuffix(next.Path, "/") != strings.TrimSuffix(current.Path, "/") {
		return "", fmt.Errorf("next-page URL must stay on the same search endpoint")
	}
	query := next.Query()
	page, err := strconv.Atoi(query.Get("page"))
	if err != nil || page != currentPage+1 || page > maxSearchPage {
		return "", fmt.Errorf("next-page URL does not point to page %d", currentPage+1)
	}
	searchQuery := current.Query().Get("q")
	canonicalQuery, _ := document.Find(".js-search-form input[name='q']").First().Attr("value")
	if query.Get("q") != searchQuery && (canonicalQuery == "" || query.Get("q") != canonicalQuery) {
		return "", fmt.Errorf("next-page URL changes the search query")
	}
	if strings.Join(query["content"], "\x00") != strings.Join(current.Query()["content"], "\x00") || query.Get("index") != current.Query().Get("index") {
		return "", fmt.Errorf("next-page URL changes the search filters")
	}
	next.Fragment = ""
	return next.String(), nil
}

func searchStartURL(baseURL, query string, filters url.Values, page int) string {
	parameters := url.Values{"q": {query}}
	for key, values := range filters {
		parameters[key] = values
	}
	if page != 1 {
		parameters.Set("page", strconv.Itoa(page))
	}
	return (&url.URL{Scheme: "https", Host: baseURL, Path: "/search", RawQuery: parameters.Encode()}).String()
}
