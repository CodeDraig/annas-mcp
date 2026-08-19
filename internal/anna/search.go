package anna

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	colly "github.com/gocolly/colly/v2"
	"github.com/iosifache/annas-mcp/internal/env"
	"github.com/iosifache/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

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

func FindBook(query string, timeout time.Duration) ([]*Book, error) {
	l := logger.GetLogger()

	var bookListMutex sync.Mutex
	bookList := make([]*colly.HTMLElement, 0)

	c := colly.NewCollector(
		colly.Async(true),
		colly.UserAgent(BrowserUserAgent),
	)
	c.SetRequestTimeout(timeout)

	c.OnHTML("a[href^='/md5/']", func(e *colly.HTMLElement) {
		if e.Attr("class") == "custom-a block mr-2 sm:mr-4 hover:opacity-80" {
			bookListMutex.Lock()
			bookList = append(bookList, e)
			bookListMutex.Unlock()
		}
	})

	c.OnRequest(func(r *colly.Request) {
		l.Info("Visiting URL", zap.String("url", r.URL.String()))
	})

	c.OnError(func(r *colly.Response, err error) {
		status := 0
		if r != nil {
			status = r.StatusCode
		}
		l.Error("Search request failed",
			zap.Int("statusCode", status),
			zap.Error(err),
		)
	})

	annasBaseURL := env.GetAnnasBaseURL()
	fullURL := fmt.Sprintf(AnnasSearchEndpointFormat, annasBaseURL, url.QueryEscape(query), "book_any")

	if err := c.Visit(fullURL); err != nil {
		l.Error("Failed to visit search URL", zap.String("url", fullURL), zap.Error(err))
		return nil, fmt.Errorf("failed to visit search URL: %w", err)
	}
	c.Wait()

	bookListParsed := make([]*Book, 0)
	for _, element := range bookList {
		if book, ok := parseBookElement(element, l); ok {
			bookListParsed = append(bookListParsed, book)
		}
	}

	l.Info("Search completed",
		zap.Int("totalElements", len(bookList)),
		zap.Int("validBooks", len(bookListParsed)),
	)

	return bookListParsed, nil
}

func parseBookElement(element *colly.HTMLElement, l *zap.Logger) (*Book, bool) {
	parent := element.DOM.Parent()
	if parent.Length() == 0 {
		l.Warn("Skipping book: no parent element found")
		return nil, false
	}

	bookInfoDiv := parent.Find("div.max-w-full")
	if bookInfoDiv.Length() == 0 {
		l.Warn("Skipping book: book info container not found")
		return nil, false
	}

	title := strings.TrimSpace(bookInfoDiv.Find("a[href^='/md5/']").Text())
	if title == "" {
		l.Warn("Skipping book: title is empty")
		return nil, false
	}

	authorsRaw := bookInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--user-edit\\]").Parent().Text()
	authors := strings.TrimSpace(authorsRaw)
	publisherRaw := bookInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--company\\]").Parent().Text()
	publisher := strings.TrimSpace(publisherRaw)
	meta := bookInfoDiv.Find("div.text-gray-800").Text()
	language, format, size := extractMetaInformation(meta)

	link := element.Attr("href")
	if link == "" {
		l.Warn("Skipping book: no link found", zap.String("title", title))
		return nil, false
	}
	hash := strings.TrimPrefix(link, "/md5/")
	if hash == "" {
		l.Warn("Skipping book: no hash found", zap.String("title", title))
		return nil, false
	}

	return &Book{
		Language:  language,
		Format:    format,
		Size:      size,
		Title:     title,
		Publisher: publisher,
		Authors:   authors,
		URL:       element.Request.AbsoluteURL(link),
		Hash:      hash,
	}, true
}

func FindArticle(query string, timeout time.Duration) ([]*Paper, error) {
	l := logger.GetLogger()

	var paperListMutex sync.Mutex
	paperList := make([]*colly.HTMLElement, 0)

	c := colly.NewCollector(
		colly.Async(true),
		colly.UserAgent(BrowserUserAgent),
	)
	c.SetRequestTimeout(timeout)

	c.OnHTML("a[href^='/md5/']", func(e *colly.HTMLElement) {
		if e.Attr("class") == "custom-a block mr-2 sm:mr-4 hover:opacity-80" {
			paperListMutex.Lock()
			paperList = append(paperList, e)
			paperListMutex.Unlock()
		}
	})

	c.OnRequest(func(r *colly.Request) {
		l.Info("Visiting URL", zap.String("url", r.URL.String()))
	})

	c.OnError(func(r *colly.Response, err error) {
		status := 0
		if r != nil {
			status = r.StatusCode
		}
		l.Error("Article search request failed",
			zap.Int("statusCode", status),
			zap.Error(err),
		)
	})

	annasBaseURL := env.GetAnnasBaseURL()
	fullURL := fmt.Sprintf(AnnasSearchEndpointFormat, annasBaseURL, url.QueryEscape(query), "journal")

	if err := c.Visit(fullURL); err != nil {
		l.Error("Failed to visit article search URL", zap.String("url", fullURL), zap.Error(err))
		return nil, fmt.Errorf("failed to visit article search URL: %w", err)
	}
	c.Wait()

	paperListParsed := make([]*Paper, 0)
	for _, element := range paperList {
		if paper, ok := parsePaperElement(element, l); ok {
			paperListParsed = append(paperListParsed, paper)
		}
	}

	l.Info("Article search completed",
		zap.Int("totalElements", len(paperList)),
		zap.Int("validPapers", len(paperListParsed)),
	)

	return paperListParsed, nil
}

func parsePaperElement(element *colly.HTMLElement, l *zap.Logger) (*Paper, bool) {
	parent := element.DOM.Parent()
	if parent.Length() == 0 {
		l.Warn("Skipping article: no parent element found")
		return nil, false
	}

	paperInfoDiv := parent.Find("div.max-w-full")
	if paperInfoDiv.Length() == 0 {
		l.Warn("Skipping article: info container not found")
		return nil, false
	}

	title := strings.TrimSpace(paperInfoDiv.Find("a[href^='/md5/']").Text())
	if title == "" {
		l.Warn("Skipping article: title is empty")
		return nil, false
	}

	authorsRaw := paperInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--user-edit\\]").Parent().Text()
	authors := strings.TrimSpace(authorsRaw)
	journalRaw := paperInfoDiv.Find("a[href^='/search'] span.icon-\\[mdi--company\\]").Parent().Text()
	journal := strings.TrimSpace(journalRaw)
	meta := paperInfoDiv.Find("div.text-gray-800").Text()
	_, _, size := extractMetaInformation(meta)

	link := element.Attr("href")
	if link == "" {
		l.Warn("Skipping article: no link found", zap.String("title", title))
		return nil, false
	}
	hash := strings.TrimPrefix(link, "/md5/")
	if hash == "" {
		l.Warn("Skipping article: no hash found", zap.String("title", title))
		return nil, false
	}

	return &Paper{
		Title:   title,
		Authors: authors,
		Journal: journal,
		Size:    size,
		Hash:    hash,
		PageURL: element.Request.AbsoluteURL(link),
	}, true
}
