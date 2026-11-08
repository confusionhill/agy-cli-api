package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestStore(t *testing.T) *ConversationStore {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "conversation_summaries.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE conversation_summaries (
			conversation_id text PRIMARY KEY,
			title text NOT NULL DEFAULT "",
			preview text NOT NULL DEFAULT "",
			step_count integer NOT NULL DEFAULT 0,
			last_modified_time datetime NOT NULL,
			workspace_uris text NOT NULL,
			status text NOT NULL DEFAULT "",
			source text NOT NULL DEFAULT "",
			project_id text NOT NULL DEFAULT "",
			agent_name text NOT NULL DEFAULT "",
			parent_conversation_id text NOT NULL DEFAULT "",
			last_user_input_time datetime NOT NULL
		);
		INSERT INTO conversation_summaries VALUES (
			'conv-1', 'Build Go API', 'lets build an api wrapper', 4,
			'2026-11-09 00:45:00+00:00', '["file:///Users/dika/Documents/github/akalin-ai"]',
			'CASCADE_RUN_STATUS_IDLE', '', 'default-cli-project', '', '', '2026-11-09 00:44:00+00:00'
		);
		INSERT INTO conversation_summaries VALUES (
			'conv-2', 'Other Project', 'fix bug in frontend', 2,
			'2026-11-08 10:00:00+00:00', '["file:///Users/dika/Documents/github/other"]',
			'CASCADE_RUN_STATUS_IDLE', '', 'default-cli-project', '', '', '2026-11-08 09:59:00+00:00'
		);
	`)
	if err != nil {
		t.Fatalf("seed test db: %v", err)
	}

	// Create transcript files for conv-1
	logsDir := filepath.Join(tmpDir, "brain", "conv-1", ".system_generated", "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}

	compactContent := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-09T00:44:00Z","content":"short"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-09T00:45:00Z","content":"truncated...","truncated_fields":["content"]}
`
	fullContent := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-09T00:44:00Z","content":"short"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-09T00:45:00Z","content":"full complete response from model"}
`
	if err := os.WriteFile(filepath.Join(logsDir, "transcript.jsonl"), []byte(compactContent), 0o644); err != nil {
		t.Fatalf("write transcript.jsonl: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "transcript_full.jsonl"), []byte(fullContent), 0o644); err != nil {
		t.Fatalf("write transcript_full.jsonl: %v", err)
	}

	return NewConversationStore(tmpDir)
}

func TestConversationStoreListSearchAndGet(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// 1. List all
	list, err := s.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(list))
	}
	if list[0].ConversationID != "conv-1" {
		t.Errorf("expected conv-1 first, got %s", list[0].ConversationID)
	}

	// 2. Filter by workspace
	filtered, err := s.List(ctx, ListOptions{Workspace: "akalin-ai"})
	if err != nil {
		t.Fatalf("List filtered failed: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ConversationID != "conv-1" {
		t.Errorf("unexpected filtered list: %+v", filtered)
	}

	// 3. Search
	hits, err := s.Search(ctx, "wrapper", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(hits) != 1 || hits[0].ConversationID != "conv-1" {
		t.Errorf("unexpected search hits: %+v", hits)
	}

	// 4. Get detail + full transcript resolution
	detail, err := s.Get(ctx, "conv-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if detail.Summary == nil || detail.Summary.Title != "Build Go API" {
		t.Errorf("unexpected summary: %+v", detail.Summary)
	}
	if len(detail.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(detail.Steps))
	}
	if detail.Steps[1].Content != "full complete response from model" {
		t.Errorf("expected full step content to be resolved, got %q", detail.Steps[1].Content)
	}
}

