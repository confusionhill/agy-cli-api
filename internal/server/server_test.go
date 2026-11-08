package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dika/agy-cli-api/internal/agy"
	"github.com/dika/agy-cli-api/internal/config"
	"github.com/dika/agy-cli-api/internal/store"
)

func createFakeAgyBinary(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "fake-agy")
	script := `#!/bin/sh
if [ "$1" = "--help" ]; then
  echo "Usage of agy:"
  exit 0
fi
if [ "$1" = "-p" ] && [ "$2" = "/usage" ]; then
  echo '{"conversation_id":"","status":"SUCCESS","response":"ok","command":{"name":"usage","data":{"description":"test","groups":[{"name":"Gemini Models","description":"desc","buckets":[{"id":"gemini-weekly","name":"Weekly","window":"weekly","remaining_fraction":0.75,"reset_time":"2026-11-14T00:00:00Z"}]}]}}}'
  exit 0
fi
echo '{"event":"init","conversation_id":"test-conv-1","init":{"cwd":"/tmp"}}'
echo '{"event":"step_update","step_update":{"conversation_id":"test-conv-1","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"Hello from fake agy!"}}'
echo '{"event":"result","result":{"conversation_id":"test-conv-1","status":"SUCCESS","response":"Hello from fake agy!","duration_seconds":0.5,"num_turns":1}}'
`
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake-agy: %v", err)
	}
	return binPath
}

func TestServerEndpointsAndSSE(t *testing.T) {
	fakeBin := createFakeAgyBinary(t)
	tmpDataDir := t.TempDir()

	cfg := &config.Config{
		Port:                  "8080",
		AgyBin:                fakeBin,
		DataDir:               tmpDataDir,
		DefaultWorkspace:      tmpDataDir,
		DefaultPermissionMode: "skip-permissions",
		CORSOrigins:           []string{"*"},
	}

	runner := agy.NewRunner(cfg.AgyBin, cfg.DefaultWorkspace, cfg.DefaultPermissionMode)
	convStore := store.NewConversationStore(cfg.DataDir)
	srv := New(cfg, runner, convStore, nil)
	handler := srv.Handler()

	// 1. Test OPTIONS CORS preflight
	reqOpt := httptest.NewRequest(http.MethodOptions, "/api/v1/chat", nil)
	reqOpt.Header.Set("Origin", "http://localhost:3000")
	recOpt := httptest.NewRecorder()
	handler.ServeHTTP(recOpt, reqOpt)
	if recOpt.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS, got %d", recOpt.Code)
	}
	if recOpt.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("missing CORS header")
	}

	// 2. Test GET /healthz
	reqHealth := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recHealth := httptest.NewRecorder()
	handler.ServeHTTP(recHealth, reqHealth)
	if recHealth.Code != http.StatusOK {
		t.Fatalf("expected 200 from /healthz, got %d", recHealth.Code)
	}

	// 3. Test GET /api/v1/usage
	reqUsage := httptest.NewRequest(http.MethodGet, "/api/v1/usage", nil)
	recUsage := httptest.NewRecorder()
	handler.ServeHTTP(recUsage, reqUsage)
	if recUsage.Code != http.StatusOK {
		t.Fatalf("expected 200 from /api/v1/usage, got %d: %s", recUsage.Code, recUsage.Body.String())
	}
	var usage agy.UsageData
	if err := json.Unmarshal(recUsage.Body.Bytes(), &usage); err != nil {
		t.Fatalf("unmarshal usage: %v", err)
	}
	if len(usage.Groups) != 1 || usage.Groups[0].Buckets[0].RemainingPercent != 75 {
		t.Errorf("unexpected usage response: %+v", usage)
	}

	// 4. Test POST /api/v1/uploads & GET /api/v1/media
	var mpBuf bytes.Buffer
	mpWriter := multipart.NewWriter(&mpBuf)
	fw, err := mpWriter.CreateFormFile("file", "screen.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = fw.Write([]byte("\x89PNG\r\n\x1a\nfake-png-content"))
	_ = mpWriter.Close()

	reqUp := httptest.NewRequest(http.MethodPost, "/api/v1/uploads", &mpBuf)
	reqUp.Header.Set("Content-Type", mpWriter.FormDataContentType())
	recUp := httptest.NewRecorder()
	handler.ServeHTTP(recUp, reqUp)
	if recUp.Code != http.StatusOK {
		t.Fatalf("expected 200 from /api/v1/uploads, got %d: %s", recUp.Code, recUp.Body.String())
	}
	var upResp struct {
		Files []agy.UploadedFile `json:"files"`
		Count int                `json:"count"`
	}
	if err := json.Unmarshal(recUp.Body.Bytes(), &upResp); err != nil || upResp.Count != 1 {
		t.Fatalf("unexpected upload response: %+v (err=%v)", upResp, err)
	}

	// Fetch uploaded file via GET /api/v1/media
	reqMedia := httptest.NewRequest(http.MethodGet, "/api/v1/media?uri="+upResp.Files[0].URI, nil)
	recMedia := httptest.NewRecorder()
	handler.ServeHTTP(recMedia, reqMedia)
	if recMedia.Code != http.StatusOK {
		t.Fatalf("expected 200 from /api/v1/media, got %d: %s", recMedia.Code, recMedia.Body.String())
	}
	if !strings.Contains(recMedia.Body.String(), "fake-png-content") {
		t.Errorf("unexpected media body: %q", recMedia.Body.String())
	}

	// 5. Test POST /api/v1/chat/stream (SSE) with inline base64 attachment
	body := bytes.NewBufferString(`{"prompt":"describe image","attachments":[{"filename":"shot.jpg","mime_type":"image/jpeg","data_base64":"ZmFrZQ=="}]}`)
	reqStream := httptest.NewRequest(http.MethodPost, "/api/v1/chat/stream", body)
	recStream := httptest.NewRecorder()
	handler.ServeHTTP(recStream, reqStream)
	if recStream.Code != http.StatusOK {
		t.Fatalf("expected 200 from /api/v1/chat/stream, got %d", recStream.Code)
	}
	sseOut := recStream.Body.String()
	if !strings.Contains(sseOut, "event: init") ||
		!strings.Contains(sseOut, "event: step_update") ||
		!strings.Contains(sseOut, "event: result") ||
		!strings.Contains(sseOut, "event: done") {
		t.Errorf("unexpected SSE stream output:\n%s", sseOut)
	}

	// 6. Test GET /v1/models (OpenAI compatibility)
	reqOAIModels := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	recOAIModels := httptest.NewRecorder()
	handler.ServeHTTP(recOAIModels, reqOAIModels)
	if recOAIModels.Code != http.StatusOK {
		t.Fatalf("expected 200 from /v1/models, got %d: %s", recOAIModels.Code, recOAIModels.Body.String())
	}
	var oaiModels struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recOAIModels.Body.Bytes(), &oaiModels); err != nil || oaiModels.Object != "list" || len(oaiModels.Data) == 0 {
		t.Fatalf("unexpected /v1/models response: %+v (err=%v)", oaiModels, err)
	}

	// 7. Test POST /v1/chat/completions (OpenAI non-streaming)
	oaiJSON := `{"model":"gemini-4-argon-medium","messages":[{"role":"user","content":"Hello from test"}],"stream":false}`
	reqOAIChat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(oaiJSON))
	recOAIChat := httptest.NewRecorder()
	handler.ServeHTTP(recOAIChat, reqOAIChat)
	if recOAIChat.Code != http.StatusOK {
		t.Fatalf("expected 200 from /v1/chat/completions, got %d: %s", recOAIChat.Code, recOAIChat.Body.String())
	}
	var oaiResp struct {
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(recOAIChat.Body.Bytes(), &oaiResp); err != nil || oaiResp.Object != "chat.completion" {
		t.Fatalf("unexpected /v1/chat/completions response: %+v (err=%v)", oaiResp, err)
	}
	if !strings.Contains(oaiResp.Choices[0].Message.Content, "Hello from fake agy!") {
		t.Errorf("unexpected completion content: %q", oaiResp.Choices[0].Message.Content)
	}

	// 8. Test POST /v1/chat/completions (OpenAI streaming SSE)
	oaiStreamJSON := `{"model":"gemini-4-argon-medium","messages":[{"role":"user","content":"Stream this"}],"stream":true}`
	reqOAIStream := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(oaiStreamJSON))
	recOAIStream := httptest.NewRecorder()
	handler.ServeHTTP(recOAIStream, reqOAIStream)
	if recOAIStream.Code != http.StatusOK {
		t.Fatalf("expected 200 from streaming /v1/chat/completions, got %d", recOAIStream.Code)
	}
	oaiStreamOut := recOAIStream.Body.String()
	if !strings.Contains(oaiStreamOut, "chat.completion.chunk") || !strings.Contains(oaiStreamOut, "data: [DONE]") {
		t.Errorf("unexpected OpenAI SSE output:\n%s", oaiStreamOut)
	}
}
