package env

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var configurationKeys = []string{
	"ANNAS_SECRET_KEY",
	"ANNAS_DOWNLOAD_PATH",
	"ANNAS_BASE_URL",
	"ANNAS_AUTO_BASE_URL",
}

func TestDefaultConfigFilePathUsesUserConfigDirectory(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	path, err := defaultConfigFilePath()
	if err != nil {
		t.Fatalf("defaultConfigFilePath returned error: %v", err)
	}
	want := filepath.Join(configDir, "annas-cli", ".env")
	if path != want {
		t.Fatalf("defaultConfigFilePath = %q, want %q", path, want)
	}
}

func TestMissingConfigUsesPublicDefaultsAndBlocksAuthenticatedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.env")
	useConfigPath(t, path)
	t.Setenv("ANNAS_SECRET_KEY", "ambient-secret")
	t.Setenv("ANNAS_DOWNLOAD_PATH", "/ambient/downloads")
	t.Setenv("ANNAS_BASE_URL", "attacker.example")
	t.Setenv("ANNAS_AUTO_BASE_URL", "true")

	invocationDirectory := t.TempDir()
	writeConfiguration(t, filepath.Join(invocationDirectory, ".env"), map[string]string{
		"ANNAS_SECRET_KEY":    "cwd-secret",
		"ANNAS_DOWNLOAD_PATH": "/cwd/downloads",
		"ANNAS_BASE_URL":      "cwd-attacker.example",
		"ANNAS_AUTO_BASE_URL": "true",
	})
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(invocationDirectory); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(workingDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	baseURL, err := GetAnnasBaseURL(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("GetAnnasBaseURL returned error: %v", err)
	}
	if baseURL != DefaultAnnasBaseURL {
		t.Fatalf("GetAnnasBaseURL = %q, want %q", baseURL, DefaultAnnasBaseURL)
	}

	if _, err := GetAuthenticatedAnnasBaseURL(); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("GetAuthenticatedAnnasBaseURL error = %v, want missing path", err)
	}
	if _, err := GetEnv(); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("GetEnv error = %v, want missing path", err)
	}
}

func TestCompleteConfigIgnoresAmbientEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	useConfigPath(t, path)

	t.Setenv("ANNAS_SECRET_KEY", "ambient-secret")
	t.Setenv("ANNAS_DOWNLOAD_PATH", "/ambient/downloads")
	t.Setenv("ANNAS_BASE_URL", "attacker.example")
	t.Setenv("ANNAS_AUTO_BASE_URL", "true")

	downloadPath := t.TempDir()
	writeConfiguration(t, path, map[string]string{
		"ANNAS_SECRET_KEY":    "file-secret",
		"ANNAS_DOWNLOAD_PATH": downloadPath,
		"ANNAS_BASE_URL":      "https://trusted.example/",
		"ANNAS_AUTO_BASE_URL": "false",
	})

	cfg, err := GetEnv()
	if err != nil {
		t.Fatalf("GetEnv returned error: %v", err)
	}
	if cfg.SecretKey != "file-secret" || cfg.DownloadPath != downloadPath {
		t.Fatalf("GetEnv used unexpected secret or download path")
	}
	if cfg.AnnasBaseURL != "trusted.example" || cfg.AutoAnnasBaseURL {
		t.Fatalf("GetEnv returned unexpected public configuration: %+v", cfg)
	}

	publicBaseURL, err := GetAnnasBaseURL(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("GetAnnasBaseURL returned error: %v", err)
	}
	if publicBaseURL != "trusted.example" {
		t.Fatalf("GetAnnasBaseURL = %q, want trusted.example", publicBaseURL)
	}
	authenticatedBaseURL, err := GetAuthenticatedAnnasBaseURL()
	if err != nil {
		t.Fatalf("GetAuthenticatedAnnasBaseURL returned error: %v", err)
	}
	if authenticatedBaseURL != "trusted.example" {
		t.Fatalf("GetAuthenticatedAnnasBaseURL = %q, want trusted.example", authenticatedBaseURL)
	}
}

func TestPublicConfigurationDoesNotParseSecretOrDownloadSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	useConfigPath(t, path)
	content := "ANNAS_SECRET_KEY='unterminated-secret\n" +
		"ANNAS_DOWNLOAD_PATH=relative/private/path\n" +
		"ANNAS_BASE_URL=https://trusted.example/\n" +
		"ANNAS_AUTO_BASE_URL=false\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}

	baseURL, err := GetAnnasBaseURL(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("GetAnnasBaseURL parsed private configuration: %v", err)
	}
	if baseURL != "trusted.example" {
		t.Fatalf("GetAnnasBaseURL = %q, want trusted.example", baseURL)
	}
	if _, err := GetEnv(); err == nil || !strings.Contains(err.Error(), "failed to parse configuration file") {
		t.Fatalf("GetEnv error = %v, want private parse failure", err)
	}
}

func TestBlankConfigurationUsesPublicDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	useConfigPath(t, path)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write blank configuration: %v", err)
	}

	baseURL, err := GetAnnasBaseURL(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("GetAnnasBaseURL returned error: %v", err)
	}
	if baseURL != DefaultAnnasBaseURL {
		t.Fatalf("GetAnnasBaseURL = %q, want %q", baseURL, DefaultAnnasBaseURL)
	}
}

func TestInvalidPublicConfigurationFailsSearch(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	useConfigPath(t, path)
	content := "ANNAS_SECRET_KEY='ignored-unterminated\n" +
		"ANNAS_BASE_URL=configured.example\n" +
		"ANNAS_AUTO_BASE_URL=sometimes\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}

	_, err := GetAnnasBaseURL(context.Background(), time.Second)
	if err == nil || !strings.Contains(err.Error(), "ANNAS_AUTO_BASE_URL") {
		t.Fatalf("GetAnnasBaseURL error = %v, want invalid public boolean", err)
	}
}

func TestAutomaticResolutionUsesFileFallbackAndCachesResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	useConfigPath(t, path)
	writeConfiguration(t, path, validConfiguration(t.TempDir(), "true"))

	resolverCalls := int32(0)
	resolveAnnasBaseURL = func(_ context.Context, fallbackBaseURL string) (string, error) {
		atomic.AddInt32(&resolverCalls, 1)
		if fallbackBaseURL != "configured.example" {
			t.Fatalf("resolver fallback = %q, want configured.example", fallbackBaseURL)
		}
		return "annas-archive.dynamic", nil
	}

	first, err := GetAnnasBaseURL(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("first GetAnnasBaseURL returned error: %v", err)
	}
	second, err := GetAnnasBaseURL(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("second GetAnnasBaseURL returned error: %v", err)
	}
	if first != "annas-archive.dynamic" || second != first {
		t.Fatalf("resolved base URLs = %q and %q", first, second)
	}
	if calls := atomic.LoadInt32(&resolverCalls); calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls)
	}
}

func TestAutomaticResolutionFailureUsesConfiguredFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	useConfigPath(t, path)
	writeConfiguration(t, path, validConfiguration(t.TempDir(), "true"))

	resolveAnnasBaseURL = func(context.Context, string) (string, error) {
		return "", errors.New("resolver failed")
	}

	baseURL, err := GetAnnasBaseURL(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("GetAnnasBaseURL returned error: %v", err)
	}
	if baseURL != "configured.example" {
		t.Fatalf("GetAnnasBaseURL = %q, want configured.example", baseURL)
	}
}

func TestConfigRequiresEveryKey(t *testing.T) {
	for _, missingKey := range configurationKeys {
		t.Run(missingKey, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			useConfigPath(t, path)
			values := validConfiguration(t.TempDir(), "false")
			delete(values, missingKey)
			writeConfiguration(t, path, values)

			_, err := GetEnv()
			if err == nil || !strings.Contains(err.Error(), missingKey) {
				t.Fatalf("GetAnnasBaseURL error = %v, want missing key %s", err, missingKey)
			}
		})
	}
}

func TestInvalidConfigFailsWithoutExposingSecret(t *testing.T) {
	t.Setenv("ANNAS_SECRET_KEY", "ambient-canary-secret")

	tests := map[string]struct {
		content string
		want    string
	}{
		"malformed": {
			content: "ANNAS_SECRET_KEY='canary-secret\n",
			want:    "failed to parse configuration file",
		},
		"invalid boolean": {
			content: configurationText(map[string]string{
				"ANNAS_SECRET_KEY":    "canary-secret",
				"ANNAS_DOWNLOAD_PATH": t.TempDir(),
				"ANNAS_BASE_URL":      "configured.example",
				"ANNAS_AUTO_BASE_URL": "sometimes",
			}),
			want: "ANNAS_AUTO_BASE_URL",
		},
		"relative download path": {
			content: configurationText(map[string]string{
				"ANNAS_SECRET_KEY":    "canary-secret",
				"ANNAS_DOWNLOAD_PATH": "relative/downloads",
				"ANNAS_BASE_URL":      "configured.example",
				"ANNAS_AUTO_BASE_URL": "false",
			}),
			want: "must be an absolute path",
		},
		"ambient expansion ignored": {
			content: configurationText(map[string]string{
				"ANNAS_SECRET_KEY":    "$ANNAS_SECRET_KEY",
				"ANNAS_DOWNLOAD_PATH": t.TempDir(),
				"ANNAS_BASE_URL":      "configured.example",
				"ANNAS_AUTO_BASE_URL": "false",
			}),
			want: "must set: ANNAS_SECRET_KEY",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			useConfigPath(t, path)
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatalf("write configuration: %v", err)
			}

			_, err := GetEnv()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("GetAnnasBaseURL error = %v, want %q", err, test.want)
			}
			if strings.Contains(err.Error(), "canary-secret") || strings.Contains(err.Error(), "ambient-canary-secret") {
				t.Fatalf("configuration error exposed secret: %v", err)
			}
		})
	}
}

func useConfigPath(t *testing.T, path string) {
	t.Helper()
	previousPath := configFilePath
	previousResolver := resolveAnnasBaseURL
	configFilePath = func() (string, error) { return path, nil }
	resolveAnnasBaseURL = defaultResolveAnnasBaseURL
	resetResolvedEnvForTests()
	t.Cleanup(func() {
		configFilePath = previousPath
		resolveAnnasBaseURL = previousResolver
		resetResolvedEnvForTests()
	})
}

func validConfiguration(downloadPath, autoBaseURL string) map[string]string {
	return map[string]string{
		"ANNAS_SECRET_KEY":    "file-secret",
		"ANNAS_DOWNLOAD_PATH": downloadPath,
		"ANNAS_BASE_URL":      "configured.example",
		"ANNAS_AUTO_BASE_URL": autoBaseURL,
	}
}

func writeConfiguration(t *testing.T, path string, values map[string]string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(configurationText(values)), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}
}

func configurationText(values map[string]string) string {
	var builder strings.Builder
	for _, key := range configurationKeys {
		if value, ok := values[key]; ok {
			builder.WriteString(key)
			builder.WriteByte('=')
			builder.WriteString(value)
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func TestCanceledDiscoveryIsNotCached(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	useConfigPath(t, path)
	writeConfiguration(t, path, validConfiguration(t.TempDir(), "true"))
	calls := 0
	resolveAnnasBaseURL = func(ctx context.Context, _ string) (string, error) {
		calls++
		if calls == 1 {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return "annas-archive.good", nil
	}
	if _, err := GetAnnasBaseURL(context.Background(), 10*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout = %v", err)
	}
	got, err := GetAnnasBaseURL(context.Background(), time.Second)
	if err != nil || got != "annas-archive.good" || calls != 2 {
		t.Fatalf("retry = %q, %v, calls=%d", got, err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GetAnnasBaseURL(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached resolution ignored cancel: %v", err)
	}
}
