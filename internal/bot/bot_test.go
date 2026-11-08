package bot

import (
	"log/slog"
	"os"
	"testing"

	"github.com/dika/agy-cli-api/internal/config"
)

func TestBotUserAuthorization(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// When TelegramAllowedUsers is empty, anyone is allowed
	bEmpty := &Bot{
		cfg:    &config.Config{TelegramAllowedUsers: []int64{}},
		logger: logger,
	}
	if !bEmpty.isUserAllowed(12345) {
		t.Errorf("expected user 12345 to be allowed when no restrictions configured")
	}

	// When TelegramAllowedUsers has specific IDs
	bRestricted := &Bot{
		cfg: &config.Config{
			TelegramAllowedUsers: []int64{100, 200, 300},
		},
		logger: logger,
	}
	if !bRestricted.isUserAllowed(100) {
		t.Errorf("expected user 100 to be allowed")
	}
	if !bRestricted.isUserAllowed(200) {
		t.Errorf("expected user 200 to be allowed")
	}
	if bRestricted.isUserAllowed(999) {
		t.Errorf("expected user 999 to be rejected")
	}
}

func TestGetFolderLabel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"file:///Users/dika/Documents/github/akalin-ai", "akalin-ai"},
		{"/Users/dika/Documents/github/touch-some-grass/", "touch-some-grass"},
		{"", "General"},
	}

	for _, tt := range tests {
		got := getFolderLabel(tt.input)
		if got != tt.want {
			t.Errorf("getFolderLabel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenderProgressBar(t *testing.T) {
	full := renderProgressBar(100)
	if full != "██████████" {
		t.Errorf("expected 10 filled blocks, got %s", full)
	}

	half := renderProgressBar(50)
	if half != "█████░░░░░" {
		t.Errorf("expected 5 filled and 5 empty blocks, got %s", half)
	}

	empty := renderProgressBar(0)
	if empty != "░░░░░░░░░░" {
		t.Errorf("expected 10 empty blocks, got %s", empty)
	}
}

