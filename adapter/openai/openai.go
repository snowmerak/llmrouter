package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/snowmerak/llmrouter/schema"
)

// ToUniversalRequest parses an OpenAI JSON request into a Universal ChatRequest.
func ToUniversalRequest(body []byte) (*schema.ChatRequest, error) {
	var req schema.ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// FromUniversalRequest serializes a Universal ChatRequest into an OpenAI JSON request.
// It uses the universal struct directly because the JSON tags match the OpenAI spec.
func FromUniversalRequest(req *schema.ChatRequest) ([]byte, error) {
	return json.Marshal(req)
}

// ParseStreamChunk reads an OpenAI SSE stream chunk (e.g. `data: {...}`) and maps it.
func ParseStreamChunk(line []byte) (*schema.ChatStreamChunk, error) {
	// The line usually starts with "data: "
	data := bytes.TrimPrefix(line, []byte("data: "))
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return nil, nil // Not a JSON chunk
	}

	var chunk schema.ChatStreamChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil, err
	}
	return &chunk, nil
}

// FormatStreamChunk formats a Universal ChatStreamChunk into an OpenAI SSE string.
func FormatStreamChunk(chunk *schema.ChatStreamChunk) ([]byte, error) {
	data, err := json.Marshal(chunk)
	if err != nil {
		return nil, err
	}
	res := append([]byte("data: "), data...)
	res = append(res, '\n', '\n')
	return res, nil
}

// ExecuteRequest sends the payload to the destination URL.
func ExecuteRequest(req *http.Request, payload []byte, destURL string) (*http.Response, error) {
	req.Body = io.NopCloser(bytes.NewReader(payload))
	req.ContentLength = int64(len(payload))
	req.Header.Set("Content-Length", string(rune(len(payload))))
	
	// This will be handled in the proxy's RoundTripper
	return nil, nil
}
