package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dika/agy-cli-api/internal/agy"
)

func TestConvertRequestToAgy_Simple(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "gemini-4-argon-medium",
		Messages: []ChatMessage{
			{
				Role:    "user",
				Content: json.RawMessage(`"Reply with PONG"`),
			},
		},
		Effort: "high",
	}

	got, err := ConvertRequestToAgy(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Prompt != "Reply with PONG" {
		t.Errorf("expected Prompt 'Reply with PONG', got %q", got.Prompt)
	}
	if got.Model != "gemini-4-argon-medium" {
		t.Errorf("expected Model 'gemini-4-argon-medium', got %q", got.Model)
	}
	if got.Effort != "high" {
		t.Errorf("expected Effort 'high', got %q", got.Effort)
	}
}

func TestConvertRequestToAgy_MultiTurnWithSystem(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "gemini-2.5-flash",
		Messages: []ChatMessage{
			{
				Role:    "system",
				Content: json.RawMessage(`"You are an assistant"`),
			},
			{
				Role:    "user",
				Content: json.RawMessage(`"Hello"`),
			},
			{
				Role:    "assistant",
				Content: json.RawMessage(`"Hi there"`),
			},
			{
				Role:    "user",
				Content: json.RawMessage(`"What is 2+2?"`),
			},
		},
	}

	got, err := ConvertRequestToAgy(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(got.Prompt, "[System Instructions: You are an assistant]") {
		t.Errorf("expected system instruction in prompt, got:\n%s", got.Prompt)
	}
	if !strings.Contains(got.Prompt, "User: Hello") || !strings.Contains(got.Prompt, "Assistant: Hi there") {
		t.Errorf("expected history context in prompt, got:\n%s", got.Prompt)
	}
	if !strings.HasSuffix(got.Prompt, "What is 2+2?") {
		t.Errorf("expected final prompt to end with 'What is 2+2?', got:\n%s", got.Prompt)
	}
}

func TestConvertRequestToAgy_MultimodalImage(t *testing.T) {
	contentJSON := `[
		{"type": "text", "text": "Describe this image:"},
		{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgo="}}
	]`
	req := ChatCompletionRequest{
		Messages: []ChatMessage{
			{
				Role:    "user",
				Content: json.RawMessage(contentJSON),
			},
		},
	}

	got, err := ConvertRequestToAgy(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(got.Attachments))
	}
	if got.Attachments[0].MimeType != "image/png" {
		t.Errorf("expected image/png, got %s", got.Attachments[0].MimeType)
	}
	if got.Attachments[0].DataBase64 != "data:image/png;base64,iVBORw0KGgo=" {
		t.Errorf("unexpected DataBase64: %s", got.Attachments[0].DataBase64)
	}
}

func TestFormatCompletionResponse(t *testing.T) {
	res := &agy.ResultPayload{
		ConversationID: "conv-456",
		Status:         "SUCCESS",
		Response:       "Hello from agent",
		Usage: agy.TokenUsage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	}

	formatted := FormatCompletionResponse(res, "gemini-4-argon-medium")
	if formatted.Choices[0].Message.Content != "Hello from agent" {
		t.Errorf("unexpected content: %s", formatted.Choices[0].Message.Content)
	}
	if formatted.Usage.TotalTokens != 30 {
		t.Errorf("expected 30 total tokens, got %d", formatted.Usage.TotalTokens)
	}
}

func TestFormatCompletionChunk(t *testing.T) {
	chunkStr, err := FormatCompletionChunk("id-123", "gemini-4-argon-medium", "hello", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(chunkStr, "data: ") || !strings.HasSuffix(chunkStr, "\n\n") {
		t.Errorf("expected data: ...\\n\\n format, got %q", chunkStr)
	}

	var chunk ChatCompletionChunk
	jsonStr := strings.TrimSuffix(strings.TrimPrefix(chunkStr, "data: "), "\n\n")
	if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
		t.Fatalf("unmarshal chunk error: %v", err)
	}

	if chunk.Choices[0].Delta.Content != "hello" {
		t.Errorf("expected delta content hello, got %s", chunk.Choices[0].Delta.Content)
	}
}
