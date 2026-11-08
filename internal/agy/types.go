package agy

import "encoding/json"

// ChatRequest represents the parameters for running a turn on the agy CLI.
type ChatRequest struct {
	Prompt               string   `json:"prompt"`
	ConversationID       string   `json:"conversation_id,omitempty"`
	WorkspaceDir         string   `json:"workspace_dir,omitempty"`
	AddDirs              []string `json:"add_dirs,omitempty"`
	Model                string   `json:"model,omitempty"`
	Effort               string   `json:"effort,omitempty"`
	Agent                string   `json:"agent,omitempty"`
	PermissionMode       string   `json:"permission_mode,omitempty"`
	DisableSlashCommands bool     `json:"disable_slash_commands,omitempty"`
	JSONSchema           string   `json:"json_schema,omitempty"`
}

// TokenUsage represents token metrics returned by agy.
type TokenUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	ThinkingTokens  int `json:"thinking_tokens"`
	CacheReadTokens int `json:"cache_read_tokens"`
	TotalTokens     int `json:"total_tokens"`
}

// ResultPayload is the final summary payload emitted in stream-json or json mode.
type ResultPayload struct {
	ConversationID  string          `json:"conversation_id"`
	Status          string          `json:"status"`
	Response        string          `json:"response"`
	DurationSeconds float64         `json:"duration_seconds"`
	NumTurns        int             `json:"num_turns"`
	Usage           TokenUsage      `json:"usage"`
	Command         *CommandPayload `json:"command,omitempty"`
}

// CommandPayload holds structured output from slash commands like /usage or /help.
type CommandPayload struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

// StreamEvent represents a single NDJSON event emitted by `agy -p --output-format stream-json`.
type StreamEvent struct {
	Event          string          `json:"event"`
	ConversationID string          `json:"conversation_id,omitempty"`
	Init           json.RawMessage `json:"init,omitempty"`
	StepUpdate     json.RawMessage `json:"step_update,omitempty"`
	Result         *ResultPayload  `json:"result,omitempty"`
	Error          string          `json:"error,omitempty"`
	Raw            json.RawMessage `json:"-"`
}

// UsageData represents the structured model quota response from `agy -p "/usage" --output-format json`.
type UsageData struct {
	Description string       `json:"description"`
	Groups      []UsageGroup `json:"groups"`
}

// UsageGroup represents a quota group (e.g., "Gemini Models", "Claude and GPT models", "Gemini 4 Argon").
type UsageGroup struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Buckets     []UsageBucket `json:"buckets"`
}

// UsageBucket represents a specific quota window within a model group.
type UsageBucket struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Description       string  `json:"description,omitempty"`
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remaining_fraction"`
	RemainingPercent  float64 `json:"remaining_percent"`
	ResetTime         string  `json:"reset_time"`
}

// ModelInfo represents an available model returned by `agy models`.
type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AgentInfo represents an available agent returned by `agy agents`.
type AgentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// MCPServerInfo represents a configured MCP server returned by `agy mcp list`.
type MCPServerInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	CommandURL string `json:"command_or_url"`
}
