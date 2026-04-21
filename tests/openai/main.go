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
	// Router's default address
	routerURL := "http://localhost:11656/v1/chat/completions"

	// Mock OpenAI request payload
	payload := map[string]interface{}{
		"model": "light", // Will be translated to target_model by the router (anthropic backend)
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": "What is the weather in Tokyo? Please use the get_weather tool.",
			},
		},
		"tools": []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "get_weather",
					"description": "Get the current weather in a given location",
					"parameters": map[string]interface{}{
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
		},
		"stream": false,
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("Failed to marshal payload: %v", err)
	}

	log.Printf("Sending request to %s...", routerURL)
	req, err := http.NewRequest("POST", routerURL, bytes.NewBuffer(reqBody))
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Request failed: %v\nMake sure the router is running on port 11656.", err)
	}
	defer resp.Body.Close()

	log.Printf("Response Status: %s", resp.Status)

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to process request correctly. Status: %d", resp.StatusCode)
	}

	// Read output
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response body: %v", err)
	}

	fmt.Println("--- Response Output ---")

	// Pretty print JSON
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, bodyBytes, "", "  "); err == nil {
		fmt.Println(prettyJSON.String())
	} else {
		fmt.Println(string(bodyBytes))
	}
	fmt.Println("--- Response End ---")
}
