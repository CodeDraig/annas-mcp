package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iosifache/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

const (
	DefaultStatusPageURL = "https://open-slum.org/"
	defaultProbeTimeout  = 10 * time.Second
	browserUserAgent     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type Heartbeat struct {
	Status    int       `json:"status"`
	Ping      int64     `json:"ping"`
	Time      time.Time `json:"-"`
	Timestamp string    `json:"time"`
}

type Candidate struct {
	MonitorID  int
	BaseURL    string
	SourceURL  string
	Heartbeats []Heartbeat
}

type CandidateScore struct {
	SuccessCount int
	SampleCount  int
	LastStatus   int
	AveragePing  int64
}

type ResolveOptions struct {
	FallbackBaseURL string
}

type ProbeFunc func(context.Context, string) error

type Resolver struct {
	client        *http.Client
	statusPageURL string
	probe         ProbeFunc
}

type heartbeatEnvelope struct {
	HeartbeatList map[string][]Heartbeat `json:"heartbeatList"`
}

func NewResolver(client *http.Client, statusPageURL string, probe ProbeFunc) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: defaultProbeTimeout}
	}
	if statusPageURL == "" {
		statusPageURL = DefaultStatusPageURL
	}

	resolver := &Resolver{
		client:        client,
		statusPageURL: statusPageURL,
	}
	if probe == nil {
		resolver.probe = resolver.defaultProbe
	} else {
		resolver.probe = probe
	}
	return resolver
}

func NormalizeBaseURL(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimSuffix(value, "/")
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	return value
}

func (r *Resolver) Resolve(ctx context.Context, opts ResolveOptions) (string, error) {
	l := logger.GetLogger()
	fallbackBaseURL := NormalizeBaseURL(opts.FallbackBaseURL)

	html, err := r.fetch(ctx, r.statusPageURL)
	if err != nil {
		l.Warn("Failed to fetch mirror status page, using fallback mirror if available",
			zap.String("statusPageURL", r.statusPageURL),
			zap.String("fallbackBaseURL", fallbackBaseURL),
			zap.Error(err),
		)
		if fallbackBaseURL != "" {
			return fallbackBaseURL, nil
		}
		return "", err
	}

	candidates, err := ParseStatusPageHTML(html)
	if err != nil || len(candidates) == 0 {
		l.Warn("Failed to discover Anna mirror candidates, using fallback mirror if available",
			zap.Int("candidateCount", len(candidates)),
			zap.String("fallbackBaseURL", fallbackBaseURL),
			zap.Error(err),
		)
		if fallbackBaseURL != "" {
			return fallbackBaseURL, nil
		}
		if err == nil {
			err = errors.New("no Anna mirror candidates discovered")
		}
		return "", err
	}

	slug := extractStatusPageSlug(html)
	heartbeatURL, err := buildHeartbeatURL(r.statusPageURL, slug)
	if err != nil {
		if fallbackBaseURL != "" {
			return fallbackBaseURL, nil
		}
		return "", err
	}

	heartbeatJSON, err := r.fetch(ctx, heartbeatURL)
	if err != nil {
		l.Warn("Failed to fetch mirror heartbeat data, using fallback mirror if available",
			zap.String("heartbeatURL", heartbeatURL),
			zap.String("fallbackBaseURL", fallbackBaseURL),
			zap.Error(err),
		)
		if fallbackBaseURL != "" {
			return fallbackBaseURL, nil
		}
		return "", err
	}

	envelope := heartbeatEnvelope{}
	if err := json.Unmarshal([]byte(heartbeatJSON), &envelope); err != nil {
		l.Warn("Failed to decode mirror heartbeat data, using fallback mirror if available",
			zap.String("fallbackBaseURL", fallbackBaseURL),
			zap.Error(err),
		)
		if fallbackBaseURL != "" {
			return fallbackBaseURL, nil
		}
		return "", err
	}

	ranked := rankCandidates(applyHeartbeats(candidates, envelope.HeartbeatList))
	l.Info("Resolved Anna mirror candidates",
		zap.Int("discoveredCandidates", len(candidates)),
		zap.Int("healthyCandidates", len(ranked)),
	)
	for _, candidate := range ranked {
		if r.probe != nil {
			if err := r.probe(ctx, candidate.BaseURL); err != nil {
				l.Warn("Anna mirror probe failed",
					zap.String("baseURL", candidate.BaseURL),
					zap.Error(err),
				)
				continue
			}
		}

		l.Info("Selected Anna mirror automatically",
			zap.String("baseURL", candidate.BaseURL),
			zap.Int("monitorID", candidate.MonitorID),
		)
		return candidate.BaseURL, nil
	}

	if fallbackBaseURL != "" {
		l.Warn("No reachable Anna mirror found, using fallback mirror",
			zap.String("fallbackBaseURL", fallbackBaseURL),
		)
		return fallbackBaseURL, nil
	}
	return "", errors.New("no reachable Anna mirror found")
}

func (r *Resolver) defaultProbe(ctx context.Context, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://%s/search?q=test&content=book_any", NormalizeBaseURL(baseURL)), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUserAgent)

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("probe failed with status %d", resp.StatusCode)
	}
	return nil
}

func (r *Resolver) fetch(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", browserUserAgent)

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("unexpected status %d for %s", resp.StatusCode, rawURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
