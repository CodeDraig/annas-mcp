package mirror

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const defaultStatusPageSlug = "slum"

var (
	slugRegex            = regexp.MustCompile(`['"]slug['"]\s*:\s*['"]([^'"]+)['"]`)
	candidateObjectRegex = regexp.MustCompile(`(?s)\{[^{}]*['"]id['"]\s*:\s*([0-9]+)[^{}]*['"]url['"]\s*:\s*['"]([^'"]+)['"][^{}]*\}`)
	fallbackURLRegex     = regexp.MustCompile(`(?:^|[\s'"<>=])(https://annas-archive\.[a-z0-9-]+/?)(?:$|[\s'"<>])`)
	candidateHostRegex   = regexp.MustCompile(`^annas-archive\.[a-z0-9-]+$`)
)

func ParseStatusPageHTML(html string) ([]Candidate, error) {
	normalized := strings.ReplaceAll(html, `\'`, `'`)
	candidates, err := parseCandidatesFromAnnaGroup(normalized)
	if err != nil {
		return nil, err
	}
	if len(candidates) > 0 {
		return candidates, nil
	}
	return parseCandidatesByURLFallback(normalized), nil
}

func parseCandidatesFromAnnaGroup(html string) ([]Candidate, error) {
	groupIndex := strings.Index(html, "Anna's Archive")
	if groupIndex < 0 {
		return nil, nil
	}

	monitorKeyIndex := strings.Index(html[groupIndex:], "'monitorList':[")
	if monitorKeyIndex < 0 {
		monitorKeyIndex = strings.Index(html[groupIndex:], `"monitorList":[`)
	}
	if monitorKeyIndex < 0 {
		return nil, errors.New("Anna group found but monitor list missing")
	}

	monitorKeyIndex += groupIndex
	listStart := strings.Index(html[monitorKeyIndex:], "[")
	if listStart < 0 {
		return nil, errors.New("monitor list start missing")
	}
	listStart += monitorKeyIndex
	listEnd := findMatchingBracket(html, listStart)
	if listEnd < 0 {
		return nil, errors.New("monitor list end missing")
	}
	return parseCandidatesFromSection(html[listStart : listEnd+1]), nil
}

func parseCandidatesByURLFallback(html string) []Candidate {
	matches := fallbackURLRegex.FindAllStringSubmatch(html, -1)
	seen := make(map[string]struct{})
	candidates := make([]Candidate, 0, len(matches))
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		baseURL, ok := validateCandidateURL(match[1])
		if !ok {
			continue
		}
		if _, exists := seen[baseURL]; exists {
			continue
		}
		seen[baseURL] = struct{}{}
		candidates = append(candidates, Candidate{
			BaseURL:   baseURL,
			SourceURL: "https://" + baseURL + "/",
		})
	}
	return candidates
}

func parseCandidatesFromSection(section string) []Candidate {
	matches := candidateObjectRegex.FindAllStringSubmatch(section, -1)
	candidates := make([]Candidate, 0, len(matches))
	seen := make(map[string]struct{})
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		baseURL, ok := validateCandidateURL(match[2])
		if !ok {
			continue
		}
		if _, exists := seen[baseURL]; exists {
			continue
		}
		seen[baseURL] = struct{}{}
		candidates = append(candidates, Candidate{
			MonitorID: mustAtoi(match[1]),
			BaseURL:   baseURL,
			SourceURL: "https://" + baseURL + "/",
		})
	}
	return candidates
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

func extractStatusPageSlug(html string) string {
	match := slugRegex.FindStringSubmatch(strings.ReplaceAll(html, `\'`, `'`))
	if len(match) == 2 {
		return match[1]
	}
	return defaultStatusPageSlug
}

func buildHeartbeatURL(statusPageURL, slug string) (string, error) {
	parsed, err := url.Parse(statusPageURL)
	if err != nil {
		return "", err
	}
	parsed.Path = fmt.Sprintf("/api/status-page/heartbeat/%s", slug)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func findMatchingBracket(input string, start int) int {
	depth := 0
	for idx := start; idx < len(input); idx++ {
		switch input[idx] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return idx
			}
		}
	}
	return -1
}

func mustAtoi(raw string) int {
	value := 0
	for _, ch := range raw {
		value = value*10 + int(ch-'0')
	}
	return value
}
