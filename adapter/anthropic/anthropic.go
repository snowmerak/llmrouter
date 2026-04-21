package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/snowmerak/llmrouter/schema"
)

type AnthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AnthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []AnthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"` // Required by Anthropic
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}

type AnthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type AnthropicResponse struct {
	ID      string             `json:"id"`
	Type    string             `json:"type"`
	Role    string             `json:"role"`
	Model   string             `json:"model"`
	Content []AnthropicContent `json:"content"`
}

type StreamEvent struct {
	Type  string `json:"type"`
	// For message_start
	Message *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"message,omitempty"`
	// For content_block_delta
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta,omitempty"`
}

func FromUniversalRequest(req *schema.ChatRequest) ([]byte, error) {
	anthropicReq := AnthropicRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	if req.MaxTokens != nil {
		anthropicReq.MaxTokens = *req.MaxTokens
	} else {
		anthropicReq.MaxTokens = 4096 // Default max tokens for Anthropic
	}

	for _, msg := range req.Messages {
		if msg.Role == nil || msg.Content == nil {
			continue
		}
		if *msg.Role == schema.RoleSystem {
			// Concatenate system prompts if there are multiple
			if anthropicReq.System != "" {
				anthropicReq.System += "\n\n"
			}
			anthropicReq.System += *msg.Content
		} else {
			anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
				Role:    string(*msg.Role),
				Content: *msg.Content,
			})
		}
	}

	return json.Marshal(anthropicReq)
}

func ToUniversalResponse(data []byte) (*schema.ChatResponse, error) {
	var resp AnthropicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	content := ""
	for _, block := range resp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	role := schema.RoleAssistant
	return &schema.ChatResponse{
		ID:    resp.ID,
		Model: resp.Model,
		Choices: []schema.Choice{
			{
				Index: 0,
				Message: schema.Message{
					Role:    &role,
					Content: &content,
				},
			},
		},
	}, nil
}

// ParseStreamChunk reads an Anthropic stream chunk and maps it to a Universal (OpenAI-like) chunk.
// Because Anthropic streams send ID and Model only in "message_start", the caller must keep state.
func ParseStreamChunk(line []byte, currentID, currentModel string) (*schema.ChatStreamChunk, string, string, error) {
	data := bytes.TrimPrefix(line, []byte("data: "))
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return nil, currentID, currentModel, nil
	}

	var event StreamEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, currentID, currentModel, err
	}

	switch event.Type {
	case "message_start":
		if event.Message != nil {
			currentID = event.Message.ID
			currentModel = event.Message.Model
		}
		role := schema.RoleAssistant
		emptyContent := ""
		return &schema.ChatStreamChunk{
			ID:    currentID,
			Model: currentModel,
			Choices: []schema.StreamChoice{
				{
					Index: 0,
					Delta: schema.Message{
						Role:    &role,
						Content: &emptyContent,
					},
				},
			},
		}, currentID, currentModel, nil

	case "content_block_delta":
		if event.Delta != nil && event.Delta.Type == "text_delta" {
			txt := event.Delta.Text
			return &schema.ChatStreamChunk{
				ID:    currentID,
				Model: currentModel,
				Choices: []schema.StreamChoice{
					{
						Index: 0,
						Delta: schema.Message{
							Content: &txt,
						},
					},
				},
			}, currentID, currentModel, nil
		}
	}

	// Ignored events (ping, message_stop, content_block_stop, etc.)
	return nil, currentID, currentModel, fmt.Errorf("ignored event type: %s", event.Type)
}
