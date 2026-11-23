package config

import (
	"os"
	"strings"
)

// Config holds server configuration loaded from environment variables.
type Config struct {
	Port      string
	DataDir   string
	AdminUser string
	AdminPass string
	LogLevel  string
	BaseURL   string
}

// Load reads CourseFin configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		Port:      envOrDefault("COURSEFIN_PORT", "8096"),
		DataDir:   envOrDefault("COURSEFIN_DATA_DIR", "/data"),
		AdminUser: strings.TrimSpace(os.Getenv("COURSEFIN_ADMIN_USER")),
		AdminPass: os.Getenv("COURSEFIN_ADMIN_PASS"),
		LogLevel:  envOrDefault("COURSEFIN_LOG_LEVEL", "info"),
		BaseURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("COURSEFIN_BASE_URL")), "/"),
	}
}

func envOrDefault(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}
