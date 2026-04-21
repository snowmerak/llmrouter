package schema

// Role represents the role of the message sender.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a single chat message.
type Message struct {
	Role       *Role               `json:"role,omitempty"`
	Content    *string             `json:"content,omitempty"`
	ToolCalls  []UniversalToolCall `json:"tool_calls,omitempty"`
	ToolCallID *string             `json:"tool_call_id,omitempty"`
	Name       *string             `json:"name,omitempty"`
}

// ChatRequest is the universal format for a chat completion request.
type ChatRequest struct {
	Model       string            `json:"model"`
	Messages    []Message         `json:"messages"`
	Temperature *float64          `json:"temperature,omitempty"`
	TopP        *float64          `json:"top_p,omitempty"`
	MaxTokens   *int              `json:"max_tokens,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
	Tools       []UniversalTool   `json:"tools,omitempty"`
	ToolChoice  interface{}       `json:"tool_choice,omitempty"`
}

// ChatResponse is the universal format for a full chat completion response.
type ChatResponse struct {
	ID      string   `json:"id,omitempty"`
	Model   string   `json:"model,omitempty"`
	Choices []Choice `json:"choices,omitempty"`
}

// Choice represents a single choice in a ChatResponse.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

// ChatStreamChunk is the universal format for a streaming chunk.
type ChatStreamChunk struct {
	ID      string         `json:"id,omitempty"`
	Model   string         `json:"model,omitempty"`
	Choices []StreamChoice `json:"choices,omitempty"`
}

// StreamChoice represents a single choice in a ChatStreamChunk.
type StreamChoice struct {
	Index        int     `json:"index"`
	Delta        Message `json:"delta,omitempty"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

type UniversalTool struct {
	Type     string            `json:"type"` // "function"
	Function UniversalFunction `json:"function"`
}

type UniversalFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type UniversalToolCall struct {
	Index    int                       `json:"index,omitempty"`
	ID       string                    `json:"id,omitempty"`
	Type     string                    `json:"type"` // "function"
	Function UniversalToolCallFunction `json:"function"`
}

type UniversalToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"` // Stringified JSON
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
