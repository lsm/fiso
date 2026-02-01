package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// FlowDefinition represents a complete inbound pipeline configuration.
type FlowDefinition struct {
	Name   string       `yaml:"name"`
	Source SourceConfig `yaml:"source"`
	Transform *TransformConfig `yaml:"transform,omitempty"`
	Sink   SinkConfig   `yaml:"sink"`
	ErrorHandling ErrorHandlingConfig `yaml:"errorHandling"`
}

// SourceConfig holds source configuration.
type SourceConfig struct {
	Type   string            `yaml:"type"`
	Config map[string]interface{} `yaml:"config"`
}

// TransformConfig holds transform configuration.
type TransformConfig struct {
	CEL string `yaml:"cel"`
}

// SinkConfig holds sink configuration.
type SinkConfig struct {
	Type   string            `yaml:"type"`
	Config map[string]interface{} `yaml:"config"`
}

// ErrorHandlingConfig holds error handling configuration.
type ErrorHandlingConfig struct {
	DeadLetterTopic string `yaml:"deadLetterTopic"`
	MaxRetries      int    `yaml:"maxRetries"`
	Backoff         string `yaml:"backoff"`
}

// Loader loads and watches flow definition files.
type Loader struct {
	mu       sync.RWMutex
	flows    map[string]*FlowDefinition
	dir      string
	logger   *slog.Logger
	onChange func(map[string]*FlowDefinition)
}

// NewLoader creates a new configuration loader for the given directory.
func NewLoader(dir string, logger *slog.Logger) *Loader {
	if logger == nil {
		logger = slog.Default()
	}
	return &Loader{
		flows:  make(map[string]*FlowDefinition),
		dir:    dir,
		logger: logger,
	}
}

// OnChange registers a callback that fires when config files change.
func (l *Loader) OnChange(fn func(map[string]*FlowDefinition)) {
	l.onChange = fn
}

// Load reads all YAML files from the configured directory.
func (l *Loader) Load() (map[string]*FlowDefinition, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("read config dir %s: %w", l.dir, err)
	}

	flows := make(map[string]*FlowDefinition)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(l.dir, entry.Name())
		flow, err := l.loadFile(path)
		if err != nil {
			l.logger.Error("failed to load config file", "path", path, "error", err)
			continue
		}
		flows[flow.Name] = flow
	}

	l.mu.Lock()
	l.flows = flows
	l.mu.Unlock()

	return flows, nil
}

// Watch starts watching the config directory for changes. Blocks until ctx.Done.
func (l *Loader) Watch(done <-chan struct{}) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer func() {
		_ = watcher.Close() // intentionally ignoring close error during cleanup
	}()

	if err := watcher.Add(l.dir); err != nil {
		return fmt.Errorf("watch dir %s: %w", l.dir, err)
	}

	l.logger.Info("watching config directory", "dir", l.dir)

	for {
		select {
		case <-done:
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
				l.logger.Info("config change detected", "file", event.Name, "op", event.Op)
				flows, err := l.Load()
				if err != nil {
					l.logger.Error("failed to reload config", "error", err)
					continue
				}
				if l.onChange != nil {
					l.onChange(flows)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			l.logger.Error("watcher error", "error", err)
		}
	}
}

// GetFlows returns a copy of the currently loaded flows.
func (l *Loader) GetFlows() map[string]*FlowDefinition {
	l.mu.RLock()
	defer l.mu.RUnlock()

	flows := make(map[string]*FlowDefinition, len(l.flows))
	for k, v := range l.flows {
		flows[k] = v
	}
	return flows
}

func (l *Loader) loadFile(path string) (*FlowDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var flow FlowDefinition
	if err := yaml.Unmarshal(data, &flow); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	if flow.Name == "" {
		return nil, fmt.Errorf("flow definition missing 'name' field in %s", path)
	}

	return &flow, nil
}
