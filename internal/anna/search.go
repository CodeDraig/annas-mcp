package anna

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/iosifache/annas-mcp/internal/env"
	"github.com/iosifache/annas-mcp/internal/logger"
	"github.com/iosifache/annas-mcp/internal/webpage"
	"go.uber.org/zap"
)

type pageLoader interface {
	Load(context.Context, string, time.Duration) (string, error)
}

var defaultPageLoader pageLoader = webpage.NewLoader(nil)

func extractMetaInformation(meta string) (language, format, size string) {
	parts := strings.Split(meta, " · ")
	if len(parts) < 3 {
		return "", "", ""
	}

	languagePart := strings.TrimSpace(parts[0])
	if idx := strings.Index(languagePart, "["); idx > 0 {
		language = strings.TrimSpace(languagePart[:idx])
		language = strings.TrimPrefix(language, "✅")
		language = strings.TrimSpace(language)
	}

	formatRegex := regexp.MustCompile(`(?i)\b(EPUB|PDF|MOBI|AZW3|AZW|DJVU|CBZ|CBR|FB2|DOCX?|TXT)\b`)
	sizeRegex := regexp.MustCompile(`\d+\.?\d*\s*(MB|KB|GB|TB)`)

	for i := 1; i < len(parts); i++ {
		part := strings.TrimSpace(parts[i])
		if size == "" && sizeRegex.MatchString(part) {
			size = part
		}
		if format == "" && formatRegex.MatchString(part) {
			matches := formatRegex.FindStringSubmatch(part)
			if len(matches) > 0 {
				format = strings.ToUpper(matches[1])
			}
		}
		if format != "" && size != "" {
			break
		}
	}

	return language, format, size
}

func FindBook(ctx context.Context, query string, timeout time.Duration) ([]*Book, error) {
	return findBooks(ctx, defaultPageLoader, query, timeout)
}

func findBooks(ctx context.Context, loader pageLoader, query string, timeout time.Duration) ([]*Book, error) {
	annasBaseURL, err := env.GetAnnasBaseURL(ctx, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to load Anna configuration: %w", err)
	}
	return findBooksAtBaseURL(ctx, loader, annasBaseURL, query, timeout)
}

func findBooksAtBaseURL(ctx context.Context, loader pageLoader, annasBaseURL, query string, timeout time.Duration) ([]*Book, error) {
	l := logger.GetLogger()
	fullURL := fmt.Sprintf(AnnasSearchEndpointFormat, annasBaseURL, url.QueryEscape(query), "book_any")

	html, err := loader.Load(ctx, fullURL, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to load book search page: %w", err)
	}
	books, totalElements, err := parseBookSearchHTML(html, annasBaseURL, l)
	if err != nil {
		return nil, fmt.Errorf("failed to parse book search page: %w", err)
	}

	l.Info("Search completed",
		zap.Int("totalElements", totalElements),
		zap.Int("validBooks", len(books)),
	)
	return books, nil
}

func parseBookSearchHTML(html, annasBaseURL string, l *zap.Logger) ([]*Book, int, error) {
	document, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, 0, err
	}
	baseURL, err := url.Parse("https://" + annasBaseURL + "/")
	if err != nil {
		return nil, 0, err
	}

	books := make([]*Book, 0)
	seen := make(map[string]struct{})
	totalElements := 0
	document.Find("a[href^='/md5/']").Each(func(_ int, element *goquery.Selection) {
		book, ok := parseBookElement(element, baseURL, l)
		if !ok {
			return
		}
		totalElements++
		if _, exists := seen[book.Hash]; exists {
			return
		}
		seen[book.Hash] = struct{}{}
		books = append(books, book)
	})
	return books, totalElements, nil
}

func parseBookElement(element *goquery.Selection, baseURL *url.URL, l *zap.Logger) (*Book, bool) {
	parent := element.Parent()
	if parent.Length() == 0 {
		l.Warn("Skipping book: no parent element found")
		return nil, false
	}

	bookInfoDiv := parent.Find("div.max-w-full").First()
	if bookInfoDiv.Length() == 0 {
		return nil, false
	}

	title := strings.TrimSpace(bookInfoDiv.Find("a[href^='/md5/']").First().Text())
	if title == "" {
		l.Warn("Skipping book: title is empty")
		return nil, false
	}

	authors := strings.TrimSpace(bookInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--user-edit\\]").Parent().First().Text())
	publisher := strings.TrimSpace(bookInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--company\\]").Parent().First().Text())
	meta := bookInfoDiv.Find("div.text-gray-800").First().Text()
	language, format, size := extractMetaInformation(meta)

	link, ok := element.Attr("href")
	if !ok || link == "" {
		l.Warn("Skipping book: no link found", zap.String("title", title))
		return nil, false
	}
	hash := strings.TrimPrefix(link, "/md5/")
	if hash == "" {
		l.Warn("Skipping book: no hash found", zap.String("title", title))
		return nil, false
	}
	absoluteURL := baseURL.ResolveReference(&url.URL{Path: link}).String()

	return &Book{
		Language:  language,
		Format:    format,
		Size:      size,
		Title:     title,
		Publisher: publisher,
		Authors:   authors,
		URL:       absoluteURL,
		Hash:      hash,
	}, true
}

func FindArticle(ctx context.Context, query string, timeout time.Duration) ([]*Paper, error) {
	return findArticles(ctx, defaultPageLoader, query, timeout)
}

func findArticles(ctx context.Context, loader pageLoader, query string, timeout time.Duration) ([]*Paper, error) {
	annasBaseURL, err := env.GetAnnasBaseURL(ctx, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to load Anna configuration: %w", err)
	}
	return findArticlesAtBaseURL(ctx, loader, annasBaseURL, query, timeout)
}

func findArticlesAtBaseURL(ctx context.Context, loader pageLoader, annasBaseURL, query string, timeout time.Duration) ([]*Paper, error) {
	l := logger.GetLogger()
	fullURL := fmt.Sprintf(AnnasSearchEndpointFormat, annasBaseURL, url.QueryEscape(query), "journal")

	html, err := loader.Load(ctx, fullURL, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to load article search page: %w", err)
	}
	papers, totalElements, err := parseArticleSearchHTML(html, annasBaseURL, l)
	if err != nil {
		return nil, fmt.Errorf("failed to parse article search page: %w", err)
	}

	l.Info("Article search completed",
		zap.Int("totalElements", totalElements),
		zap.Int("validPapers", len(papers)),
	)
	return papers, nil
}

func parseArticleSearchHTML(html, annasBaseURL string, l *zap.Logger) ([]*Paper, int, error) {
	document, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, 0, err
	}
	baseURL, err := url.Parse("https://" + annasBaseURL + "/")
	if err != nil {
		return nil, 0, err
	}

	papers := make([]*Paper, 0)
	seen := make(map[string]struct{})
	totalElements := 0
	document.Find("a[href^='/md5/']").Each(func(_ int, element *goquery.Selection) {
		paper, ok := parsePaperElement(element, baseURL, l)
		if !ok {
			return
		}
		totalElements++
		if _, exists := seen[paper.Hash]; exists {
			return
		}
		seen[paper.Hash] = struct{}{}
		papers = append(papers, paper)
	})
	return papers, totalElements, nil
}

func parsePaperElement(element *goquery.Selection, baseURL *url.URL, l *zap.Logger) (*Paper, bool) {
	parent := element.Parent()
	if parent.Length() == 0 {
		l.Warn("Skipping article: no parent element found")
		return nil, false
	}

	paperInfoDiv := parent.Find("div.max-w-full").First()
	if paperInfoDiv.Length() == 0 {
		return nil, false
	}

	title := strings.TrimSpace(paperInfoDiv.Find("a[href^='/md5/']").First().Text())
	if title == "" {
		l.Warn("Skipping article: title is empty")
		return nil, false
	}

	authors := strings.TrimSpace(paperInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--user-edit\\]").Parent().First().Text())
	journal := strings.TrimSpace(paperInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--company\\]").Parent().First().Text())
	meta := paperInfoDiv.Find("div.text-gray-800").First().Text()
	_, _, size := extractMetaInformation(meta)

	link, ok := element.Attr("href")
	if !ok || link == "" {
		l.Warn("Skipping article: no link found", zap.String("title", title))
		return nil, false
	}
	hash := strings.TrimPrefix(link, "/md5/")
	if hash == "" {
		l.Warn("Skipping article: no hash found", zap.String("title", title))
		return nil, false
	}
	absoluteURL := baseURL.ResolveReference(&url.URL{Path: link}).String()

	return &Paper{
		Title:   title,
		Authors: authors,
		Journal: journal,
		Size:    size,
		Hash:    hash,
		PageURL: absoluteURL,
	}, true
}
