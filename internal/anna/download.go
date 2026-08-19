package anna

import (
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

func (b *Book) Download(secretKey, folderPath string, timeout time.Duration) (string, error) {
	l := logger.GetLogger()
	annasBaseURL := env.GetAuthenticatedAnnasBaseURL()
	client := &http.Client{Timeout: timeout}

	apiURL := fmt.Sprintf(AnnasDownloadEndpointFormat, annasBaseURL, b.Hash, secretKey)
	l.Info("Fetching download URL", zap.String("hash", b.Hash))
	resp, err := client.Get(apiURL)
	if err != nil {
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
	downloadResp, err := client.Get(apiResp.DownloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download file: %w", err)
	}
	defer downloadResp.Body.Close()
	if downloadResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status %d: %s", downloadResp.StatusCode, downloadResp.Status)
	}

	filePath := bookDownloadPath(folderPath, b)
	l.Info("Creating file", zap.String("path", filePath))
	return persistDownload(filePath, downloadResp.Body, l, "Download completed successfully")
}

func (p *Paper) Download(folderPath string, timeout time.Duration) (string, error) {
	l := logger.GetLogger()
	if p.DownloadURL == "" {
		return "", errors.New("no download URL available for this paper")
	}

	annasBaseURL := env.GetAnnasBaseURL()
	downloadURL := p.DownloadURL
	if !strings.HasPrefix(downloadURL, "http") {
		downloadURL = fmt.Sprintf("https://%s%s", annasBaseURL, downloadURL)
	}
	client := &http.Client{Timeout: timeout}

	l.Info("Downloading paper via SciDB", zap.String("url", downloadURL))
	req, err := http.NewRequest("GET", downloadURL, nil)
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
	return persistDownload(filePath, resp.Body, l, "Paper download completed successfully")
}
