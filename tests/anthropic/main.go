package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

func main() {
	// Router's default address for Anthropic
	routerURL := "http://localhost:11656/v1/messages"

	// Mock Anthropic request payload
	// Requesting "super" which routes to openai backend (qwen3.6)
	// This tests Cross-Protocol Routing: Anthropic Client -> Router -> OpenAI Backend
	payload := map[string]interface{}{
		"model": "super",
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": "What is the weather in Tokyo? Please use the get_weather tool.",
			},
		},
		"tools": []map[string]interface{}{
			{
				"name":        "get_weather",
				"description": "Get the current weather in a given location",
				"input_schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "The city and state, e.g. San Francisco, CA",
						},
					},
					"required": []string{"location"},
				},
			},
		},
		"stream": false,
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("Failed to marshal payload: %v", err)
	}

	log.Printf("Sending Anthropic format request to %s...", routerURL)
	req, err := http.NewRequest("POST", routerURL, bytes.NewBuffer(reqBody))
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "test-key")
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	fmt.Printf("Response Status: %s\n", resp.Status)
	fmt.Println("--- Response Output ---")

	var prettyJSON bytes.Buffer
	bodyBytes, _ := io.ReadAll(resp.Body)
	if err := json.Indent(&prettyJSON, bodyBytes, "", "  "); err == nil {
		fmt.Println(prettyJSON.String())
	} else {
		fmt.Println(string(bodyBytes))
	}

	fmt.Println("\n--- Response End ---")
}
