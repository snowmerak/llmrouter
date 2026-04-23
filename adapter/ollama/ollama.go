package ollama

import (
	"encoding/json"
	"time"

	"github.com/snowmerak/llmrouter/schema"
)

// OllamaMessage is the exact message format expected by Ollama clients
type OllamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OllamaChatResponseChunk represents the NDJSON response expected by Ollama clients
type OllamaChatResponseChunk struct {
	Model     string         `json:"model"`
	CreatedAt string         `json:"created_at"`
	Message   *OllamaMessage `json:"message,omitempty"`
	Done      bool           `json:"done"`
}

// FormatStreamChunk formats a Universal ChatStreamChunk into an Ollama NDJSON string.
func FormatStreamChunk(chunk *schema.ChatStreamChunk, isEOF bool) ([]byte, error) {
	if isEOF {
		emptyMsg := OllamaMessage{
			Role:    "assistant",
			Content: "",
		}
		doneChunk := OllamaChatResponseChunk{
			Model:     "llmrouter",
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message:   &emptyMsg,
			Done:      true,
		}
		data, err := json.Marshal(doneChunk)
		if err != nil {
			return nil, err
		}
		res := append(data, '\n')
		return res, nil
	}

	if chunk == nil || len(chunk.Choices) == 0 {
		return nil, nil
	}

	delta := chunk.Choices[0].Delta

	// Skip completely empty chunks (like end-of-stream markers before DONE)
	if (delta.Content == nil || *delta.Content == "") && len(delta.ToolCalls) == 0 && delta.Role == nil {
		return nil, nil
	}

	role := "assistant"
	if delta.Role != nil {
		role = string(*delta.Role)
	}
	content := ""
	if delta.Content != nil {
		content = *delta.Content
	}

	ollamaMsg := OllamaMessage{
		Role:    role,
		Content: content, // explicitly include content even if empty string
	}

	ollamaChunk := OllamaChatResponseChunk{
		Model:     chunk.Model,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Message:   &ollamaMsg,
		Done:      false,
	}

	data, err := json.Marshal(ollamaChunk)
	if err != nil {
		return nil, err
	}

	res := append(data, '\n')
	return res, nil
}

// ToUniversalRequest parses an Ollama JSON request into a Universal ChatRequest.
func ToUniversalRequest(body []byte) (*schema.ChatRequest, error) {
	var req schema.ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// FromUniversalResponse formats a Universal ChatResponse into an Ollama JSON string.
func FromUniversalResponse(resp *schema.ChatResponse) ([]byte, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return []byte("{}"), nil
	}

	msg := resp.Choices[0].Message

	role := "assistant"
	if msg.Role != nil {
		role = string(*msg.Role)
	}
	content := ""
	if msg.Content != nil {
		content = *msg.Content
	}

	ollamaMsg := OllamaMessage{
		Role:    role,
		Content: content,
	}

	ollamaResp := OllamaChatResponseChunk{
		Model:     resp.Model,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Message:   &ollamaMsg,
		Done:      true,
	}

	return json.Marshal(ollamaResp)
}
