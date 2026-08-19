package anna

import (
	"fmt"
	"strings"
	"time"

	colly "github.com/gocolly/colly/v2"
	"github.com/iosifache/annas-mcp/internal/env"
	"github.com/iosifache/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

func LookupDOI(doi string, timeout time.Duration) (*Paper, error) {
	l := logger.GetLogger()
	annasBaseURL := env.GetAnnasBaseURL()
	paper := &Paper{DOI: doi}

	searchCollector := colly.NewCollector(colly.UserAgent(BrowserUserAgent))
	searchCollector.SetRequestTimeout(timeout)
	searchCollector.OnHTML("a[href^='/md5/']", func(e *colly.HTMLElement) {
		if paper.Hash != "" {
			return
		}
		hash := strings.TrimPrefix(e.Attr("href"), "/md5/")
		if hash != "" {
			paper.Hash = hash
		}
	})
	searchCollector.OnError(func(r *colly.Response, err error) {
		status := 0
		if r != nil {
			status = r.StatusCode
		}
		l.Error("SciDB search failed",
			zap.String("doi", doi),
			zap.Int("statusCode", status),
			zap.Error(err),
		)
	})

	scidbURL := fmt.Sprintf(AnnasSciDBEndpointFormat, annasBaseURL, doi)
	paper.PageURL = scidbURL
	l.Info("Looking up DOI", zap.String("url", scidbURL))
	if err := searchCollector.Visit(scidbURL); err != nil {
		return nil, fmt.Errorf("failed to lookup DOI: %w", err)
	}
	if paper.Hash == "" {
		return nil, fmt.Errorf("no paper found for DOI: %s", doi)
	}

	detailCollector := colly.NewCollector(colly.UserAgent(BrowserUserAgent))
	detailCollector.SetRequestTimeout(timeout)
	detailCollector.OnHTML("title", func(e *colly.HTMLElement) {
		if title := paperTitleFromPageTitle(e.Text); title != "" {
			paper.Title = title
		}
	})
	detailCollector.OnHTML("meta[name=description]", func(e *colly.HTMLElement) {
		paper.Journal = journalFromDescription(e.Attr("content"))
	})
	detailCollector.OnHTML("a[href^='/search']", func(e *colly.HTMLElement) {
		if paper.Authors == "" && e.DOM.Find("span.icon-\\[mdi--user-edit\\]").Length() > 0 {
			paper.Authors = strings.TrimSpace(e.Text)
		}
	})
	detailCollector.OnHTML("div.text-gray-500", func(e *colly.HTMLElement) {
		if strings.Contains(e.Text, "MB") || strings.Contains(e.Text, "KB") {
			paper.Size = strings.TrimSpace(e.Text)
		}
	})
	detailCollector.OnError(func(r *colly.Response, err error) {
		l.Warn("Failed to fetch paper details", zap.String("hash", paper.Hash), zap.Error(err))
	})

	md5URL := fmt.Sprintf("https://%s/md5/%s", annasBaseURL, paper.Hash)
	l.Info("Fetching paper details", zap.String("url", md5URL))
	if err := detailCollector.Visit(md5URL); err != nil {
		l.Warn("Failed to visit paper detail page", zap.Error(err))
	}

	paper.DownloadURL = fmt.Sprintf("/scidb?doi=%s", doi)
	return paper, nil
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
