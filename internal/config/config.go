package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds server configuration loaded from environment variables.
type Config struct {
	Port               string
	DataDir            string
	AdminUser          string
	AdminPass          string
	LogLevel           string
	BaseURL            string
	Version            string
	UpdateRepo         string
	UpdateAPIBase      string
	UpdateGitHubToken  string
	UpdateAutoCheckEnv *bool
	ManagedBinDir      string
}

// Load reads CourseFin configuration from environment variables with sensible defaults.
func Load() Config {
	var autoCheckEnv *bool
	if raw := strings.TrimSpace(os.Getenv("COURSEFIN_UPDATE_AUTO_CHECK")); raw != "" {
		if b, err := strconv.ParseBool(raw); err == nil {
			autoCheckEnv = &b
		}
	}

	managedBinDir := strings.TrimSpace(os.Getenv("COURSEFIN_MANAGED_BIN_DIR"))
	if managedBinDir != "" {
		managedBinDir = filepath.Clean(managedBinDir)
	}

	return Config{
		Port:               envOrDefault("COURSEFIN_PORT", "8096"),
		DataDir:            envOrDefault("COURSEFIN_DATA_DIR", "/data"),
		AdminUser:          strings.TrimSpace(os.Getenv("COURSEFIN_ADMIN_USER")),
		AdminPass:          os.Getenv("COURSEFIN_ADMIN_PASS"),
		LogLevel:           envOrDefault("COURSEFIN_LOG_LEVEL", "info"),
		BaseURL:            strings.TrimRight(strings.TrimSpace(os.Getenv("COURSEFIN_BASE_URL")), "/"),
		UpdateRepo:         strings.Trim(envOrDefault("COURSEFIN_UPDATE_REPO", "booooodv/CourseFin"), "/"),
		UpdateAPIBase:      strings.TrimRight(envOrDefault("COURSEFIN_UPDATE_API_BASE", "https://api.github.com"), "/"),
		UpdateGitHubToken:  strings.TrimSpace(os.Getenv("COURSEFIN_GITHUB_TOKEN")),
		UpdateAutoCheckEnv: autoCheckEnv,
		ManagedBinDir:      managedBinDir,
	}
}

func envOrDefault(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}
