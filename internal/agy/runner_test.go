package agy

import (
	"reflect"
	"testing"
)

func TestBuildArgs(t *testing.T) {
	r := NewRunner("agy", "/workspace", "skip-permissions")

	req := ChatRequest{
		Prompt:               "hello world",
		ConversationID:       "conv-123",
		AddDirs:              []string{"/extra/dir1", "/extra/dir2"},
		Model:                "gemini-4-argon-medium",
		Effort:               "high",
		Agent:                "research",
		PermissionMode:       "auto",
		DisableSlashCommands: true,
	}

	got := r.BuildArgs(req, true)
	want := []string{
		"-p", "hello world",
		"--output-format", "stream-json",
		"--conversation", "conv-123",
		"--add-dir", "/extra/dir1",
		"--add-dir", "/extra/dir2",
		"--model", "gemini-4-argon-medium",
		"--effort", "high",
		"--agent", "research",
		"--disable-slash-commands",
		"--mode", "auto",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildArgs() =\n%v\nwant:\n%v", got, want)
	}

	// Default skip-permissions mode
	reqDefault := ChatRequest{Prompt: "ping"}
	gotDefault := r.BuildArgs(reqDefault, false)
	wantDefault := []string{
		"-p", "ping",
		"--output-format", "json",
		"--dangerously-skip-permissions",
	}
	if !reflect.DeepEqual(gotDefault, wantDefault) {
		t.Errorf("BuildArgs(default) =\n%v\nwant:\n%v", gotDefault, wantDefault)
	}
}

func TestParseStreamLine(t *testing.T) {
	line := []byte(`{"event":"step_update","step_update":{"conversation_id":"9506f739-9dce-4233-87ae-528bd688db80","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"PONG"}}`)
	ev, err := ParseStreamLine(line)
	if err != nil {
		t.Fatalf("ParseStreamLine failed: %v", err)
	}
	if ev.Event != "step_update" {
		t.Errorf("expected event step_update, got %s", ev.Event)
	}
	if ev.ConversationID != "9506f739-9dce-4233-87ae-528bd688db80" {
		t.Errorf("expected ConversationID 9506f739-9dce-4233-87ae-528bd688db80, got %s", ev.ConversationID)
	}
}

func TestParseUsageJSON(t *testing.T) {
	raw := []byte(`{"conversation_id":"","status":"SUCCESS","response":"...","command":{"name":"usage","data":{"description":"Quota summary","groups":[{"name":"Gemini 4 Argon","description":"Argon quota","buckets":[{"id":"gemini-argon-wtu-weekly","name":"Gemini 4 Argon Weekly Limit","window":"weekly","remaining_fraction":0.2976485,"reset_time":"2026-11-14T08:35:24Z"}]}]}}}`)
	usage, err := ParseUsageJSON(raw)
	if err != nil {
		t.Fatalf("ParseUsageJSON failed: %v", err)
	}
	if len(usage.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(usage.Groups))
	}
	bucket := usage.Groups[0].Buckets[0]
	if bucket.ID != "gemini-argon-wtu-weekly" {
		t.Errorf("expected bucket ID gemini-argon-wtu-weekly, got %s", bucket.ID)
	}
	if bucket.RemainingPercent != 29.76 {
		t.Errorf("expected RemainingPercent 29.76, got %v", bucket.RemainingPercent)
	}
}

func TestParseModelsOutput(t *testing.T) {
	raw := "\r⠋ Fetching available models...\r⠙ Fetching available models...\rgemini-4-argon-medium     Gemini 4 Argon (Medium)\nclaude-sonnet-5-5-high    Claude Sonnet 5.5 (High)\n"
	models := ParseModelsOutput(raw)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(models), models)
	}
	if models[0].ID != "gemini-4-argon-medium" || models[0].Name != "Gemini 4 Argon (Medium)" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	if models[1].ID != "claude-sonnet-5-5-high" || models[1].Name != "Claude Sonnet 5.5 (High)" {
		t.Errorf("unexpected second model: %+v", models[1])
	}
}

func TestParseMCPListOutput(t *testing.T) {
	raw := `NAME                   TYPE   STATUS   COMMAND/URL
codegraph              stdio  enabled  /Users/dika/.local/bin/codegraph serve --mcp
open-design            stdio  enabled  /Applications/Open Design.app/Contents/Helper mcp
`
	servers := ParseMCPListOutput(raw)
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
	if servers[0].Name != "codegraph" || servers[0].CommandURL != "/Users/dika/.local/bin/codegraph serve --mcp" {
		t.Errorf("unexpected server[0]: %+v", servers[0])
	}
	if servers[1].Name != "open-design" || servers[1].CommandURL != "/Applications/Open Design.app/Contents/Helper mcp" {
		t.Errorf("unexpected server[1]: %+v", servers[1])
	}
}
