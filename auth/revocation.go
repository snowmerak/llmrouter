package auth

import (
	"log"
	"os"
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

type RevokedConfig struct {
	RevokedKeys []string `yaml:"revoked_keys"`
}

type RevocationManager struct {
	keys atomic.Value // holds map[string]struct{}
	mu   sync.Mutex
}

func NewRevocationManager() *RevocationManager {
	rm := &RevocationManager{}
	rm.keys.Store(make(map[string]struct{}))
	return rm
}

func (rm *RevocationManager) IsRevoked(key string) bool {
	keys := rm.keys.Load().(map[string]struct{})
	_, exists := keys[key]
	return exists
}

func (rm *RevocationManager) LoadFile(path string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// If file doesn't exist, just use empty list
			rm.keys.Store(make(map[string]struct{}))
			return nil
		}
		return err
	}

	var cfg RevokedConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}

	newKeys := make(map[string]struct{}, len(cfg.RevokedKeys))
	for _, k := range cfg.RevokedKeys {
		newKeys[k] = struct{}{}
	}

	rm.keys.Store(newKeys)
	return nil
}

func (rm *RevocationManager) WatchFile(path string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Failed to create revocation file watcher: %v", err)
		return
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Rename == fsnotify.Rename {
					log.Printf("Revocation file changed: %s. Reloading...", event.Name)
					if err := rm.LoadFile(path); err != nil {
						log.Printf("Failed to reload revocation list: %v", err)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Revocation watcher error: %v", err)
			}
		}
	}()

	// Watch the file if it exists, or its parent directory so we can detect when it's created
	if err := watcher.Add(path); err != nil {
		// If file doesn't exist yet, we could watch the directory, but for simplicity we'll just log
		log.Printf("Warning: Could not watch %s directly: %v. Please restart if you create it later.", path, err)
	}
}
