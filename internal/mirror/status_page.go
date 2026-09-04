package mirror

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type CandidateStatus string

const (
	StatusUp        CandidateStatus = "UP"
	StatusDegraded  CandidateStatus = "DEGRADED"
	StatusProtected CandidateStatus = "PROTECTED"
	StatusDown      CandidateStatus = "DOWN"
	StatusTimeout   CandidateStatus = "TIMEOUT"
	StatusUnknown   CandidateStatus = "UNKNOWN"
)

var candidateHostRegex = regexp.MustCompile(`^annas-archive\.[a-z0-9-]+$`)

func ParseStatusPageHTML(html string) ([]Candidate, error) {
	document, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	candidates := make([]Candidate, 0)
	document.Find("div.card").Each(func(_ int, card *goquery.Selection) {
		link := card.Find("a.card-title[href]").First()
		rawURL, ok := link.Attr("href")
		if !ok {
			return
		}

		baseURL, ok := validateCandidateURL(rawURL)
		if !ok {
			return
		}
		if _, exists := seen[baseURL]; exists {
			return
		}

		status := normalizeStatus(card.Find(".card-header .status-badge").Last().Text())
		latencyMillis := int64(0)
		if rawLatency, ok := card.Find(".timeline span[data-latency]").Last().Attr("data-latency"); ok {
			if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(rawLatency), 10, 64); parseErr == nil && parsed > 0 {
				latencyMillis = parsed
			}
		}

		seen[baseURL] = struct{}{}
		candidates = append(candidates, Candidate{
			BaseURL:       baseURL,
			SourceURL:     "https://" + baseURL + "/",
			Status:        status,
			LatencyMillis: latencyMillis,
		})
	})

	return candidates, nil
}

func normalizeStatus(raw string) CandidateStatus {
	switch CandidateStatus(strings.ToUpper(strings.TrimSpace(raw))) {
	case StatusUp:
		return StatusUp
	case StatusDegraded:
		return StatusDegraded
	case StatusProtected:
		return StatusProtected
	case StatusDown:
		return StatusDown
	case StatusTimeout:
		return StatusTimeout
	default:
		return StatusUnknown
	}
}

func validateCandidateURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Opaque != "" {
		return "", false
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" || !candidateHostRegex.MatchString(hostname) {
		return "", false
	}
	if parsed.Port() != "" || !strings.EqualFold(parsed.Host, parsed.Hostname()) {
		return "", false
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", false
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	return hostname, true
}
