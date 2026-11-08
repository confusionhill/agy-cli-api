package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dika/agy-cli-api/internal/agy"
	"github.com/dika/agy-cli-api/internal/store"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	err := s.runner.CheckBinary(ctx)
	status := "ok"
	errMsg := ""
	if err != nil {
		status = "degraded"
		errMsg = err.Error()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":                  status,
		"agy_bin":                 s.cfg.AgyBin,
		"default_workspace":       s.cfg.DefaultWorkspace,
		"default_permission_mode": s.cfg.DefaultPermissionMode,
		"error":                   errMsg,
		"timestamp":               time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	models, err := s.runner.ListModels(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	agents, _ := s.runner.ListAgents(ctx)
	if agents == nil {
		agents = []agy.AgentInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"models": models,
		"agents": agents,
	})
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := s.runner.GetUsage(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	servers, err := s.runner.ListMCP(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mcp_servers": servers,
	})
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	opts := store.ListOptions{
		Limit:     limit,
		Offset:    offset,
		Workspace: q.Get("workspace"),
		Status:    q.Get("status"),
	}

	items, err := s.store.List(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"conversations": items,
		"count":         len(items),
	})
}

func (s *Server) handleSearchConversations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	hits, err := s.store.Search(r.Context(), q, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"query":   q,
		"results": hits,
		"count":   len(hits),
	})
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	detail, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCancelConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cancelled := s.runner.CancelConversation(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation_id": id,
		"cancelled":       cancelled,
	})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req agy.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	res, err := s.runner.RunChat(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	var req agy.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported by response writer")
		return
	}

	events, err := s.runner.StreamChat(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for ev := range events {
		evName := ev.Event
		if evName == "" {
			evName = "message"
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evName, string(ev.Raw))
		flusher.Flush()
	}

	fmt.Fprintf(w, "event: done\ndata: {\"status\":\"completed\"}\n\n")
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}
