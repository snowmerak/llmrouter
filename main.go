package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/snowmerak/llmrouter/auth"
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

metrics:
  enabled: true
  port: 9090

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
	var genKeyClient string

	flag.BoolVar(&initFlag, "init", false, "Generate default config.yaml")
	flag.BoolVar(&initFlagShort, "i", false, "Generate default config.yaml")
	flag.StringVar(&genKeyClient, "gen-key", "", "Generate a new API key for the specified client_id")
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

	if genKeyClient != "" {
		if !cfg.Auth.Enabled || cfg.Auth.MasterKey == "" {
			log.Fatalf("Cannot generate key: auth is not enabled or master_key is empty in config.yaml")
		}
		key, err := auth.GenerateKey(genKeyClient, cfg.Auth.MasterKey)
		if err != nil {
			log.Fatalf("Failed to generate API key: %v", err)
		}
		fmt.Printf("Generated API Key for '%s':\n%s\n", genKeyClient, key)
		return
	}

	ollamaProxy, reloadableTransport := proxy.NewOllamaProxy(cfg)

	// Start a background goroutine that listens to config.yaml changes.
	// When changed, parse the config and swap out the transport internally.
	config.WatchConfig(cfgPath, func(newCfg *config.Config) {
		reloadableTransport.Update(newCfg)
		log.Printf("Proxy destinations successfully updated without downtime!")
	})

	if cfg.Metrics.Enabled {
		metricsPort := cfg.Metrics.Port
		if metricsPort == 0 {
			metricsPort = 9090 // Default
		}
		go func() {
			metricsAddr := fmt.Sprintf(":%d", metricsPort)
			log.Printf("Starting Prometheus Metrics Server on %s", metricsAddr)
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.Handler())
			if err := http.ListenAndServe(metricsAddr, mux); err != nil {
				log.Fatalf("Metrics server failed: %v", err)
			}
		}()
	}

	// Since the user specified "only Ollama traffic will hit this",
	// we just handle all requests with the proxy.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Auth.Enabled {
			authHeader := r.Header.Get("Authorization")
			apiKey := ""
			if strings.HasPrefix(authHeader, "Bearer ") {
				apiKey = strings.TrimPrefix(authHeader, "Bearer ")
			} else {
				apiKey = r.Header.Get("x-api-key")
			}

			clientID := "anonymous"
			if apiKey != "" {
				id, err := auth.ValidateKey(apiKey, cfg.Auth.MasterKey)
				if err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					w.Write([]byte(`{"error": {"message": "Invalid API Key", "type": "invalid_request_error"}}`))
					return
				}
				clientID = id
			}

			ctx := context.WithValue(r.Context(), auth.ClientIDKey{}, clientID)
			r = r.WithContext(ctx)
		}

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
