package anna

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/iosifache/annas-mcp/internal/env"
	"github.com/iosifache/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

var unsafeFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

type fastDownloadResponse struct {
	DownloadURL string `json:"download_url"`
	Error       string `json:"error"`
}

func sanitizeFilename(filename string) string {
	safe := unsafeFilenameChars.ReplaceAllString(filename, "_")
	safe = strings.ReplaceAll(safe, "..", "_")
	safe = filepath.Base(safe)
	if len(safe) > 200 {
		safe = safe[:200]
	}
	return safe
}

func createDownloadFile(filePath string) (*os.File, error) {
	out, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("destination already exists: %s", filePath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	return out, nil
}

func persistDownload(filePath string, body io.Reader, l *zap.Logger, completionMessage string) (string, error) {
	out, err := createDownloadFile(filePath)
	if err != nil {
		return "", err
	}

	success := false
	defer func() {
		out.Close()
		if !success {
			if removeErr := os.Remove(filePath); removeErr != nil {
				l.Warn("Failed to remove partial file",
					zap.String("path", filePath),
					zap.Error(removeErr),
				)
			}
		}
	}()

	written, err := io.Copy(out, body)
	if err != nil {
		return "", fmt.Errorf("failed to write file (wrote %d bytes): %w", written, err)
	}
	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync file to disk: %w", err)
	}

	success = true
	l.Info(completionMessage,
		zap.String("path", filePath),
		zap.Int64("bytes", written),
	)
	return filePath, nil
}

func bookDownloadPath(folderPath string, book *Book) string {
	safeTitle := sanitizeFilename(book.Title)
	if safeTitle == "" {
		safeTitle = "untitled"
	}
	format := strings.ToLower(book.Format)
	if format == "" {
		format = "bin"
	}
	return filepath.Join(folderPath, safeTitle+"."+format)
}

func paperDownloadPath(folderPath string, paper *Paper, header http.Header) string {
	ext := ".pdf"
	if contentDisposition := header.Get("Content-Disposition"); contentDisposition != "" {
		if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
			if filename, ok := params["filename"]; ok {
				if candidate := filepath.Ext(filename); candidate != "" {
					ext = candidate
				}
			}
		}
	} else if contentType := header.Get("Content-Type"); contentType != "" {
		exts, _ := mime.ExtensionsByType(contentType)
		if len(exts) > 0 {
			ext = exts[0]
		}
	}

	name := paper.Title
	if name == "" {
		name = paper.DOI
	}
	safeName := sanitizeFilename(name)
	if safeName == "" {
		safeName = "paper"
	}
	return filepath.Join(folderPath, safeName+ext)
}

func authenticatedDownloadClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// Go otherwise includes the key-bearing API URL in Referer.
		req.Header.Del("Referer")
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		origin := via[0].URL
		if req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Host, origin.Host) || req.URL.User != nil {
			return errors.New("authenticated redirect leaves the configured HTTPS origin")
		}
		return nil
	}}
}

func (b *Book) Download(ctx context.Context, secretKey, folderPath string, timeout time.Duration) (string, error) {
	l := logger.GetLogger()
	annasBaseURL, err := env.GetAuthenticatedAnnasBaseURL()
	if err != nil {
		return "", fmt.Errorf("failed to load authenticated Anna configuration: %w", err)
	}
	client := authenticatedDownloadClient(timeout)

	apiURL := fmt.Sprintf(AnnasDownloadEndpointFormat, annasBaseURL, b.Hash, secretKey)
	l.Info("Fetching download URL", zap.String("hash", b.Hash))
	resp, err := downloadRequest(ctx, client, apiURL)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("failed to fetch download URL")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, resp.Status)
		}
		return "", fmt.Errorf("API request failed with status %d: %s (body: %s)", resp.StatusCode, resp.Status, string(body))
	}

	var apiResp fastDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("failed to decode API response: %w", err)
	}
	if apiResp.DownloadURL == "" {
		if apiResp.Error != "" {
			return "", fmt.Errorf("API error: %s", apiResp.Error)
		}
		return "", errors.New("API returned empty download URL")
	}

	l.Info("Downloading file", zap.String("url", apiResp.DownloadURL))
	// File delivery may use another host; it never inherits the API request URL.
	downloadResp, err := downloadRequest(ctx, &http.Client{Timeout: timeout}, apiResp.DownloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %w", err)
	}
	defer downloadResp.Body.Close()
	if downloadResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status %d: %s", downloadResp.StatusCode, downloadResp.Status)
	}

	filePath := bookDownloadPath(folderPath, b)
	l.Info("Creating file", zap.String("path", filePath))
	body, err := validateDownloadBody(downloadResp, b.Format)
	if err != nil {
		return "", err
	}
	return persistDownload(filePath, body, l, "Download completed successfully")
}

func (p *Paper) Download(ctx context.Context, folderPath string, timeout time.Duration) (string, error) {
	l := logger.GetLogger()
	if p.DownloadURL == "" {
		return "", errors.New("no download URL available for this paper")
	}

	annasBaseURL, err := env.GetAnnasBaseURL(ctx, timeout)
	if err != nil {
		return "", fmt.Errorf("failed to load Anna configuration: %w", err)
	}
	downloadURL := p.DownloadURL
	if !strings.HasPrefix(downloadURL, "http") {
		downloadURL = fmt.Sprintf("https://%s%s", annasBaseURL, downloadURL)
	}
	client := &http.Client{Timeout: timeout}

	l.Info("Downloading paper via SciDB", zap.String("url", downloadURL))
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", BrowserUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download paper: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return "", fmt.Errorf("download failed with status %d: %s", resp.StatusCode, resp.Status)
		}
		return "", fmt.Errorf("download failed with status %d: %s (body: %s)", resp.StatusCode, resp.Status, string(body))
	}

	filePath := paperDownloadPath(folderPath, p, resp.Header)
	l.Info("Creating file", zap.String("path", filePath))
	body, err := validateDownloadBody(resp, "pdf")
	if err != nil {
		return "", err
	}
	return persistDownload(filePath, body, l, "Paper download completed successfully")
}

func downloadRequest(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

// Peek retains every inspected byte for persistence, without buffering the file.
func validateDownloadBody(response *http.Response, format string) (io.Reader, error) {
	body := bufio.NewReaderSize(response.Body, 4096)
	prefix, err := body.Peek(4096)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("inspect download: %w", err)
	}
	text := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(string(prefix), "\ufeff")))
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	sniffedType, _, _ := mime.ParseMediaType(http.DetectContentType([]byte(text)))
	html := contentType == "text/html" || contentType == "application/xhtml+xml" || sniffedType == "text/html"
	explicitText := strings.EqualFold(format, "txt") || strings.EqualFold(format, "html") || strings.EqualFold(format, "htm")
	if html && !explicitText {
		return nil, errors.New("download returned an HTML page instead of a document")
	}
	return body, nil
}
