package anna

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestCreateDownloadFileRefusesExistingDestination(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "existing.pdf")
	want := []byte("keep this content")
	if err := os.WriteFile(destination, want, 0o600); err != nil {
		t.Fatalf("seed destination: %v", err)
	}

	out, err := createDownloadFile(destination)
	if out != nil {
		out.Close()
		t.Fatal("createDownloadFile returned a file for an existing destination")
	}
	if err == nil || !strings.Contains(err.Error(), "destination already exists") {
		t.Fatalf("expected existing-destination error, got %v", err)
	}

	got, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatalf("read preserved destination: %v", readErr)
	}
	if string(got) != string(want) {
		t.Fatalf("existing destination changed: got %q, want %q", got, want)
	}
}

func TestPersistDownloadWritesContent(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "new.pdf")
	path, err := persistDownload(destination, strings.NewReader("document"), zap.NewNop(), "complete")
	if err != nil {
		t.Fatalf("persistDownload returned error: %v", err)
	}
	if path != destination {
		t.Fatalf("persistDownload returned %q, want %q", path, destination)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(content) != "document" {
		t.Fatalf("destination content = %q", content)
	}
}

func TestPersistDownloadRemovesPartialFile(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "partial.pdf")
	_, err := persistDownload(destination, failingReader{}, zap.NewNop(), "complete")
	if err == nil || !strings.Contains(err.Error(), "failed to write file (wrote 7 bytes)") {
		t.Fatalf("unexpected persistDownload error: %v", err)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial destination remains: %v", statErr)
	}
}

func TestDownloadPathsPreserveFilenameRules(t *testing.T) {
	t.Parallel()

	folder := t.TempDir()
	if got := bookDownloadPath(folder, &Book{Title: "a/b..c", Format: "PDF"}); got != filepath.Join(folder, "a_b_c.pdf") {
		t.Fatalf("bookDownloadPath = %q", got)
	}
	// Preserve the current filepath.Base("") behavior for direct callers that
	// construct an entirely empty Book.
	if got := bookDownloadPath(folder, &Book{}); got != filepath.Join(folder, "..bin") {
		t.Fatalf("default bookDownloadPath = %q", got)
	}

	header := make(http.Header)
	header.Set("Content-Disposition", `attachment; filename="source.epub"`)
	if got := paperDownloadPath(folder, &Paper{Title: "Paper"}, header); got != filepath.Join(folder, "Paper.epub") {
		t.Fatalf("paperDownloadPath from content disposition = %q", got)
	}
	if got := paperDownloadPath(folder, &Paper{DOI: "10.1/example"}, make(http.Header)); got != filepath.Join(folder, "10.1_example.pdf") {
		t.Fatalf("default paperDownloadPath = %q", got)
	}
}

type failingReader struct{}

func (failingReader) Read(buffer []byte) (int, error) {
	return copy(buffer, "partial"), errors.New("reader failed")
}
