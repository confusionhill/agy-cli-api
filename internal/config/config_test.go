package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("AGY_DATA_DIR", "~/custom-data")
	t.Setenv("AGY_DEFAULT_PERMISSION_MODE", "auto")
	t.Setenv("AGY_CORS_ORIGINS", "http://localhost:3000, https://app.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Port != "9090" {
		t.Errorf("expected Port 9090, got %s", cfg.Port)
	}

	homeDir, _ := os.UserHomeDir()
	expectedDataDir := filepath.Join(homeDir, "custom-data")
	if cfg.DataDir != expectedDataDir {
		t.Errorf("expected DataDir %s, got %s", expectedDataDir, cfg.DataDir)
	}

	if cfg.DefaultPermissionMode != "auto" {
		t.Errorf("expected DefaultPermissionMode auto, got %s", cfg.DefaultPermissionMode)
	}

	if len(cfg.CORSOrigins) != 2 || cfg.CORSOrigins[0] != "http://localhost:3000" || cfg.CORSOrigins[1] != "https://app.example.com" {
		t.Errorf("unexpected CORSOrigins: %v", cfg.CORSOrigins)
	}
}
