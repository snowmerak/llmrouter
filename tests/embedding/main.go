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
	// Router's default address for embeddings
	routerURL := "http://localhost:11656/v1/embeddings"

	// Mock OpenAI request payload for embeddings
	payload := map[string]interface{}{
		"model": "embedding", // Will be translated to target_model by the router based on tag
		"input": "This is a test for the embedding pipeline.",
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

	fmt.Println("--- Embedding Response ---")
	
	// Pretty print JSON
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, bodyBytes, "", "  "); err == nil {
		fmt.Println(prettyJSON.String())
	} else {
		fmt.Println(string(bodyBytes))
	}
	fmt.Println("--- Response End ---")
}
