package config_test

import (
	"testing"

	"coursefin/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("COURSEFIN_PORT", "")
	t.Setenv("COURSEFIN_DATA_DIR", "")
	t.Setenv("COURSEFIN_ADMIN_USER", "")
	t.Setenv("COURSEFIN_ADMIN_PASS", "")
	t.Setenv("COURSEFIN_LOG_LEVEL", "")
	t.Setenv("COURSEFIN_BASE_URL", "")

	cfg := config.Load()
	if cfg.Port != "8096" {
		t.Fatalf("expected default Port 8096, got %q", cfg.Port)
	}
	if cfg.DataDir != "/data" {
		t.Fatalf("expected default DataDir /data, got %q", cfg.DataDir)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("expected default LogLevel info, got %q", cfg.LogLevel)
	}
	if cfg.AdminUser != "" || cfg.AdminPass != "" || cfg.BaseURL != "" {
		t.Fatalf("expected empty optional fields, got %+v", cfg)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("COURSEFIN_PORT", "9090")
	t.Setenv("COURSEFIN_DATA_DIR", "/tmp/coursefin-data")
	t.Setenv("COURSEFIN_ADMIN_USER", "root")
	t.Setenv("COURSEFIN_ADMIN_PASS", "secret")
	t.Setenv("COURSEFIN_LOG_LEVEL", "debug")
	t.Setenv("COURSEFIN_BASE_URL", "https://course.example.com")

	cfg := config.Load()
	if cfg.Port != "9090" {
		t.Fatalf("expected Port 9090, got %q", cfg.Port)
	}
	if cfg.DataDir != "/tmp/coursefin-data" {
		t.Fatalf("expected DataDir /tmp/coursefin-data, got %q", cfg.DataDir)
	}
	if cfg.AdminUser != "root" {
		t.Fatalf("expected AdminUser root, got %q", cfg.AdminUser)
	}
	if cfg.AdminPass != "secret" {
		t.Fatalf("expected AdminPass secret, got %q", cfg.AdminPass)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("expected LogLevel debug, got %q", cfg.LogLevel)
	}
	if cfg.BaseURL != "https://course.example.com" {
		t.Fatalf("expected BaseURL https://course.example.com, got %q", cfg.BaseURL)
	}
}
