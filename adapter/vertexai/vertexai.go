package vertexai

import (
	"bytes"
	"encoding/json"

	"github.com/snowmerak/llmrouter/schema"
)

type Part struct {
	Text string `json:"text,omitempty"`
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

type VertexRequest struct {
	Contents          []Content         `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
}

type Candidate struct {
	Content      *Content `json:"content,omitempty"`
	FinishReason string   `json:"finishReason,omitempty"`
}

type VertexResponse struct {
	Candidates []Candidate `json:"candidates,omitempty"`
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

	for _, msg := range req.Messages {
		if msg.Role == nil || msg.Content == nil {
			continue
		}
		
		roleStr := string(*msg.Role)
		if roleStr == "system" {
			vReq.SystemInstruction = &Content{
				Role: "system",
				Parts: []Part{{Text: *msg.Content}},
			}
		} else {
			// Vertex uses "model" instead of "assistant"
			if roleStr == "assistant" {
				roleStr = "model"
			}
			vReq.Contents = append(vReq.Contents, Content{
				Role:  roleStr,
				Parts: []Part{{Text: *msg.Content}},
			})
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
		role := schema.Role(roleStr)
		
		txt := ""
		if len(content.Parts) > 0 {
			txt = content.Parts[0].Text
		}
		
		req.Messages = append(req.Messages, schema.Message{
			Role:    &role,
			Content: &txt,
		})
	}
	
	return req, nil
}

func ToUniversalResponse(data []byte) (*schema.ChatResponse, error) {
	var vResp VertexResponse
	if err := json.Unmarshal(data, &vResp); err != nil {
		return nil, err
	}
	
	txt := ""
	if len(vResp.Candidates) > 0 && vResp.Candidates[0].Content != nil && len(vResp.Candidates[0].Content.Parts) > 0 {
		txt = vResp.Candidates[0].Content.Parts[0].Text
	}
	
	role := schema.RoleAssistant
	
	return &schema.ChatResponse{
		ID: "vertex-res",
		Choices: []schema.Choice{
			{
				Index: 0,
				Message: schema.Message{
					Role:    &role,
					Content: &txt,
				},
			},
		},
	}, nil
}

func FromUniversalResponse(resp *schema.ChatResponse) ([]byte, error) {
	txt := ""
	if len(resp.Choices) > 0 && resp.Choices[0].Message.Content != nil {
		txt = *resp.Choices[0].Message.Content
	}
	
	vResp := VertexResponse{
		Candidates: []Candidate{
			{
				Content: &Content{
					Role: "model",
					Parts: []Part{{Text: txt}},
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
	
	txt := ""
	if len(vResp.Candidates) > 0 && vResp.Candidates[0].Content != nil && len(vResp.Candidates[0].Content.Parts) > 0 {
		txt = vResp.Candidates[0].Content.Parts[0].Text
	}
	
	return &schema.ChatStreamChunk{
		ID: "vertex-stream",
		Choices: []schema.StreamChoice{
			{
				Index: 0,
				Delta: schema.Message{
					Content: &txt,
				},
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
	
	txt := ""
	if chunk.Choices[0].Delta.Content != nil {
		txt = *chunk.Choices[0].Delta.Content
	}
	
	vResp := VertexResponse{
		Candidates: []Candidate{
			{
				Content: &Content{
					Role: "model",
					Parts: []Part{{Text: txt}},
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
