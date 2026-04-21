package vertexai

import (
	"encoding/json"
	"fmt"

	"github.com/snowmerak/llmrouter/schema"
)

type VertexEmbeddingInstance struct {
	Content string `json:"content"`
}

type VertexEmbeddingRequest struct {
	Instances []VertexEmbeddingInstance `json:"instances"`
}

type VertexEmbeddingValues struct {
	Values []float64 `json:"values"`
}

type VertexPrediction struct {
	Embeddings VertexEmbeddingValues `json:"embeddings"`
}

type VertexEmbeddingResponse struct {
	Predictions []VertexPrediction `json:"predictions"`
}

func ToVertexEmbeddingRequest(data []byte) ([]byte, error) {
	var req schema.EmbeddingRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	var instances []VertexEmbeddingInstance

	// Input can be string or []interface{} (if JSON array)
	switch v := req.Input.(type) {
	case string:
		instances = append(instances, VertexEmbeddingInstance{Content: v})
	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				instances = append(instances, VertexEmbeddingInstance{Content: str})
			} else {
				return nil, fmt.Errorf("unsupported input type in array: %T", item)
			}
		}
	default:
		return nil, fmt.Errorf("unsupported input type: %T", req.Input)
	}

	vReq := VertexEmbeddingRequest{
		Instances: instances,
	}

	return json.Marshal(vReq)
}

func FromVertexEmbeddingResponse(data []byte, model string) ([]byte, error) {
	var vResp VertexEmbeddingResponse
	if err := json.Unmarshal(data, &vResp); err != nil {
		return nil, err
	}

	resp := schema.EmbeddingResponse{
		Object: "list",
		Model:  model,
		Data:   make([]schema.EmbeddingData, len(vResp.Predictions)),
	}

	for i, pred := range vResp.Predictions {
		resp.Data[i] = schema.EmbeddingData{
			Object:    "embedding",
			Embedding: pred.Embeddings.Values,
			Index:     i,
		}
	}

	return json.Marshal(resp)
}
