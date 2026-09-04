package env

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iosifache/annas-mcp/internal/logger"
	"github.com/iosifache/annas-mcp/internal/mirror"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

const DefaultAnnasBaseURL = "annas-archive.gl"

type Env struct {
	SecretKey        string `json:"secret"`
	DownloadPath     string `json:"download_path"`
	AnnasBaseURL     string `json:"annas_base_url"`
	AutoAnnasBaseURL bool   `json:"auto_annas_base_url"`
}

type configurationState struct {
	path  string
	value *Env
	found bool
	err   error
}

type publicConfiguration struct {
	AnnasBaseURL     string
	AutoAnnasBaseURL bool
}

type publicConfigurationState struct {
	path  string
	value publicConfiguration
	found bool
	err   error
}

var (
	configurationOnce   sync.Once
	cachedConfiguration configurationState
	configFilePath      = defaultConfigFilePath
	publicConfigOnce    sync.Once
	cachedPublicConfig  publicConfigurationState

	resolvedEnvMu        sync.Mutex
	resolvedAnnasBaseURL string
	resolveAnnasBaseURL  = defaultResolveAnnasBaseURL
)

func defaultConfigFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to locate the user configuration directory: %w", err)
	}
	return filepath.Join(configDir, "annas-cli", ".env"), nil
}

func loadConfiguration() configurationState {
	configurationOnce.Do(func() {
		path, err := configFilePath()
		cachedConfiguration.path = path
		if err != nil {
			cachedConfiguration.err = err
			return
		}

		values, err := godotenv.Read(path)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			var pathErr *os.PathError
			if errors.As(err, &pathErr) {
				cachedConfiguration.err = fmt.Errorf("failed to read configuration file %s: %w", path, err)
			} else {
				cachedConfiguration.err = fmt.Errorf("failed to parse configuration file %s", path)
			}
			return
		}

		cachedConfiguration.found = true
		cachedConfiguration.value, cachedConfiguration.err = parseConfiguration(path, values)
	})

	return cachedConfiguration
}

func loadPublicConfiguration() publicConfigurationState {
	publicConfigOnce.Do(func() {
		path, err := configFilePath()
		cachedPublicConfig.path = path
		if err != nil {
			cachedPublicConfig.err = err
			return
		}

		file, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			cachedPublicConfig.err = fmt.Errorf("failed to read configuration file %s: %w", path, err)
			return
		}
		defer file.Close()

		var selected strings.Builder
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			key := configurationLineKey(line)
			if key != "ANNAS_BASE_URL" && key != "ANNAS_AUTO_BASE_URL" {
				continue
			}
			selected.WriteString(line)
			selected.WriteByte('\n')
		}
		if err := scanner.Err(); err != nil {
			cachedPublicConfig.err = fmt.Errorf("failed to read configuration file %s: %w", path, err)
			return
		}

		values, err := godotenv.Unmarshal(selected.String())
		if err != nil {
			cachedPublicConfig.err = fmt.Errorf("failed to parse public configuration in %s", path)
			return
		}
		cachedPublicConfig.found = true
		cachedPublicConfig.value, cachedPublicConfig.err = parsePublicConfiguration(path, values)
	})

	return cachedPublicConfig
}

func configurationLineKey(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}
	key, _, found := strings.Cut(trimmed, "=")
	if !found {
		return ""
	}
	return strings.TrimSpace(key)
}

func parsePublicConfiguration(path string, values map[string]string) (publicConfiguration, error) {
	configuration := publicConfiguration{
		AnnasBaseURL: DefaultAnnasBaseURL,
	}
	if baseURL := normalizeBaseURL(values["ANNAS_BASE_URL"]); baseURL != "" {
		configuration.AnnasBaseURL = baseURL
	}

	autoValue := strings.TrimSpace(values["ANNAS_AUTO_BASE_URL"])
	if autoValue == "" {
		return configuration, nil
	}
	autoAnnasBaseURL, err := strconv.ParseBool(autoValue)
	if err != nil {
		return publicConfiguration{}, fmt.Errorf("ANNAS_AUTO_BASE_URL in %s must be a boolean", path)
	}
	configuration.AutoAnnasBaseURL = autoAnnasBaseURL
	return configuration, nil
}

func parseConfiguration(path string, values map[string]string) (*Env, error) {
	requiredKeys := []string{
		"ANNAS_SECRET_KEY",
		"ANNAS_DOWNLOAD_PATH",
		"ANNAS_BASE_URL",
		"ANNAS_AUTO_BASE_URL",
	}
	missingKeys := make([]string, 0)
	for _, key := range requiredKeys {
		if strings.TrimSpace(values[key]) == "" {
			missingKeys = append(missingKeys, key)
		}
	}
	if len(missingKeys) > 0 {
		return nil, fmt.Errorf("configuration file %s must set: %s", path, strings.Join(missingKeys, ", "))
	}

	downloadPath := strings.TrimSpace(values["ANNAS_DOWNLOAD_PATH"])
	if !filepath.IsAbs(downloadPath) {
		return nil, fmt.Errorf("ANNAS_DOWNLOAD_PATH in %s must be an absolute path", path)
	}

	annasBaseURL := normalizeBaseURL(values["ANNAS_BASE_URL"])
	if annasBaseURL == "" {
		return nil, fmt.Errorf("ANNAS_BASE_URL in %s must not be empty", path)
	}

	autoAnnasBaseURL, err := strconv.ParseBool(strings.TrimSpace(values["ANNAS_AUTO_BASE_URL"]))
	if err != nil {
		return nil, fmt.Errorf("ANNAS_AUTO_BASE_URL in %s must be a boolean", path)
	}

	return &Env{
		SecretKey:        values["ANNAS_SECRET_KEY"],
		DownloadPath:     downloadPath,
		AnnasBaseURL:     annasBaseURL,
		AutoAnnasBaseURL: autoAnnasBaseURL,
	}, nil
}

func GetAnnasBaseURL(ctx context.Context, timeout time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if timeout <= 0 {
		return "", errors.New("mirror timeout must be greater than zero")
	}
	state := loadPublicConfiguration()
	if state.err != nil {
		return "", state.err
	}
	if !state.found {
		return DefaultAnnasBaseURL, nil
	}
	if !state.value.AutoAnnasBaseURL {
		return state.value.AnnasBaseURL, nil
	}

	resolvedEnvMu.Lock()
	cached := resolvedAnnasBaseURL
	resolvedEnvMu.Unlock()
	if cached != "" {
		return cached, nil
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resolved, err := resolveAnnasBaseURL(discoveryCtx, state.value.AnnasBaseURL)
	if discoveryCtx.Err() != nil {
		return "", discoveryCtx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	if err != nil {
		logger.GetLogger().Warn("Automatic Anna mirror resolution failed, using configured mirror", zap.Error(err))
		return state.value.AnnasBaseURL, nil
	}
	if resolved = normalizeBaseURL(resolved); resolved != "" {
		resolvedEnvMu.Lock()
		resolvedAnnasBaseURL = resolved
		resolvedEnvMu.Unlock()
		return resolved, nil
	}

	return state.value.AnnasBaseURL, nil
}

// GetAuthenticatedAnnasBaseURL returns only the host from the fixed per-user
// configuration file. Automatically discovered mirrors are suitable for public
// requests, but they must never receive ANNAS_SECRET_KEY.
func GetAuthenticatedAnnasBaseURL() (string, error) {
	state := loadConfiguration()
	if state.err != nil {
		return "", state.err
	}
	if !state.found {
		return "", fmt.Errorf("configuration file not found: %s", state.path)
	}
	return state.value.AnnasBaseURL, nil
}

func GetEnv() (*Env, error) {
	state := loadConfiguration()
	if state.err != nil {
		return nil, state.err
	}
	if !state.found {
		return nil, fmt.Errorf("configuration file not found: %s", state.path)
	}

	value := *state.value
	return &value, nil
}

func defaultResolveAnnasBaseURL(ctx context.Context, fallbackBaseURL string) (string, error) {
	resolver := mirror.NewResolver(nil, mirror.DefaultStatusPageURL, nil)
	return resolver.Resolve(ctx, mirror.ResolveOptions{FallbackBaseURL: fallbackBaseURL})
}

func normalizeBaseURL(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimSuffix(value, "/")
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	return value
}

func resetResolvedEnvForTests() {
	configurationOnce = sync.Once{}
	cachedConfiguration = configurationState{}
	publicConfigOnce = sync.Once{}
	cachedPublicConfig = publicConfigurationState{}
	resolvedEnvMu = sync.Mutex{}
	resolvedAnnasBaseURL = ""
}
