package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
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
				"content": "What is your name?",
			},
		},
		"stream": true,
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
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	fmt.Printf("Response Status: %s\n", resp.Status)
	fmt.Println("--- Stream Output ---")

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Stream reading error: %v", err)
	}
	fmt.Println("\n--- Stream End ---")
}
