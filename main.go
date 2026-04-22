package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/snowmerak/llmrouter/config"
	"github.com/snowmerak/llmrouter/proxy"
)

const defaultConfig = `server:
  port: 11656

destinations:
  - url: "http://localhost:11434"
    protocol: "openai"
    weight: 1
    target_model: "llama3"
    tags: ["llama3"]
    context_length: 32768
    capabilities: ["generate", "chat", "tools", "embedding"]

health_check:
  enabled: true
  interval_secs: 10
  timeout_secs: 3
  ping_path: "/"

circuit_breaker:
  max_requests: 3
  interval_secs: 600
  timeout_secs: 300
`

func main() {
	var initFlag bool
	var initFlagShort bool

	flag.BoolVar(&initFlag, "init", false, "Generate default config.yaml")
	flag.BoolVar(&initFlagShort, "i", false, "Generate default config.yaml")
	flag.Parse()

	if initFlag || initFlagShort {
		if err := os.WriteFile("config.yaml", []byte(defaultConfig), 0644); err != nil {
			log.Fatalf("Failed to write default config.yaml: %v", err)
		}
		fmt.Println("Generated default config.yaml")
		return
	}

	// Try to load config.yaml from current directory
	cfgPath := "config.yaml"
	if flag.NArg() > 0 {
		cfgPath = flag.Arg(0)
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
