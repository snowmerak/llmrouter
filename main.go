package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/snowmerak/llmrouter/config"
	"github.com/snowmerak/llmrouter/proxy"
)

func main() {
	// Try to load config.yaml from current directory
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load configuration from %s: %v", cfgPath, err)
	}

	if len(cfg.Destinations) == 0 {
		log.Fatalf("No destinations configured in %s", cfgPath)
	}

	ollamaProxy, reloadableTransport := proxy.NewOllamaProxy(cfg)

	// Start a background goroutine that listens to config.yaml changes.
	// When changed, parse the config and swap out the transport internally.
	config.WatchConfig(cfgPath, func(newCfg *config.Config) {
		reloadableTransport.Update(newCfg)
		log.Printf("Proxy destinations successfully updated without downtime!")
	})

	// Since the user specified "only Ollama traffic will hit this",
	// we just handle all requests with the proxy.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received request for: %s", r.URL.Path)
		ollamaProxy.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("Starting Multi-Ollama Proxy on %s", addr)
	for i, dest := range cfg.Destinations {
		log.Printf("  Destination %d: %s", i+1, dest.URL)
	}

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
