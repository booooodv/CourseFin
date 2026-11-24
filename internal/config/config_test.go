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
	t.Setenv("COURSEFIN_UPDATE_REPO", "")
	t.Setenv("COURSEFIN_UPDATE_API_BASE", "")
	t.Setenv("COURSEFIN_GITHUB_TOKEN", "")
	t.Setenv("COURSEFIN_UPDATE_AUTO_CHECK", "")
	t.Setenv("COURSEFIN_MANAGED_BIN_DIR", "")

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
	if cfg.Version != "" {
		t.Fatalf("expected empty default Version, got %q", cfg.Version)
	}
	if cfg.UpdateRepo != "booooodv/CourseFin" {
		t.Fatalf("expected default UpdateRepo booooodv/CourseFin, got %q", cfg.UpdateRepo)
	}
	if cfg.UpdateAPIBase != "https://api.github.com" {
		t.Fatalf("expected default UpdateAPIBase https://api.github.com, got %q", cfg.UpdateAPIBase)
	}
	if cfg.UpdateGitHubToken != "" {
		t.Fatalf("expected empty default UpdateGitHubToken, got %q", cfg.UpdateGitHubToken)
	}
	if cfg.UpdateAutoCheckEnv != nil {
		t.Fatalf("expected nil default UpdateAutoCheckEnv, got %v", *cfg.UpdateAutoCheckEnv)
	}
	if cfg.ManagedBinDir != "" {
		t.Fatalf("expected empty default ManagedBinDir, got %q", cfg.ManagedBinDir)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("COURSEFIN_PORT", "9090")
	t.Setenv("COURSEFIN_DATA_DIR", "/tmp/coursefin-data")
	t.Setenv("COURSEFIN_ADMIN_USER", "root")
	t.Setenv("COURSEFIN_ADMIN_PASS", "secret")
	t.Setenv("COURSEFIN_LOG_LEVEL", "debug")
	t.Setenv("COURSEFIN_BASE_URL", "https://course.example.com")
	t.Setenv("COURSEFIN_UPDATE_REPO", "acme/cf")
	t.Setenv("COURSEFIN_UPDATE_API_BASE", "https://gh.local/api/")
	t.Setenv("COURSEFIN_GITHUB_TOKEN", "ghp_test")
	t.Setenv("COURSEFIN_UPDATE_AUTO_CHECK", "false")
	t.Setenv("COURSEFIN_MANAGED_BIN_DIR", "/data/updates/bin")

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
	if cfg.UpdateRepo != "acme/cf" {
		t.Fatalf("expected UpdateRepo acme/cf, got %q", cfg.UpdateRepo)
	}
	if cfg.UpdateAPIBase != "https://gh.local/api" {
		t.Fatalf("expected UpdateAPIBase https://gh.local/api, got %q", cfg.UpdateAPIBase)
	}
	if cfg.UpdateGitHubToken != "ghp_test" {
		t.Fatalf("expected UpdateGitHubToken ghp_test, got %q", cfg.UpdateGitHubToken)
	}
	if cfg.UpdateAutoCheckEnv == nil || *cfg.UpdateAutoCheckEnv != false {
		t.Fatalf("expected UpdateAutoCheckEnv=false, got %v", cfg.UpdateAutoCheckEnv)
	}
	if cfg.ManagedBinDir != "/data/updates/bin" {
		t.Fatalf("expected ManagedBinDir /data/updates/bin, got %q", cfg.ManagedBinDir)
	}
}
