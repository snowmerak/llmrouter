package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/snowmerak/llmrouter/schema"
)

type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []AnthropicContentBlock
}

type AnthropicContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`          // For tool_use
	Name      string                 `json:"name,omitempty"`        // For tool_use
	Input     map[string]interface{} `json:"input,omitempty"`       // For tool_use
	ToolUseID string                 `json:"tool_use_id,omitempty"` // For tool_result
	Content   interface{}            `json:"content,omitempty"`     // For tool_result
}

type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type AnthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []AnthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"` // Required by Anthropic
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Tools       []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice  interface{}        `json:"tool_choice,omitempty"`
}

type AnthropicContent struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
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
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJson string `json:"partial_json,omitempty"`
	} `json:"delta,omitempty"`
	// For content_block_start
	ContentBlock *AnthropicContent `json:"content_block,omitempty"`
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

	if len(req.Tools) > 0 {
		for _, tool := range req.Tools {
			if tool.Type == "function" {
				anthropicReq.Tools = append(anthropicReq.Tools, AnthropicTool{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					InputSchema: tool.Function.Parameters,
				})
			}
		}
	}

	if req.ToolChoice != nil {
		switch tc := req.ToolChoice.(type) {
		case string:
			if tc == "auto" {
				anthropicReq.ToolChoice = map[string]string{"type": "auto"}
			} else if tc == "required" {
				anthropicReq.ToolChoice = map[string]string{"type": "any"}
			}
		case map[string]interface{}:
			if f, ok := tc["function"].(map[string]interface{}); ok {
				if name, ok := f["name"].(string); ok {
					anthropicReq.ToolChoice = map[string]string{"type": "tool", "name": name}
				}
			}
		}
	}

	for _, msg := range req.Messages {
		if msg.Role == nil {
			continue
		}
		if *msg.Role == schema.RoleSystem {
			if msg.Content != nil {
				if anthropicReq.System != "" {
					anthropicReq.System += "\n\n"
				}
				anthropicReq.System += *msg.Content
			}
		} else if *msg.Role == schema.RoleTool {
			if msg.ToolCallID != nil && msg.Content != nil {
				block := AnthropicContentBlock{
					Type:      "tool_result",
					ToolUseID: *msg.ToolCallID,
					Content:   *msg.Content,
				}
				
				// Try to merge with previous user message if it exists and is an array
				merged := false
				if len(anthropicReq.Messages) > 0 {
					lastIdx := len(anthropicReq.Messages) - 1
					lastMsg := anthropicReq.Messages[lastIdx]
					if lastMsg.Role == "user" {
						if blocks, ok := lastMsg.Content.([]AnthropicContentBlock); ok {
							anthropicReq.Messages[lastIdx].Content = append(blocks, block)
							merged = true
						}
					}
				}
				
				if !merged {
					anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
						Role:    "user",
						Content: []AnthropicContentBlock{block},
					})
				}
			}
		} else if *msg.Role == schema.RoleAssistant && len(msg.ToolCalls) > 0 {
			var blocks []AnthropicContentBlock
			if msg.Content != nil && *msg.Content != "" {
				blocks = append(blocks, AnthropicContentBlock{
					Type: "text",
					Text: *msg.Content,
				})
			}
			for _, tc := range msg.ToolCalls {
				var input map[string]interface{}
				json.Unmarshal([]byte(tc.Function.Arguments), &input)
				if input == nil {
					input = make(map[string]interface{})
				}
				blocks = append(blocks, AnthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
				Role:    "assistant",
				Content: blocks,
			})
		} else {
			if msg.Content != nil {
				anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
					Role:    string(*msg.Role),
					Content: *msg.Content,
				})
			}
		}
	}

	return json.Marshal(anthropicReq)
}

func ToUniversalRequest(data []byte) (*schema.ChatRequest, error) {
	var req AnthropicRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	universalReq := &schema.ChatRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	if req.System != "" {
		role := schema.RoleSystem
		content := req.System
		universalReq.Messages = append(universalReq.Messages, schema.Message{
			Role:    &role,
			Content: &content,
		})
	}

	if len(req.Tools) > 0 {
		for _, tool := range req.Tools {
			universalReq.Tools = append(universalReq.Tools, schema.UniversalTool{
				Type: "function",
				Function: schema.UniversalFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
		}
	}

	if req.ToolChoice != nil {
		switch tc := req.ToolChoice.(type) {
		case map[string]interface{}:
			if t, ok := tc["type"].(string); ok {
				if t == "auto" {
					universalReq.ToolChoice = "auto"
				} else if t == "any" {
					universalReq.ToolChoice = "required"
				} else if t == "tool" {
					if name, ok := tc["name"].(string); ok {
						universalReq.ToolChoice = map[string]interface{}{
							"type": "function",
							"function": map[string]interface{}{
								"name": name,
							},
						}
					}
				}
			}
		}
	}

	for _, msg := range req.Messages {
		role := schema.Role(msg.Role)
		var contentStr string
		var toolCalls []schema.UniversalToolCall

		switch c := msg.Content.(type) {
		case string:
			contentStr = c
			universalReq.Messages = append(universalReq.Messages, schema.Message{
				Role:    &role,
				Content: &contentStr,
			})
		case []interface{}:
			hasToolUseOrText := false
			for _, b := range c {
				if bm, ok := b.(map[string]interface{}); ok {
					t, _ := bm["type"].(string)
					if t == "text" {
						if text, ok := bm["text"].(string); ok {
							contentStr += text
						}
						hasToolUseOrText = true
					} else if t == "tool_use" {
						if id, ok := bm["id"].(string); ok {
							if name, ok := bm["name"].(string); ok {
								args := "{}"
								if input, ok := bm["input"].(map[string]interface{}); ok {
									if bArgs, err := json.Marshal(input); err == nil {
										args = string(bArgs)
									}
								}
								toolCalls = append(toolCalls, schema.UniversalToolCall{
									ID:   id,
									Type: "function",
									Function: schema.UniversalToolCallFunction{
										Name:      name,
										Arguments: args,
									},
								})
							}
						}
						hasToolUseOrText = true
					} else if t == "tool_result" {
						if id, ok := bm["tool_use_id"].(string); ok {
							idCopy := id
							var resStr string
							if resContent, ok := bm["content"].(string); ok {
								resStr = resContent
							} else {
								bRes, _ := json.Marshal(bm["content"])
								resStr = string(bRes)
							}
							toolRole := schema.RoleTool
							universalReq.Messages = append(universalReq.Messages, schema.Message{
								Role:       &toolRole,
								Content:    &resStr,
								ToolCallID: &idCopy,
							})
						}
					}
				}
			}

			if hasToolUseOrText {
				uMsg := schema.Message{
					Role: &role,
				}
				if contentStr != "" {
					uMsg.Content = &contentStr
				}
				if len(toolCalls) > 0 {
					uMsg.ToolCalls = toolCalls
				}
				universalReq.Messages = append(universalReq.Messages, uMsg)
			}
		}
	}

	return universalReq, nil
}

func ToUniversalResponse(data []byte) (*schema.ChatResponse, error) {
	var resp AnthropicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	content := ""
	var toolCalls []schema.UniversalToolCall
	for _, block := range resp.Content {
		if block.Type == "text" {
			content += block.Text
		} else if block.Type == "tool_use" {
			args, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, schema.UniversalToolCall{
				ID:   block.ID,
				Type: "function",
				Function: schema.UniversalToolCallFunction{
					Name:      block.Name,
					Arguments: string(args),
				},
			})
		}
	}

	role := schema.RoleAssistant
	msg := schema.Message{
		Role: &role,
	}
	if content != "" {
		msg.Content = &content
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	return &schema.ChatResponse{
		ID:    resp.ID,
		Model: resp.Model,
		Choices: []schema.Choice{
			{
				Index:   0,
				Message: msg,
			},
		},
	}, nil
}

func FromUniversalResponse(resp *schema.ChatResponse) ([]byte, error) {
	var contents []AnthropicContent
	
	if len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message
		if msg.Content != nil && *msg.Content != "" {
			contents = append(contents, AnthropicContent{
				Type: "text",
				Text: *msg.Content,
			})
		}
		
		for _, tc := range msg.ToolCalls {
			var input map[string]interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
			if input == nil {
				input = make(map[string]interface{})
			}
			contents = append(contents, AnthropicContent{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
	}
	
	anthropicResp := AnthropicResponse{
		ID:    resp.ID,
		Type:  "message",
		Role:  "assistant",
		Model: resp.Model,
		Content: contents,
	}
	return json.Marshal(anthropicResp)
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

	case "content_block_start":
		if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
			return &schema.ChatStreamChunk{
				ID:    currentID,
				Model: currentModel,
				Choices: []schema.StreamChoice{
					{
						Index: 0,
						Delta: schema.Message{
							ToolCalls: []schema.UniversalToolCall{
								{
									ID:   event.ContentBlock.ID,
									Type: "function",
									Function: schema.UniversalToolCallFunction{
										Name: event.ContentBlock.Name,
									},
								},
							},
						},
					},
				},
			}, currentID, currentModel, nil
		}

	case "content_block_delta":
		if event.Delta != nil {
			if event.Delta.Type == "text_delta" {
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
			} else if event.Delta.Type == "input_json_delta" {
				return &schema.ChatStreamChunk{
					ID:    currentID,
					Model: currentModel,
					Choices: []schema.StreamChoice{
						{
							Index: 0,
							Delta: schema.Message{
								ToolCalls: []schema.UniversalToolCall{
									{
										Index: 0,
										Function: schema.UniversalToolCallFunction{
											Arguments: event.Delta.PartialJson,
										},
									},
								},
							},
						},
					},
				}, currentID, currentModel, nil
			}
		}
	}

	// Ignored events (ping, message_stop, content_block_stop, etc.)
	return nil, currentID, currentModel, fmt.Errorf("ignored event type: %s", event.Type)
}

func FormatStreamChunk(chunk *schema.ChatStreamChunk, isEOF bool) ([]byte, error) {
	if isEOF {
		return []byte("event: message_stop\ndata: {\"type\": \"message_stop\"}\n\n"), nil
	}
	
	if chunk == nil {
		return nil, nil
	}

	if len(chunk.Choices) == 0 {
		return nil, nil
	}

	delta := chunk.Choices[0].Delta
	
	if delta.Role != nil {
		startEvent := map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id": chunk.ID,
				"type": "message",
				"role": "assistant",
				"model": chunk.Model,
				"content": []interface{}{},
			},
		}
		data1, _ := json.Marshal(startEvent)
		res := append([]byte("event: message_start\ndata: "), data1...)
		res = append(res, '\n', '\n')

		blockStartEvent := map[string]interface{}{
			"type": "content_block_start",
			"index": 0,
			"content_block": map[string]interface{}{
				"type": "text",
				"text": "",
			},
		}
		data2, _ := json.Marshal(blockStartEvent)
		res = append(res, []byte("event: content_block_start\ndata: ")...)
		res = append(res, append(data2, '\n', '\n')...)
		
		return res, nil
	}

	if delta.Content != nil {
		contentEvent := map[string]interface{}{
			"type": "content_block_delta",
			"index": 0,
			"delta": map[string]interface{}{
				"type": "text_delta",
				"text": *delta.Content,
			},
		}
		data, _ := json.Marshal(contentEvent)
		res := append([]byte("event: content_block_delta\ndata: "), data...)
		res = append(res, '\n', '\n')
		return res, nil
	}
	
	return nil, nil
}
