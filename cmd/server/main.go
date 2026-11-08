package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dika/agy-cli-api/internal/agy"
	"github.com/dika/agy-cli-api/internal/bot"
	"github.com/dika/agy-cli-api/internal/config"
	"github.com/dika/agy-cli-api/internal/server"
	"github.com/dika/agy-cli-api/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	runner := agy.NewRunner(cfg.AgyBin, cfg.DefaultWorkspace, cfg.DefaultPermissionMode)
	convStore := store.NewConversationStore(cfg.DataDir)
	convStore.SetDefaultWorkspace(cfg.DefaultWorkspace)
	srv := server.New(cfg, runner, convStore, logger)

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start Telegram Bot if enabled and token provided
	botCtx, botCancel := context.WithCancel(context.Background())
	defer botCancel()

	if cfg.TelegramEnabled && cfg.TelegramBotToken != "" {
		tgBot, err := bot.New(cfg, runner, convStore, logger)
		if err != nil {
			logger.Warn("failed to initialize telegram bot (skipping)", "error", err)
		} else {
			go func() {
				if bErr := tgBot.Start(botCtx); bErr != nil {
					logger.Warn("telegram bot terminated", "error", bErr)
				}
			}()
		}
	} else {
		logger.Info("telegram bot disabled (TELEGRAM_ENABLED=false or TELEGRAM_BOT_TOKEN not provided)")
	}

	go func() {
		logger.Info("starting agy-cli-api server",
			"addr", httpServer.Addr,
			"agy_bin", cfg.AgyBin,
			"data_dir", cfg.DataDir,
			"default_workspace", cfg.DefaultWorkspace,
			"default_permission_mode", cfg.DefaultPermissionMode,
			"telegram_enabled", cfg.TelegramEnabled,
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down agy-cli-api server and telegram bot...")
	botCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
	logger.Info("server stopped")
}

