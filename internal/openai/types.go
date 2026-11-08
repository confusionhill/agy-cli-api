package openai

import (
	"encoding/json"
	"fmt"
)

// ChatCompletionRequest represents an incoming OpenAI /v1/chat/completions request.
type ChatCompletionRequest struct {
	Model               string            `json:"model"`
	Messages            []ChatMessage     `json:"messages"`
	Stream              bool              `json:"stream,omitempty"`
	Temperature         *float64          `json:"temperature,omitempty"`
	TopP                *float64          `json:"top_p,omitempty"`
	MaxTokens           *int              `json:"max_tokens,omitempty"`
	PresencePenalty     *float64          `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64          `json:"frequency_penalty,omitempty"`
	User                string            `json:"user,omitempty"`
	ConversationID      string            `json:"conversation_id,omitempty"`      // Extension for Antigravity continuity
	WorkspaceDir        string            `json:"workspace_dir,omitempty"`        // Extension for Antigravity folder
	Effort              string            `json:"effort,omitempty"`               // Extension for reasoning effort (low|medium|high)
	DangerouslySkipPerm *bool             `json:"dangerously_skip_permissions,omitempty"`
}

// ChatMessage represents a single message in the conversation history.
type ChatMessage struct {
	Role       string          `json:"role"` // system, user, assistant, tool
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// ContentPart represents a multimodal content piece (text or image_url).
type ContentPart struct {
	Type     string         `json:"type"` // "text" or "image_url"
	Text     string         `json:"text,omitempty"`
	ImageURL *ImageURLParam `json:"image_url,omitempty"`
}

// ImageURLParam holds the URL or base64 data URL for an image part.
type ImageURLParam struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// TextContent extracts the plain string content from a ChatMessage.
func (m ChatMessage) TextContent() (string, []ContentPart, error) {
	if len(m.Content) == 0 {
		return "", nil, nil
	}

	// Try parsing as plain string
	var str string
	if err := json.Unmarshal(m.Content, &str); err == nil {
		return str, nil, nil
	}

	// Try parsing as array of content parts
	var parts []ContentPart
	if err := json.Unmarshal(m.Content, &parts); err == nil {
		var textParts []string
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				textParts = append(textParts, p.Text)
			}
		}
		return fmt.Sprintf("%v", textParts), parts, nil
	}

	return string(m.Content), nil, nil
}

// ChatCompletionResponse represents the standard non-streaming response.
type ChatCompletionResponse struct {
	ID                string       `json:"id"`
	Object            string       `json:"object"` // "chat.completion"
	Created           int64        `json:"created"`
	Model             string       `json:"model"`
	SystemFingerprint string       `json:"system_fingerprint,omitempty"`
	Choices           []ChatChoice `json:"choices"`
	Usage             OpenAIUsage  `json:"usage"`
}

// ChatChoice represents an individual completion choice.
type ChatChoice struct {
	Index        int                    `json:"index"`
	Message      ChatResponseMessage    `json:"message"`
	FinishReason string                 `json:"finish_reason"` // "stop", "length", "tool_calls"
}

// ChatResponseMessage represents the message emitted by the assistant.
type ChatResponseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionChunk represents an individual SSE chunk when stream=true.
type ChatCompletionChunk struct {
	ID                string            `json:"id"`
	Object            string            `json:"object"` // "chat.completion.chunk"
	Created           int64             `json:"created"`
	Model             string            `json:"model"`
	SystemFingerprint string            `json:"system_fingerprint,omitempty"`
	Choices           []ChatChunkChoice `json:"choices"`
	Usage             *OpenAIUsage      `json:"usage,omitempty"`
}

// ChatChunkChoice represents an individual choice delta in an SSE chunk.
type ChatChunkChoice struct {
	Index        int            `json:"index"`
	Delta        ChatChunkDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

// ChatChunkDelta contains the partial text delta.
type ChatChunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// OpenAIUsage represents token counts in OpenAI format.
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ModelListResponse represents the response for GET /v1/models.
type ModelListResponse struct {
	Object string        `json:"object"` // "list"
	Data   []ModelObject `json:"data"`
}

// ModelObject represents an individual model item in GET /v1/models.
type ModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"` // "model"
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// OpenAIError represents standard OpenAI error responses.
type OpenAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

// OpenAIErrorResponse wraps OpenAIError.
type OpenAIErrorResponse struct {
	Error OpenAIError `json:"error"`
}
