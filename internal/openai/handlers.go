package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dika/agy-cli-api/internal/agy"
	"github.com/google/uuid"
)

// Handler serves OpenAI-compatible endpoints under /v1/...
type Handler struct {
	runner *agy.Runner
}

// NewHandler creates a new OpenAI compatibility handler.
func NewHandler(runner *agy.Runner) *Handler {
	return &Handler{
		runner: runner,
	}
}

// HandleChatCompletions processes POST /v1/chat/completions (streaming and non-streaming).
func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid request json: "+err.Error(), "invalid_request_error")
		return
	}

	// Check for optional conversation continuity header (e.g. x-conversation-id)
	if req.ConversationID == "" {
		req.ConversationID = r.Header.Get("X-Conversation-Id")
	}

	agyReq, err := ConvertRequestToAgy(req)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "failed to convert request: "+err.Error(), "invalid_request_error")
		return
	}

	if agyReq.Prompt == "" && len(agyReq.Attachments) == 0 {
		writeOpenAIError(w, http.StatusBadRequest, "messages array must not be empty", "invalid_request_error")
		return
	}

	// Route between streaming and non-streaming modes
	if req.Stream {
		h.handleStreamingCompletion(w, r, req, agyReq)
	} else {
		h.handleNonStreamingCompletion(w, r, req, agyReq)
	}
}

func (h *Handler) handleNonStreamingCompletion(w http.ResponseWriter, r *http.Request, req ChatCompletionRequest, agyReq agy.ChatRequest) {
	result, err := h.runner.RunChat(r.Context(), agyReq)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, err.Error(), "api_error")
		return
	}

	resp := FormatCompletionResponse(result, req.Model)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleStreamingCompletion(w http.ResponseWriter, r *http.Request, req ChatCompletionRequest, agyReq agy.ChatRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeOpenAIError(w, http.StatusInternalServerError, "streaming unsupported by server", "api_error")
		return
	}

	events, err := h.runner.StreamChat(r.Context(), agyReq)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, err.Error(), "api_error")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	completionID := "chatcmpl-" + uuid.New().String()
	model := req.Model
	if model == "" {
		model = "gemini-4-argon-medium"
	}

	// 1. Initial role chunk
	if initChunk, err := FormatInitialChunk(completionID, model); err == nil {
		_, _ = fmt.Fprint(w, initChunk)
		flusher.Flush()
	}

	// 2. Stream content deltas
	for ev := range events {
		if ev.ConversationID != "" && strings.HasPrefix(completionID, "chatcmpl-") && len(completionID) > 40 {
			completionID = "chatcmpl-" + ev.ConversationID
		}

		switch ev.Event {
		case "step_update":
			su := ev.StepUpdatePayload()
			if su != nil && su.TextDelta != "" {
				chunk, err := FormatCompletionChunk(completionID, model, su.TextDelta, nil)
				if err == nil {
					_, _ = fmt.Fprint(w, chunk)
					flusher.Flush()
				}
			}
		case "result":
			// Turn completed
		case "error":
			errChunk, _ := json.Marshal(map[string]any{
				"error": map[string]string{
					"message": ev.Error,
					"type":    "server_error",
				},
			})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(errChunk))
			flusher.Flush()
			return
		}
	}

	// 3. Final stop chunk
	stopReason := "stop"
	if finalChunk, err := FormatCompletionChunk(completionID, model, "", &stopReason); err == nil {
		_, _ = fmt.Fprint(w, finalChunk)
		flusher.Flush()
	}

	// 4. Standard [DONE] message
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// HandleListModels processes GET /v1/models.
func (h *Handler) HandleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}

	models, err := h.runner.ListModels(r.Context())
	if err != nil || len(models) == 0 {
		models = []agy.ModelInfo{
			{ID: "gemini-4-argon-medium", Name: "Gemini 4 Argon (Medium)"},
			{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"},
			{ID: "claude-3-5-sonnet", Name: "Claude 3.5 Sonnet"},
		}
	}

	resp := FormatModelList(models)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleGetModel processes GET /v1/models/{id}.
func (h *Handler) HandleGetModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}

	modelID := r.PathValue("id")
	if modelID == "" {
		writeOpenAIError(w, http.StatusBadRequest, "missing model id", "invalid_request_error")
		return
	}

	models, _ := h.runner.ListModels(r.Context())
	found := false
	for _, m := range models {
		if m.ID == modelID {
			found = true
			break
		}
	}

	// Allow default fallback models as valid
	if !found {
		switch modelID {
		case "gemini-4-argon-medium", "gemini-2.5-flash", "claude-3-5-sonnet":
			found = true
		}
	}

	if !found {
		writeOpenAIError(w, http.StatusNotFound, "model not found: "+modelID, "invalid_request_error")
		return
	}

	obj := ModelObject{
		ID:      modelID,
		Object:  "model",
		Created: time.Now().Unix(),
		OwnedBy: "antigravity",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(obj)
}

func writeOpenAIError(w http.ResponseWriter, status int, msg, errType string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(OpenAIErrorResponse{
		Error: OpenAIError{
			Message: msg,
			Type:    errType,
		},
	})
}
