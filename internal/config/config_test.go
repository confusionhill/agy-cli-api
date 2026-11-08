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
	t.Setenv("TELEGRAM_ENABLED", "true")
	t.Setenv("TELEGRAM_BOT_TOKEN", "123456:ABC-DEF")
	t.Setenv("TELEGRAM_ALLOWED_USER_IDS", "111, 222, 333")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if !cfg.TelegramEnabled {
		t.Errorf("expected TelegramEnabled true, got false")
	}
	if cfg.TelegramBotToken != "123456:ABC-DEF" {
		t.Errorf("expected TelegramBotToken 123456:ABC-DEF, got %s", cfg.TelegramBotToken)
	}
	if len(cfg.TelegramAllowedUsers) != 3 || cfg.TelegramAllowedUsers[0] != 111 || cfg.TelegramAllowedUsers[1] != 222 || cfg.TelegramAllowedUsers[2] != 333 {
		t.Errorf("unexpected TelegramAllowedUsers: %v", cfg.TelegramAllowedUsers)
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

