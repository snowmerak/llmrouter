package schema

// Role represents the role of the message sender.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single chat message.
type Message struct {
	Role    *Role   `json:"role,omitempty"`
	Content *string `json:"content,omitempty"`
}

// ChatRequest is the universal format for a chat completion request.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// ChatResponse is the universal format for a full chat completion response.
type ChatResponse struct {
	ID      string   `json:"id,omitempty"`
	Model   string   `json:"model,omitempty"`
	Choices []Choice `json:"choices,omitempty"`
}

// Choice represents a single choice in a ChatResponse.
type Choice struct {
	Index   int     `json:"index"`
	Message Message `json:"message"`
}

// ChatStreamChunk is the universal format for a streaming chunk.
type ChatStreamChunk struct {
	ID      string         `json:"id,omitempty"`
	Model   string         `json:"model,omitempty"`
	Choices []StreamChoice `json:"choices,omitempty"`
}

// StreamChoice represents a single choice in a ChatStreamChunk.
type StreamChoice struct {
	Index int     `json:"index"`
	Delta Message `json:"delta,omitempty"`
}

// EmbeddingRequest is the universal format for an embedding request (OpenAI style).
type EmbeddingRequest struct {
	Model string      `json:"model"`
	Input interface{} `json:"input"` // string or []string
}

// EmbeddingResponse is the universal format for an embedding response.
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  map[string]int  `json:"usage,omitempty"`
}

type EmbeddingData struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}
