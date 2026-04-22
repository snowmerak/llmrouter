package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"

	"github.com/dgraph-io/ristretto"
)

var embCache *ristretto.Cache

func init() {
	var err error
	embCache, err = ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,     // track frequency of 10M keys
		MaxCost:     1 << 30, // 1GB max memory
		BufferItems: 64,      // 64 keys per Get buffer
	})
	if err != nil {
		log.Fatalf("Failed to initialize Ristretto cache: %v", err)
	}
}

func getEmbeddingCacheKey(model, text string) string {
	h := sha256.New()
	h.Write([]byte(model + ":" + text))
	return hex.EncodeToString(h.Sum(nil))
}

func getCachedEmbedding(key string) []float32 {
	if val, found := embCache.Get(key); found {
		if emb, ok := val.([]float32); ok {
			return emb
		}
	}
	return nil
}

func setCachedEmbedding(key string, emb []float32) {
	// Each float32 is 4 bytes
	embCache.Set(key, emb, int64(len(emb)*4))
}

type EmbeddingData struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  map[string]int  `json:"usage"`
}

type EmbeddingContext struct {
	OriginalInput []string
	CachedData    []EmbeddingData
	MissingIndices []int
	MissingInput  []string
}

// ProcessEmbeddingRequest returns the parsed context, a synthesized complete response (if all cached),
// and the new bodyBytes (if some are missing)
func ProcessEmbeddingRequest(bodyBytes []byte, requestedModel string) (*EmbeddingContext, []byte, *EmbeddingResponse) {
	var payload map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, nil, nil
	}

	rawInput, ok := payload["input"]
	if !ok {
		return nil, nil, nil
	}

	var inputs []string
	switch v := rawInput.(type) {
	case string:
		inputs = []string{v}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				inputs = append(inputs, s)
			}
		}
	}

	if len(inputs) == 0 {
		return nil, nil, nil
	}

	ctx := &EmbeddingContext{
		OriginalInput: inputs,
	}

	var allCached = true

	for i, text := range inputs {
		key := getEmbeddingCacheKey(requestedModel, text)
		if emb := getCachedEmbedding(key); emb != nil {
			ctx.CachedData = append(ctx.CachedData, EmbeddingData{
				Object:    "embedding",
				Embedding: emb,
				Index:     i,
			})
		} else {
			allCached = false
			ctx.MissingIndices = append(ctx.MissingIndices, i)
			ctx.MissingInput = append(ctx.MissingInput, text)
		}
	}

	if allCached {
		resp := &EmbeddingResponse{
			Object: "list",
			Data:   ctx.CachedData,
			Model:  requestedModel,
			Usage: map[string]int{
				"prompt_tokens": 0, // Cached
				"total_tokens":  0,
			},
		}
		return ctx, nil, resp
	}

	// Rebuild request body with only missing inputs
	payload["input"] = ctx.MissingInput
	newBodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, nil
	}

	return ctx, newBodyBytes, nil
}

func MergeEmbeddingResponse(ctx *EmbeddingContext, respBytes []byte, requestedModel string) ([]byte, error) {
	var upstreamResp EmbeddingResponse
	if err := json.Unmarshal(respBytes, &upstreamResp); err != nil {
		return nil, err
	}

	// Cache the new embeddings
	for i, data := range upstreamResp.Data {
		if i < len(ctx.MissingIndices) {
			origIdx := ctx.MissingIndices[i]
			text := ctx.OriginalInput[origIdx]
			key := getEmbeddingCacheKey(requestedModel, text)
			setCachedEmbedding(key, data.Embedding)
			
			// Fix index for final response
			data.Index = origIdx
			ctx.CachedData = append(ctx.CachedData, data)
		}
	}

	// Reconstruct the final response
	finalResp := upstreamResp
	finalResp.Data = ctx.CachedData

	// Sort by index to maintain original order
	for i := 0; i < len(finalResp.Data); i++ {
		for j := i + 1; j < len(finalResp.Data); j++ {
			if finalResp.Data[i].Index > finalResp.Data[j].Index {
				finalResp.Data[i], finalResp.Data[j] = finalResp.Data[j], finalResp.Data[i]
			}
		}
	}

	return json.Marshal(finalResp)
}
