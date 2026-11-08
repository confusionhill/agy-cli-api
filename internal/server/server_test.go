package server

import (
	"bytes"
	"encoding/json"
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

	// 4. Test POST /api/v1/chat/stream (SSE)
	body := bytes.NewBufferString(`{"prompt":"hello"}`)
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
}
