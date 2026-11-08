package store

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a requested conversation does not exist.
var ErrNotFound = errors.New("conversation not found")

// ConversationSummary represents metadata for a single conversation from conversation_summaries.db.
type ConversationSummary struct {
	ConversationID       string   `json:"conversation_id"`
	Title                string   `json:"title"`
	Preview              string   `json:"preview"`
	StepCount            int      `json:"step_count"`
	LastModifiedTime     string   `json:"last_modified_time"`
	WorkspaceURIs        []string `json:"workspace_uris"`
	Status               string   `json:"status"`
	Source               string   `json:"source,omitempty"`
	ProjectID            string   `json:"project_id,omitempty"`
	AgentName            string   `json:"agent_name,omitempty"`
	ParentConversationID string   `json:"parent_conversation_id,omitempty"`
	LastUserInputTime    string   `json:"last_user_input_time"`
}

// SearchHit represents a search match from the FTS5 index or summary search.
type SearchHit struct {
	ConversationID   string `json:"conversation_id"`
	Title            string `json:"title"`
	Workspace        string `json:"workspace"`
	Project          string `json:"project,omitempty"`
	UserSnippet      string `json:"user_snippet,omitempty"`
	AgentSnippet     string `json:"agent_snippet,omitempty"`
	StepCount        int    `json:"step_count"`
	LastModifiedTime string `json:"last_modified_time"`
}

// TranscriptStep represents a single step from transcript.jsonl.
type TranscriptStep struct {
	StepIndex       int             `json:"step_index"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Status          string          `json:"status"`
	CreatedAt       string          `json:"created_at"`
	Content         string          `json:"content,omitempty"`
	Thinking        string          `json:"thinking,omitempty"`
	ToolCalls       json.RawMessage `json:"tool_calls,omitempty"`
	Media           json.RawMessage `json:"media,omitempty"`
	InputTokens     int             `json:"input_tokens,omitempty"`
	OutputTokens    int             `json:"output_tokens,omitempty"`
	CacheReadTokens int             `json:"cache_read_tokens,omitempty"`
	TruncatedFields []string        `json:"truncated_fields,omitempty"`
}

// ConversationDetail combines summary metadata and the full transcript steps.
type ConversationDetail struct {
	Summary *ConversationSummary `json:"summary,omitempty"`
	Steps   []TranscriptStep     `json:"steps"`
}

// ListOptions defines filtering and pagination options for listing conversations.
type ListOptions struct {
	Limit     int
	Offset    int
	Workspace string
	Status    string
}

// ConversationStore reads conversation metadata and transcripts from the Antigravity data directory.
type ConversationStore struct {
	dataDir string
}

// NewConversationStore creates a new ConversationStore rooted at dataDir (e.g. ~/.gemini/antigravity-cli).
func NewConversationStore(dataDir string) *ConversationStore {
	return &ConversationStore{dataDir: dataDir}
}

func (s *ConversationStore) openDB() (*sql.DB, error) {
	dbPath := filepath.Join(s.dataDir, "conversation_summaries.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("stat conversation_summaries.db: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)", filepath.ToSlash(dbPath))
	return sql.Open("sqlite", dsn)
}

// List returns a paginated list of ConversationSummary items ordered by last_modified_time DESC.
func (s *ConversationStore) List(ctx context.Context, opts ListOptions) ([]ConversationSummary, error) {
	if opts.Limit <= 0 || opts.Limit > 200 {
		opts.Limit = 50
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}

	db, err := s.openDB()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []ConversationSummary{}, nil
		}
		return nil, err
	}
	defer db.Close()

	query := `
		SELECT
			conversation_id,
			title,
			preview,
			step_count,
			CAST(last_modified_time AS TEXT),
			workspace_uris,
			status,
			source,
			project_id,
			agent_name,
			parent_conversation_id,
			CAST(last_user_input_time AS TEXT)
		FROM conversation_summaries
		WHERE 1=1
	`
	var args []any

	if strings.TrimSpace(opts.Workspace) != "" {
		query += ` AND workspace_uris LIKE ?`
		args = append(args, "%"+strings.TrimSpace(opts.Workspace)+"%")
	}
	if strings.TrimSpace(opts.Status) != "" {
		query += ` AND status = ?`
		args = append(args, strings.TrimSpace(opts.Status))
	}

	query += ` ORDER BY last_modified_time DESC LIMIT ? OFFSET ?`
	args = append(args, opts.Limit, opts.Offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query conversation_summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]ConversationSummary, 0)
	for rows.Next() {
		var item ConversationSummary
		var wsRaw string
		if err := rows.Scan(
			&item.ConversationID,
			&item.Title,
			&item.Preview,
			&item.StepCount,
			&item.LastModifiedTime,
			&wsRaw,
			&item.Status,
			&item.Source,
			&item.ProjectID,
			&item.AgentName,
			&item.ParentConversationID,
			&item.LastUserInputTime,
		); err != nil {
			return nil, fmt.Errorf("scan conversation_summaries: %w", err)
		}
		item.WorkspaceURIs = parseWorkspaceURIs(wsRaw)
		summaries = append(summaries, item)
	}

	return summaries, rows.Err()
}

// Search performs a full-text search across conversations using FTS5 (with fallback to LIKE on summaries).
func (s *ConversationStore) Search(ctx context.Context, q string, limit int) ([]SearchHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []SearchHit{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	db, err := s.openDB()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SearchHit{}, nil
		}
		return nil, err
	}
	defer db.Close()

	// Try FTS5 search first
	ftsQuery := `
		SELECT
			conversation_id,
			title,
			workspace,
			project,
			substr(user_body, 1, 300),
			substr(agent_body, 1, 300),
			step_count,
			CAST(last_modified_time AS TEXT)
		FROM conversations_fts
		WHERE conversations_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`
	rows, err := db.QueryContext(ctx, ftsQuery, escapeFTS5Query(q), limit)
	if err == nil {
		defer rows.Close()
		hits := make([]SearchHit, 0)
		for rows.Next() {
			var hit SearchHit
			if scanErr := rows.Scan(
				&hit.ConversationID,
				&hit.Title,
				&hit.Workspace,
				&hit.Project,
				&hit.UserSnippet,
				&hit.AgentSnippet,
				&hit.StepCount,
				&hit.LastModifiedTime,
			); scanErr != nil {
				break
			}
			hits = append(hits, hit)
		}
		if len(hits) > 0 || rows.Err() == nil {
			return hits, nil
		}
	}

	// Fallback to LIKE on conversation_summaries if FTS5 returns an error
	likePattern := "%" + q + "%"
	fallbackQuery := `
		SELECT
			conversation_id,
			title,
			workspace_uris,
			project_id,
			preview,
			step_count,
			CAST(last_modified_time AS TEXT)
		FROM conversation_summaries
		WHERE title LIKE ? OR preview LIKE ? OR workspace_uris LIKE ?
		ORDER BY last_modified_time DESC
		LIMIT ?
	`
	fbRows, fbErr := db.QueryContext(ctx, fallbackQuery, likePattern, likePattern, likePattern, limit)
	if fbErr != nil {
		return nil, fmt.Errorf("search conversations: %w", fbErr)
	}
	defer fbRows.Close()

	hits := make([]SearchHit, 0)
	for fbRows.Next() {
		var hit SearchHit
		if err := fbRows.Scan(
			&hit.ConversationID,
			&hit.Title,
			&hit.Workspace,
			&hit.Project,
			&hit.UserSnippet,
			&hit.StepCount,
			&hit.LastModifiedTime,
		); err != nil {
			return nil, err
		}
		hits = append(hits, hit)
	}
	return hits, fbRows.Err()
}

// Get retrieves both the summary metadata and the full transcript steps for a conversation ID.
func (s *ConversationStore) Get(ctx context.Context, conversationID string) (*ConversationDetail, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || strings.ContainsAny(conversationID, `/\..`) {
		return nil, ErrNotFound
	}

	var summary *ConversationSummary
	if db, err := s.openDB(); err == nil {
		defer db.Close()
		row := db.QueryRowContext(ctx, `
			SELECT
				conversation_id,
				title,
				preview,
				step_count,
				CAST(last_modified_time AS TEXT),
				workspace_uris,
				status,
				source,
				project_id,
				agent_name,
				parent_conversation_id,
				CAST(last_user_input_time AS TEXT)
			FROM conversation_summaries
			WHERE conversation_id = ?
		`, conversationID)

		var item ConversationSummary
		var wsRaw string
		if scanErr := row.Scan(
			&item.ConversationID,
			&item.Title,
			&item.Preview,
			&item.StepCount,
			&item.LastModifiedTime,
			&wsRaw,
			&item.Status,
			&item.Source,
			&item.ProjectID,
			&item.AgentName,
			&item.ParentConversationID,
			&item.LastUserInputTime,
		); scanErr == nil {
			item.WorkspaceURIs = parseWorkspaceURIs(wsRaw)
			summary = &item
		}
	}

	steps, err := s.ReadTranscript(conversationID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if summary == nil && len(steps) == 0 {
		return nil, ErrNotFound
	}

	return &ConversationDetail{
		Summary: summary,
		Steps:   steps,
	}, nil
}

// ReadTranscript reads transcript.jsonl (and resolves truncated lines from transcript_full.jsonl if present).
func (s *ConversationStore) ReadTranscript(conversationID string) ([]TranscriptStep, error) {
	logsDir := filepath.Join(s.dataDir, "brain", conversationID, ".system_generated", "logs")
	compactPath := filepath.Join(logsDir, "transcript.jsonl")
	fullPath := filepath.Join(logsDir, "transcript_full.jsonl")

	f, err := os.Open(compactPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var steps []TranscriptStep
	needsFull := make(map[int]int) // lineNumber -> index in steps

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 20*1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var step TranscriptStep
		if err := json.Unmarshal(line, &step); err != nil {
			continue
		}
		if len(step.TruncatedFields) > 0 {
			needsFull[lineNum] = len(steps)
		}
		steps = append(steps, step)
	}
	if err := scanner.Err(); err != nil {
		return steps, err
	}

	if len(needsFull) > 0 {
		if fullFile, err := os.Open(fullPath); err == nil {
			defer fullFile.Close()
			fullScanner := bufio.NewScanner(fullFile)
			fullScanner.Buffer(make([]byte, 64*1024), 50*1024*1024)
			currLine := 0
			for fullScanner.Scan() {
				currLine++
				idx, wanted := needsFull[currLine]
				if !wanted {
					continue
				}
				var fullStep TranscriptStep
				if err := json.Unmarshal(bytes.TrimSpace(fullScanner.Bytes()), &fullStep); err == nil {
					steps[idx] = fullStep
				}
			}
		}
	}

	return steps, nil
}

func parseWorkspaceURIs(raw string) []string {
	var uris []string
	if err := json.Unmarshal([]byte(raw), &uris); err == nil {
		return uris
	}
	if strings.TrimSpace(raw) != "" {
		return []string{raw}
	}
	return []string{}
}

func escapeFTS5Query(q string) string {
	escaped := strings.ReplaceAll(q, `"`, `""`)
	return `"` + escaped + `"`
}

