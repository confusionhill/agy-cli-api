package server

import (
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/dika/agy-cli-api/internal/agy"
	"github.com/dika/agy-cli-api/internal/config"
	"github.com/dika/agy-cli-api/internal/store"
)

// Server bundles HTTP routing, the CLI Runner, and the ConversationStore.
type Server struct {
	cfg    *config.Config
	runner *agy.Runner
	store  *store.ConversationStore
	logger *slog.Logger
}

// New creates a new Server instance.
func New(cfg *config.Config, runner *agy.Runner, convStore *store.ConversationStore, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg != nil && cfg.DataDir != "" && runner != nil {
		runner.SetUploadsDir(filepath.Join(cfg.DataDir, "uploads"))
	}
	return &Server{
		cfg:    cfg,
		runner: runner,
		store:  convStore,
		logger: logger,
	}
}

// Handler returns the configured http.Handler with all routes and middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/v1/models", s.handleModels)
	mux.HandleFunc("GET /api/v1/usage", s.handleUsage)
	mux.HandleFunc("GET /api/v1/mcp", s.handleMCP)

	mux.HandleFunc("GET /api/v1/conversations", s.handleListConversations)
	mux.HandleFunc("GET /api/v1/conversations/search", s.handleSearchConversations)
	mux.HandleFunc("GET /api/v1/conversations/{id}", s.handleGetConversation)
	mux.HandleFunc("POST /api/v1/conversations/{id}/cancel", s.handleCancelConversation)

	mux.HandleFunc("POST /api/v1/uploads", s.handleUploads)
	mux.HandleFunc("GET /api/v1/media", s.handleMedia)

	mux.HandleFunc("POST /api/v1/chat", s.handleChat)
	mux.HandleFunc("POST /api/v1/chat/stream", s.handleChatStream)

	var handler http.Handler = mux
	handler = LoggingMiddleware(s.logger)(handler)
	handler = CORSMiddleware(s.cfg.CORSOrigins)(handler)
	return handler
}
