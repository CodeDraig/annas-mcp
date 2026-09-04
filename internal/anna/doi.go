package anna

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/iosifache/annas-mcp/internal/env"
	"github.com/iosifache/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

func LookupDOI(ctx context.Context, doi string, timeout time.Duration) (*Paper, error) {
	return lookupDOI(ctx, defaultPageLoader, doi, timeout)
}

func lookupDOI(ctx context.Context, loader pageLoader, doi string, timeout time.Duration) (*Paper, error) {
	annasBaseURL, err := env.GetAnnasBaseURL(ctx, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to load Anna configuration: %w", err)
	}
	return lookupDOIAtBaseURL(ctx, loader, annasBaseURL, doi, timeout)
}

func lookupDOIAtBaseURL(ctx context.Context, loader pageLoader, annasBaseURL, doi string, timeout time.Duration) (*Paper, error) {
	l := logger.GetLogger()
	paper := &Paper{DOI: doi}

	// Keep the raw identifier separate from its path and query representations.
	scidbURL := (&url.URL{Scheme: "https", Host: annasBaseURL, Path: "/scidb/" + doi}).String()
	paper.PageURL = scidbURL
	l.Info("Looking up DOI", zap.String("url", scidbURL))
	searchHTML, err := loader.Load(ctx, scidbURL, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup DOI: %w", err)
	}
	searchDocument, err := goquery.NewDocumentFromReader(strings.NewReader(searchHTML))
	if err != nil {
		return nil, fmt.Errorf("failed to parse DOI lookup: %w", err)
	}
	searchDocument.Find("a[href^='/md5/']").EachWithBreak(func(_ int, element *goquery.Selection) bool {
		link, ok := element.Attr("href")
		if !ok {
			return true
		}
		hash := strings.TrimPrefix(link, "/md5/")
		if hash == "" {
			return true
		}
		paper.Hash = hash
		return false
	})
	if paper.Hash == "" {
		return nil, fmt.Errorf("no paper found for DOI: %s", doi)
	}

	md5URL := fmt.Sprintf("https://%s/md5/%s", annasBaseURL, paper.Hash)
	l.Info("Fetching paper details", zap.String("url", md5URL))
	detailHTML, detailErr := loader.Load(ctx, md5URL, timeout)
	if detailErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		l.Warn("Failed to fetch paper details", zap.String("hash", paper.Hash), zap.Error(detailErr))
	} else if detailDocument, parseErr := goquery.NewDocumentFromReader(strings.NewReader(detailHTML)); parseErr != nil {
		l.Warn("Failed to parse paper details", zap.String("hash", paper.Hash), zap.Error(parseErr))
	} else {
		populatePaperDetails(paper, detailDocument)
	}

	paper.DownloadURL = (&url.URL{Path: "/scidb", RawQuery: url.Values{"doi": {doi}}.Encode()}).String()
	return paper, nil
}

func populatePaperDetails(paper *Paper, document *goquery.Document) {
	if title := paperTitleFromPageTitle(document.Find("title").First().Text()); title != "" {
		paper.Title = title
	}
	if description, ok := document.Find("meta[name=description]").First().Attr("content"); ok {
		paper.Journal = journalFromDescription(description)
	}
	document.Find("a[href^='/search']").EachWithBreak(func(_ int, element *goquery.Selection) bool {
		if element.Find("span.icon-\\[mdi--user-edit\\]").Length() == 0 {
			return true
		}
		paper.Authors = strings.TrimSpace(element.Text())
		return false
	})
	document.Find("div.text-gray-500").EachWithBreak(func(_ int, element *goquery.Selection) bool {
		text := strings.TrimSpace(element.Text())
		if !strings.Contains(text, "MB") && !strings.Contains(text, "KB") {
			return true
		}
		paper.Size = text
		return false
	})
}

func paperTitleFromPageTitle(title string) string {
	if idx := strings.Index(title, " - Anna"); idx > 0 {
		return strings.TrimSpace(title[:idx])
	}
	return ""
}

func journalFromDescription(description string) string {
	parts := strings.Split(description, "\n\n")
	if len(parts) >= 3 {
		return strings.TrimSpace(parts[2])
	}
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(description)
}
