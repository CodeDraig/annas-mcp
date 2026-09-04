// Package testutil provides process isolation for configuration-dependent tests.
package testutil

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"testing"
	"time"
)

// InFreshProcess runs the named top-level test once in a fresh test process.
// Callers return when it returns false; the child executes the fixture itself.
// This preserves production configuration caching without sharing fixture state.
func InFreshProcess(t *testing.T) bool {
	t.Helper()
	const marker = "ANNAS_TEST_CHILD"
	if os.Getenv(marker) == t.Name() {
		return true
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1", "-test.timeout=50s")
	command.Env = append(os.Environ(), marker+"="+t.Name())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("isolated test: %v\n%s", err, output)
	}
	return false
}
