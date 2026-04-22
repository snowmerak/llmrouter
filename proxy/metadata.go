package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func (t *MultiTransport) getAllAvailableModels() []string {
	modelSet := make(map[string]struct{})

	// Add from ModelRouting explicitly
	if t.cfg != nil && t.cfg.ModelRouting != nil {
		for k := range t.cfg.ModelRouting {
			modelSet[k] = struct{}{}
		}
	}

	// Add from all tags in destinations
	for _, dest := range t.destinations {
		for _, tag := range dest.tags {
			modelSet[tag] = struct{}{}
		}
	}

	var models []string
	for k := range modelSet {
		models = append(models, k)
	}
	return models
}

func (t *MultiTransport) getModelMetadata(modelName string) (int, []string) {
	ctxLength := 32768
	caps := []string{"generate", "chat", "tools", "embedding"}

	for _, dest := range t.destinations {
		for _, tag := range dest.tags {
			if tag == modelName {
				if dest.contextLength > 0 {
					ctxLength = dest.contextLength
				}
				if len(dest.capabilities) > 0 {
					caps = dest.capabilities
				}
				log.Printf("[Metadata] Matched tag '%s' -> ContextLength: %d, Caps: %v", modelName, ctxLength, caps)
				return ctxLength, caps
			}
		}
	}
	log.Printf("[Metadata] Unmatched tag '%s' -> Fallback ContextLength: %d", modelName, ctxLength)
	return ctxLength, caps
}

func (t *MultiTransport) handleOpenAIModels(req *http.Request) (*http.Response, error) {
	models := t.getAllAvailableModels()

	type OpenAIModel struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	type OpenAIModelsResponse struct {
		Object string        `json:"object"`
		Data   []OpenAIModel `json:"data"`
	}

	resp := OpenAIModelsResponse{
		Object: "list",
		Data:   make([]OpenAIModel, 0, len(models)),
	}

	for _, m := range models {
		resp.Data = append(resp.Data, OpenAIModel{
			ID:      m,
			Object:  "model",
			Created: 1713830400, // Dummy timestamp
			OwnedBy: "llmrouter",
		})
	}

	b, _ := json.Marshal(resp)
	return makeJSONResponse(b), nil
}

func (t *MultiTransport) handleOllamaTags(req *http.Request) (*http.Response, error) {
	models := t.getAllAvailableModels()

	type OllamaModelDetails struct {
		ParentModel       string   `json:"parent_model"`
		Format            string   `json:"format"`
		Family            string   `json:"family"`
		Families          []string `json:"families"`
		ParameterSize     string   `json:"parameter_size"`
		QuantizationLevel string   `json:"quantization_level"`
	}

	type OllamaModel struct {
		Name       string             `json:"name"`
		Model      string             `json:"model"`
		ModifiedAt string             `json:"modified_at"`
		Size       int64              `json:"size"`
		Digest     string             `json:"digest"`
		Details    OllamaModelDetails `json:"details"`
	}

	type OllamaTagsResponse struct {
		Models []OllamaModel `json:"models"`
	}

	resp := OllamaTagsResponse{
		Models: make([]OllamaModel, 0, len(models)),
	}

	for _, m := range models {
		resp.Models = append(resp.Models, OllamaModel{
			Name:       m,
			Model:      m,
			ModifiedAt: time.Now().Format(time.RFC3339),
			Size:       8000000000,
			Digest:     "llmrouter-virtual-model",
			Details: OllamaModelDetails{
				ParentModel:       "",
				Format:            "gguf",
				Family:            "llama",
				Families:          []string{"llama"},
				ParameterSize:     "8B",
				QuantizationLevel: "Q4_0",
			},
		})
	}

	b, _ := json.Marshal(resp)
	return makeJSONResponse(b), nil
}

func (t *MultiTransport) handleOllamaVersion(req *http.Request) (*http.Response, error) {
	resp := map[string]string{"version": "0.21.0"}
	b, _ := json.Marshal(resp)
	return makeJSONResponse(b), nil
}

func (t *MultiTransport) handleOllamaPs(req *http.Request) (*http.Response, error) {
	resp := map[string]interface{}{"models": []interface{}{}}
	b, _ := json.Marshal(resp)
	return makeJSONResponse(b), nil
}

func (t *MultiTransport) handleOllamaShow(req *http.Request, bodyBytes []byte) (*http.Response, error) {
	type OllamaModelDetails struct {
		ParentModel       string   `json:"parent_model"`
		Format            string   `json:"format"`
		Family            string   `json:"family"`
		Families          []string `json:"families"`
		ParameterSize     string   `json:"parameter_size"`
		QuantizationLevel string   `json:"quantization_level"`
	}

	type OllamaShowResponse struct {
		License      string                 `json:"license"`
		Modelfile    string                 `json:"modelfile"`
		Parameters   string                 `json:"parameters"`
		Template     string                 `json:"template"`
		System       string                 `json:"system"`
		Details      OllamaModelDetails     `json:"details"`
		ModelInfo    map[string]interface{} `json:"model_info"`
		Capabilities []string               `json:"capabilities"`
		ModifiedAt   string                 `json:"modified_at"`
	}

	type showReq struct {
		Name string `json:"name"`
	}
	var reqBody showReq
	json.Unmarshal(bodyBytes, &reqBody)

	queryName := reqBody.Name
	if strings.HasSuffix(queryName, ":latest") {
		queryName = strings.TrimSuffix(queryName, ":latest")
	}

	ctxLength, caps := t.getModelMetadata(queryName)

	resp := OllamaShowResponse{
		License:    "MIT",
		Modelfile:  "FROM llama\nTEMPLATE \"{{ .Prompt }}\"",
		Parameters: "",
		Template:   "{{ .Prompt }}",
		System:     "You are a helpful AI assistant.",
		Details: OllamaModelDetails{
			ParentModel:       "",
			Format:            "gguf",
			Family:            "llama",
			Families:          []string{"llama"},
			ParameterSize:     "8B",
			QuantizationLevel: "Q4_0",
		},
		ModelInfo: map[string]interface{}{
			"general.architecture":    "llama",
			"general.name":            queryName,
			"general.parameter_count": 8000000000,
			"general.context_length":  ctxLength,
			"llama.context_length":    ctxLength,
		},
		Capabilities: caps,
		ModifiedAt:   time.Now().Format(time.RFC3339),
	}

	b, _ := json.Marshal(resp)
	return makeJSONResponse(b), nil
}

func (t *MultiTransport) handleRootPing(req *http.Request) (*http.Response, error) {
	b := []byte("Ollama is running")
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Body:          io.NopCloser(bytes.NewReader(b)),
		Header:        http.Header{"Content-Type": []string{"text/plain"}},
		ContentLength: int64(len(b)),
	}, nil
}

func (t *MultiTransport) handleOpenAISingleModel(req *http.Request) (*http.Response, error) {
	modelName := req.URL.Path[len("/v1/models/"):]
	
	models := t.getAllAvailableModels()
	modelExists := false
	for _, m := range models {
		if m == modelName {
			modelExists = true
			break
		}
	}

	if !modelExists {
		// Return 404
		b := []byte(`{"error": {"message": "The model '` + modelName + `' does not exist", "type": "invalid_request_error", "param": "model", "code": "model_not_found"}}`)
		return &http.Response{
			StatusCode:    http.StatusNotFound,
			Status:        "404 Not Found",
			Body:          io.NopCloser(bytes.NewReader(b)),
			Header:        http.Header{"Content-Type": []string{"application/json"}},
			ContentLength: int64(len(b)),
		}, nil
	}

	type OpenAIModel struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	resp := OpenAIModel{
		ID:      modelName,
		Object:  "model",
		Created: 1713830400, // Dummy timestamp
		OwnedBy: "llmrouter",
	}

	b, _ := json.Marshal(resp)
	return makeJSONResponse(b), nil
}

func makeJSONResponse(b []byte) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Body:          io.NopCloser(bytes.NewReader(b)),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		ContentLength: int64(len(b)),
	}
}
