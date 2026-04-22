package vertexai

import (
	"bytes"
	"encoding/json"

	"github.com/snowmerak/llmrouter/schema"
)

type VertexFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type VertexFunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

type Part struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *VertexFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *VertexFunctionResponse `json:"functionResponse,omitempty"`
}

type Content struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts,omitempty"`
}

type GenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
}

type VertexTool struct {
	FunctionDeclarations []schema.UniversalFunction `json:"functionDeclarations"`
}

type VertexRequest struct {
	Contents          []Content         `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
	Tools             []VertexTool      `json:"tools,omitempty"`
}

type Candidate struct {
	Content      *Content `json:"content,omitempty"`
	FinishReason string   `json:"finishReason,omitempty"`
}

type VertexResponse struct {
	Candidates []Candidate `json:"candidates,omitempty"`
}

// sanitizeVertexParameters recursively removes unsupported JSON schema keys from tool parameters.
// Vertex AI STRICTLY rejects keys like "$comment" and "enumDescriptions".
func sanitizeVertexParameters(params map[string]interface{}) {
	if params == nil {
		return
	}
	delete(params, "$comment")
	delete(params, "enumDescriptions")

	for _, v := range params {
		if subMap, ok := v.(map[string]interface{}); ok {
			sanitizeVertexParameters(subMap)
		} else if subSlice, ok := v.([]interface{}); ok {
			for _, item := range subSlice {
				if itemMap, ok := item.(map[string]interface{}); ok {
					sanitizeVertexParameters(itemMap)
				}
			}
		}
	}
}

func FromUniversalRequest(req *schema.ChatRequest) ([]byte, error) {
	vReq := VertexRequest{}
	
	if req.Temperature != nil || req.TopP != nil || req.MaxTokens != nil {
		vReq.GenerationConfig = &GenerationConfig{
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			MaxOutputTokens: req.MaxTokens,
		}
	}

	if len(req.Tools) > 0 {
		var decls []schema.UniversalFunction
		for _, tool := range req.Tools {
			if tool.Type == "function" {
				fn := tool.Function
				if fn.Parameters != nil {
					sanitizeVertexParameters(fn.Parameters)
				}
				decls = append(decls, fn)
			}
		}
		if len(decls) > 0 {
			vReq.Tools = []VertexTool{{FunctionDeclarations: decls}}
		}
	}

	for _, msg := range req.Messages {
		if msg.Role == nil {
			continue
		}
		
		roleStr := string(*msg.Role)
		if roleStr == string(schema.RoleSystem) {
			if msg.Content != nil {
				vReq.SystemInstruction = &Content{
					Role: "system",
					Parts: []Part{{Text: *msg.Content}},
				}
			}
		} else if roleStr == string(schema.RoleTool) {
			if msg.ToolCallID != nil && msg.Content != nil {
				var respMap map[string]interface{}
				if err := json.Unmarshal([]byte(*msg.Content), &respMap); err != nil {
					respMap = map[string]interface{}{"result": *msg.Content}
				}
				vReq.Contents = append(vReq.Contents, Content{
					Role: "function",
					Parts: []Part{
						{
							FunctionResponse: &VertexFunctionResponse{
								Name:     *msg.ToolCallID,
								Response: respMap,
							},
						},
					},
				})
			}
		} else {
			if roleStr == string(schema.RoleAssistant) {
				roleStr = "model"
			}
			
			var parts []Part
			if msg.Content != nil && *msg.Content != "" {
				parts = append(parts, Part{Text: *msg.Content})
			}
			
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					var args map[string]interface{}
					json.Unmarshal([]byte(tc.Function.Arguments), &args)
					if args == nil {
						args = make(map[string]interface{})
					}
					parts = append(parts, Part{
						FunctionCall: &VertexFunctionCall{
							Name: tc.Function.Name,
							Args: args,
						},
					})
				}
			}
			
			if len(parts) > 0 {
				vReq.Contents = append(vReq.Contents, Content{
					Role:  roleStr,
					Parts: parts,
				})
			}
		}
	}
	
	return json.Marshal(vReq)
}

func ToUniversalRequest(data []byte) (*schema.ChatRequest, error) {
	var vReq VertexRequest
	if err := json.Unmarshal(data, &vReq); err != nil {
		return nil, err
	}
	
	req := &schema.ChatRequest{}
	if vReq.GenerationConfig != nil {
		req.Temperature = vReq.GenerationConfig.Temperature
		req.TopP = vReq.GenerationConfig.TopP
		req.MaxTokens = vReq.GenerationConfig.MaxOutputTokens
	}

	if len(vReq.Tools) > 0 && len(vReq.Tools[0].FunctionDeclarations) > 0 {
		for _, decl := range vReq.Tools[0].FunctionDeclarations {
			req.Tools = append(req.Tools, schema.UniversalTool{
				Type:     "function",
				Function: decl,
			})
		}
	}
	
	if vReq.SystemInstruction != nil && len(vReq.SystemInstruction.Parts) > 0 {
		sysRole := schema.RoleSystem
		sysContent := vReq.SystemInstruction.Parts[0].Text
		req.Messages = append(req.Messages, schema.Message{
			Role:    &sysRole,
			Content: &sysContent,
		})
	}
	
	for _, content := range vReq.Contents {
		roleStr := content.Role
		if roleStr == "model" {
			roleStr = "assistant"
		}
		
		var txtStr string
		var toolCalls []schema.UniversalToolCall
		var toolCallID *string
		isToolResult := false
		
		for _, part := range content.Parts {
			if part.Text != "" {
				txtStr += part.Text
			}
			if part.FunctionCall != nil {
				argsBytes, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, schema.UniversalToolCall{
					ID: part.FunctionCall.Name, // Using name as ID
					Type: "function",
					Function: schema.UniversalToolCallFunction{
						Name: part.FunctionCall.Name,
						Arguments: string(argsBytes),
					},
				})
			}
			if part.FunctionResponse != nil {
				isToolResult = true
				idCopy := part.FunctionResponse.Name
				toolCallID = &idCopy
				resBytes, _ := json.Marshal(part.FunctionResponse.Response)
				txtStr += string(resBytes)
			}
		}
		
		if isToolResult {
			roleStr = string(schema.RoleTool)
		}
		
		role := schema.Role(roleStr)
		uMsg := schema.Message{
			Role: &role,
		}
		if txtStr != "" {
			uMsg.Content = &txtStr
		}
		if len(toolCalls) > 0 {
			uMsg.ToolCalls = toolCalls
		}
		if toolCallID != nil {
			uMsg.ToolCallID = toolCallID
		}
		req.Messages = append(req.Messages, uMsg)
	}
	
	return req, nil
}

func ToUniversalResponse(data []byte) (*schema.ChatResponse, error) {
	var vResp VertexResponse
	if err := json.Unmarshal(data, &vResp); err != nil {
		return nil, err
	}
	
	role := schema.RoleAssistant
	msg := schema.Message{
		Role: &role,
	}

	if len(vResp.Candidates) > 0 && vResp.Candidates[0].Content != nil {
		var txtStr string
		var toolCalls []schema.UniversalToolCall
		
		for _, part := range vResp.Candidates[0].Content.Parts {
			if part.Text != "" {
				txtStr += part.Text
			}
			if part.FunctionCall != nil {
				argsBytes, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, schema.UniversalToolCall{
					ID: part.FunctionCall.Name,
					Type: "function",
					Function: schema.UniversalToolCallFunction{
						Name: part.FunctionCall.Name,
						Arguments: string(argsBytes),
					},
				})
			}
		}
		
		if txtStr != "" {
			msg.Content = &txtStr
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}
	}
	
	return &schema.ChatResponse{
		ID: "vertex-res",
		Choices: []schema.Choice{
			{
				Index: 0,
				Message: msg,
			},
		},
	}, nil
}

func FromUniversalResponse(resp *schema.ChatResponse) ([]byte, error) {
	var parts []Part
	
	if len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message
		if msg.Content != nil && *msg.Content != "" {
			parts = append(parts, Part{Text: *msg.Content})
		}
		
		for _, tc := range msg.ToolCalls {
			var args map[string]interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
			if args == nil {
				args = make(map[string]interface{})
			}
			parts = append(parts, Part{
				FunctionCall: &VertexFunctionCall{
					Name: tc.Function.Name,
					Args: args,
				},
			})
		}
	}
	
	vResp := VertexResponse{
		Candidates: []Candidate{
			{
				Content: &Content{
					Role: "model",
					Parts: parts,
				},
			},
		},
	}
	
	return json.Marshal(vResp)
}

func ParseStreamChunk(line []byte) (*schema.ChatStreamChunk, error) {
	data := bytes.TrimPrefix(line, []byte("data: "))
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	
	var vResp VertexResponse
	if err := json.Unmarshal(data, &vResp); err != nil {
		return nil, err
	}
	
	msg := schema.Message{}
	var finishReason *string
	
	if len(vResp.Candidates) > 0 {
		if vResp.Candidates[0].FinishReason != "" {
			fr := vResp.Candidates[0].FinishReason
			if fr == "STOP" {
				fr = "stop"
			}
			finishReason = &fr
		}
		
		if vResp.Candidates[0].Content != nil {
			var txtStr string
			var toolCalls []schema.UniversalToolCall
			
			for _, part := range vResp.Candidates[0].Content.Parts {
				if part.Text != "" {
					txtStr += part.Text
				}
				if part.FunctionCall != nil {
					argsBytes, _ := json.Marshal(part.FunctionCall.Args)
					toolCalls = append(toolCalls, schema.UniversalToolCall{
						ID: part.FunctionCall.Name,
						Type: "function",
						Function: schema.UniversalToolCallFunction{
							Name: part.FunctionCall.Name,
							Arguments: string(argsBytes),
						},
					})
				}
			}
			
			if txtStr != "" {
				msg.Content = &txtStr
			}
			if len(toolCalls) > 0 {
				msg.ToolCalls = toolCalls
			}
		}
	}
	
	// If the chunk has NO content, NO tool calls, NO role, and NO finish reason, it's just a metadata/usage chunk.
	// We MUST drop it, otherwise Copilot throws "Response contained no choices" when seeing empty delta.
	if msg.Content == nil && len(msg.ToolCalls) == 0 && msg.Role == nil && finishReason == nil {
		return nil, nil
	}
	
	return &schema.ChatStreamChunk{
		ID: "vertex-stream",
		Choices: []schema.StreamChoice{
			{
				Index: 0,
				Delta: msg,
				FinishReason: finishReason,
			},
		},
	}, nil
}

func FormatStreamChunk(chunk *schema.ChatStreamChunk, isEOF bool) ([]byte, error) {
	if isEOF {
		return nil, nil // Vertex streaming SSE format just ends the connection
	}
	
	if chunk == nil || len(chunk.Choices) == 0 {
		return nil, nil
	}
	
	msg := chunk.Choices[0].Delta
	var parts []Part
	
	if msg.Content != nil && *msg.Content != "" {
		parts = append(parts, Part{Text: *msg.Content})
	}
	
	for _, tc := range msg.ToolCalls {
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		if args == nil {
			args = make(map[string]interface{})
		}
		parts = append(parts, Part{
			FunctionCall: &VertexFunctionCall{
				Name: tc.Function.Name,
				Args: args,
			},
		})
	}
	
	vResp := VertexResponse{
		Candidates: []Candidate{
			{
				Content: &Content{
					Role: "model",
					Parts: parts,
				},
			},
		},
	}
	
	data, err := json.Marshal(vResp)
	if err != nil {
		return nil, err
	}
	
	res := append([]byte("data: "), data...)
	res = append(res, '\n', '\n')
	return res, nil
}
