package anna

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestCreateDownloadFileCreatesNewDestination(t *testing.T) {
	t.Parallel()

	destination := filepath.Join(t.TempDir(), "new.pdf")
	out, err := createDownloadFile(destination)
	if err != nil {
		t.Fatalf("createDownloadFile returned error: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close destination: %v", err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("new destination was not created: %v", err)
	}
}
