package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/dika/agy-cli-api/internal/agy"
	"github.com/dika/agy-cli-api/internal/config"
	"github.com/dika/agy-cli-api/internal/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// UserSession stores per-chat state (active conversation, workspace folder, model, effort).
type UserSession struct {
	mu                   sync.Mutex
	ActiveConversationID string
	ActiveWorkspaceDir   string
	ActiveWorkspaceLabel string
	Model                string
	Effort               string
	CancelFunc           context.CancelFunc
}

// Bot manages Telegram Bot interactions with the local Antigravity agent.
type Bot struct {
	api      *tgbotapi.BotAPI
	cfg      *config.Config
	runner   *agy.Runner
	store    *store.ConversationStore
	logger   *slog.Logger
	sessions sync.Map // map[int64]*UserSession
}

// New creates a new Telegram Bot instance.
func New(cfg *config.Config, runner *agy.Runner, store *store.ConversationStore, logger *slog.Logger) (*Bot, error) {
	if cfg.TelegramBotToken == "" {
		return nil, errors.New("telegram bot token is empty")
	}

	api, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		return nil, fmt.Errorf("init telegram bot: %w", err)
	}

	return &Bot{
		api:    api,
		cfg:    cfg,
		runner: runner,
		store:  store,
		logger: logger,
	}, nil
}

// Start begins long-polling for Telegram updates until ctx is canceled.
func (b *Bot) Start(ctx context.Context) error {
	b.logger.Info("telegram bot connected",
		"username", b.api.Self.UserName,
		"allowed_users_count", len(b.cfg.TelegramAllowedUsers),
	)

	// Register bot command menu
	commands := []tgbotapi.BotCommand{
		{Command: "new", Description: "Start a new conversation (/new [folder])"},
		{Command: "folders", Description: "Pick project workspace folder"},
		{Command: "convs", Description: "Pick recent conversation"},
		{Command: "models", Description: "Pick AI model"},
		{Command: "effort", Description: "Set reasoning effort (low/medium/high)"},
		{Command: "usage", Description: "View model quotas & limits"},
		{Command: "mcp", Description: "List connected MCP tools"},
		{Command: "cancel", Description: "Cancel current turn"},
		{Command: "status", Description: "Show active session details"},
		{Command: "help", Description: "Show commands & cheatsheet"},
	}
	_, _ = b.api.Request(tgbotapi.NewSetMyCommands(commands...))

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("stopping telegram bot...")
			b.api.StopReceivingUpdates()
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			go b.handleUpdate(update)
		}
	}
}

// getSession retrieves or creates a session for the given chat ID.
func (b *Bot) getSession(chatID int64) *UserSession {
	val, ok := b.sessions.Load(chatID)
	if ok {
		return val.(*UserSession)
	}

	s := &UserSession{
		ActiveWorkspaceDir:   b.cfg.DefaultWorkspace,
		ActiveWorkspaceLabel: getFolderLabel(b.cfg.DefaultWorkspace),
		Model:                "gemini-4-argon-medium",
		Effort:               "medium",
	}
	b.sessions.Store(chatID, s)
	return s
}

// isUserAllowed checks if a Telegram user ID is authorized.
func (b *Bot) isUserAllowed(userID int64) bool {
	if len(b.cfg.TelegramAllowedUsers) == 0 {
		return true // No restriction configured
	}
	for _, id := range b.cfg.TelegramAllowedUsers {
		if id == userID {
			return true
		}
	}
	return false
}

