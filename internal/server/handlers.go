package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
		"uploads_dir":             s.runner.UploadsDir(),
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

	includeChildren := q.Get("include_children") == "true"
	opts := store.ListOptions{
		Limit:           limit,
		Offset:          offset,
		Workspace:       q.Get("workspace"),
		Status:          q.Get("status"),
		IncludeChildren: includeChildren,
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

func (s *Server) handleUploads(w http.ResponseWriter, r *http.Request) {
	// Allow up to 100 MiB multipart uploads
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid multipart form: %v", err))
		return
	}
	if r.MultipartForm == nil || len(r.MultipartForm.File) == 0 {
		writeError(w, http.StatusBadRequest, "no files provided in multipart form")
		return
	}

	var uploaded []agy.UploadedFile
	for _, fileHeaders := range r.MultipartForm.File {
		for _, fh := range fileHeaders {
			f, err := fh.Open()
			if err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("open uploaded file: %v", err))
				return
			}
			contentType := fh.Header.Get("Content-Type")
			saved, saveErr := s.runner.SaveReaderAttachment(fh.Filename, contentType, f)
			_ = f.Close()
			if saveErr != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("save uploaded file: %v", saveErr))
				return
			}
			uploaded = append(uploaded, *saved)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"files": uploaded,
		"count": len(uploaded),
	})
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	rawTarget := strings.TrimSpace(r.URL.Query().Get("uri"))
	if rawTarget == "" {
		rawTarget = strings.TrimSpace(r.URL.Query().Get("path"))
	}
	rawTarget = strings.TrimPrefix(rawTarget, "file://")
	if rawTarget == "" {
		writeError(w, http.StatusBadRequest, "uri or path query parameter is required")
		return
	}

	cleanTarget, err := filepath.Abs(rawTarget)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file path")
		return
	}

	// Security check: only allow serving files from DataDir (brain/ or uploads/) or UploadsDir
	allowedDataDir, _ := filepath.Abs(s.cfg.DataDir)
	allowedUploadsDir, _ := filepath.Abs(s.runner.UploadsDir())

	inDataDir := allowedDataDir != "" && strings.HasPrefix(cleanTarget, allowedDataDir+string(filepath.Separator))
	inUploadsDir := allowedUploadsDir != "" && strings.HasPrefix(cleanTarget, allowedUploadsDir+string(filepath.Separator))

	if !inDataDir && !inUploadsDir {
		writeError(w, http.StatusForbidden, "media path outside allowed data/uploads directories")
		return
	}

	info, err := os.Stat(cleanTarget)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "media file not found")
		return
	}

	http.ServeFile(w, r, cleanTarget)
}

func hasPromptOrAttachment(req agy.ChatRequest) bool {
	if strings.TrimSpace(req.Prompt) != "" {
		return true
	}
	if len(req.Attachments) > 0 || len(req.AttachmentPaths) > 0 {
		return true
	}
	return false
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req agy.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	if !hasPromptOrAttachment(req) {
		writeError(w, http.StatusBadRequest, "prompt or attachment is required")
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
	if !hasPromptOrAttachment(req) {
		writeError(w, http.StatusBadRequest, "prompt or attachment is required")
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
