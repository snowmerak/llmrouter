package config

import (
	"log"
	"os"
	"regexp"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

type Destination struct {
	URL           string   `yaml:"url"`
	Protocol      string   `yaml:"protocol"`
	ApiKey        string   `yaml:"api_key"`
	Weight        int      `yaml:"weight"`
	Tags          []string `yaml:"tags"`
	TargetModel   string   `yaml:"target_model"`
	ContextLength int      `yaml:"context_length"`
	Capabilities  []string `yaml:"capabilities"`
}

type HealthCheck struct {
	Enabled      bool   `yaml:"enabled"`
	IntervalSecs uint32 `yaml:"interval_secs"`
	TimeoutSecs  uint32 `yaml:"timeout_secs"`
	PingPath     string `yaml:"ping_path"`
}

type CircuitBreaker struct {
	MaxRequests  uint32 `yaml:"max_requests"`
	IntervalSecs uint32 `yaml:"interval_secs"`
	TimeoutSecs  uint32 `yaml:"timeout_secs"`
}

type Server struct {
	Port int `yaml:"port"`
}

type Metrics struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

type Config struct {
	Server         Server            `yaml:"server"`
	Metrics        Metrics           `yaml:"metrics"`
	HealthCheck    HealthCheck       `yaml:"health_check"`
	Destinations   []Destination     `yaml:"destinations"`
	CircuitBreaker CircuitBreaker    `yaml:"circuit_breaker"`
	ModelRouting   map[string]string `yaml:"model_routing"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile(`\{\{env:([a-zA-Z0-9_]+)\}\}`)
	data = re.ReplaceAllFunc(data, func(b []byte) []byte {
		match := re.FindSubmatch(b)
		if len(match) > 1 {
			return []byte(os.Getenv(string(match[1])))
		}
		return b
	})

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func WatchConfig(path string, onChange func(*Config)) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to create config watcher: %v", err)
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// Watch for Write and Create (some editors use copy+rename replacing original file)
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Rename == fsnotify.Rename {
					log.Printf("Config file changed: %s. Reloading...", event.Name)
					cfg, err := LoadConfig(path)
					if err != nil {
						log.Printf("Failed to reload config: %v", err)
						continue // Ignore bad configs and stick to the current one
					}
					onChange(cfg)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Config watcher error: %v", err)
			}
		}
	}()

	err = watcher.Add(path)
	if err != nil {
		log.Fatalf("Failed to watch config at %s: %v", path, err)
	}
}
