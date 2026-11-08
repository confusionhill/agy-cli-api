package openai

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dika/agy-cli-api/internal/agy"
	"github.com/google/uuid"
)

// ConvertRequestToAgy translates an OpenAI ChatCompletionRequest into an Antigravity ChatRequest.
func ConvertRequestToAgy(req ChatCompletionRequest) (agy.ChatRequest, error) {
	var systemInstructions []string
	var historyBuilder strings.Builder
	var lastUserPrompt string
	var attachments []agy.Attachment

	for i, msg := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		text, parts, err := msg.TextContent()
		if err != nil {
			return agy.ChatRequest{}, fmt.Errorf("message[%d] content parse: %w", i, err)
		}

		// Extract any multimodal image parts
		for _, part := range parts {
			if part.Type == "image_url" && part.ImageURL != nil && part.ImageURL.URL != "" {
				att := parseImageURLToAttachment(part.ImageURL.URL)
				attachments = append(attachments, att)
			}
		}

		switch role {
		case "system":
			if text != "" {
				systemInstructions = append(systemInstructions, text)
			}
		case "user":
			if i == len(req.Messages)-1 {
				lastUserPrompt = text
			} else {
				historyBuilder.WriteString(fmt.Sprintf("User: %s\n\n", text))
			}
		case "assistant":
			historyBuilder.WriteString(fmt.Sprintf("Assistant: %s\n\n", text))
		}
	}

	var finalPromptBuilder strings.Builder

	// Prepend system instructions if present
	if len(systemInstructions) > 0 {
		finalPromptBuilder.WriteString(fmt.Sprintf("[System Instructions: %s]\n\n", strings.Join(systemInstructions, " ")))
	}

	// Prepend conversation history if present
	historyStr := historyBuilder.String()
	if historyStr != "" {
		finalPromptBuilder.WriteString(fmt.Sprintf("[Previous Conversation Context:\n%s]\n\n", strings.TrimSpace(historyStr)))
	}

	// Append main user prompt
	if lastUserPrompt != "" {
		finalPromptBuilder.WriteString(lastUserPrompt)
	} else if finalPromptBuilder.Len() == 0 && len(req.Messages) > 0 {
		// Fallback: take content from first message if no user role explicitly tagged last
		firstText, _, _ := req.Messages[0].TextContent()
		finalPromptBuilder.WriteString(firstText)
	}

	model := req.Model
	effort := req.Effort
	if effort == "" {
		lowerModel := strings.ToLower(model)
		if strings.HasSuffix(lowerModel, "-low") ||
			strings.HasSuffix(lowerModel, "-medium") ||
			strings.HasSuffix(lowerModel, "-high") ||
			strings.HasSuffix(lowerModel, "-xhigh") ||
			strings.HasSuffix(lowerModel, "-max") {
			effort = ""
		} else if model == "" {
			model = "gemini-4-argon-medium"
		}
	}

	permMode := "skip-permissions"
	if req.DangerouslySkipPerm != nil && !*req.DangerouslySkipPerm {
		permMode = "auto"
	}

	return agy.ChatRequest{
		Prompt:         strings.TrimSpace(finalPromptBuilder.String()),
		ConversationID: req.ConversationID,
		WorkspaceDir:   req.WorkspaceDir,
		Model:          model,
		Effort:         effort,
		PermissionMode: permMode,
		Attachments:    attachments,
	}, nil
}

// FormatCompletionResponse builds an OpenAI ChatCompletionResponse from an Antigravity ResultPayload.
func FormatCompletionResponse(result *agy.ResultPayload, model string) ChatCompletionResponse {
	id := "chatcmpl-" + uuid.New().String()
	if result.ConversationID != "" {
		id = "chatcmpl-" + result.ConversationID
	}

	respModel := model
	if respModel == "" {
		respModel = "gemini-4-argon-medium"
	}

	return ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   respModel,
		Choices: []ChatChoice{
			{
				Index: 0,
				Message: ChatResponseMessage{
					Role:    "assistant",
					Content: result.Response,
				},
				FinishReason: "stop",
			},
		},
		Usage: OpenAIUsage{
			PromptTokens:     result.Usage.InputTokens,
			CompletionTokens: result.Usage.OutputTokens,
			TotalTokens:      result.Usage.TotalTokens,
		},
	}
}

// FormatCompletionChunk creates an SSE chunk JSON string.
func FormatCompletionChunk(id, model, textDelta string, finishReason *string) (string, error) {
	chunk := ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatChunkChoice{
			{
				Index: 0,
				Delta: ChatChunkDelta{
					Content: textDelta,
				},
				FinishReason: finishReason,
			},
		},
	}

	bytes, err := json.Marshal(chunk)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data: %s\n\n", string(bytes)), nil
}

// FormatInitialChunk creates the very first SSE chunk containing role="assistant".
func FormatInitialChunk(id, model string) (string, error) {
	chunk := ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatChunkChoice{
			{
				Index: 0,
				Delta: ChatChunkDelta{
					Role:    "assistant",
					Content: "",
				},
				FinishReason: nil,
			},
		},
	}

	bytes, err := json.Marshal(chunk)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data: %s\n\n", string(bytes)), nil
}

// FormatModelList formats agy models into standard OpenAI ModelListResponse.
func FormatModelList(models []agy.ModelInfo) ModelListResponse {
	now := time.Now().Unix()
	var items []ModelObject
	for _, m := range models {
		items = append(items, ModelObject{
			ID:      m.ID,
			Object:  "model",
			Created: now,
			OwnedBy: "antigravity",
		})
	}

	return ModelListResponse{
		Object: "list",
		Data:   items,
	}
}

func parseImageURLToAttachment(rawURL string) agy.Attachment {
	if strings.HasPrefix(rawURL, "data:") {
		// e.g. data:image/png;base64,iVBOR...
		mimeType := "image/jpeg"
		if commaIdx := strings.Index(rawURL, ","); commaIdx != -1 {
			meta := rawURL[:commaIdx]
			if strings.HasPrefix(meta, "data:") && strings.Contains(meta, ";") {
				mimeType = meta[5:strings.Index(meta, ";")]
			}
		}
		return agy.Attachment{
			Filename:   "attachment_" + uuid.New().String()[:8] + ".jpg",
			MimeType:   mimeType,
			DataBase64: rawURL,
		}
	}

	// Plain local path or file URL
	cleanPath := strings.TrimPrefix(rawURL, "file://")
	return agy.Attachment{
		Filename: filepath.Base(cleanPath),
		Path:     cleanPath,
	}
}
