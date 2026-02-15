package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/lsm/fiso/internal/kafka"
	"gopkg.in/yaml.v3"
)

var (
	validSourceTypes      = map[string]bool{"kafka": true, "grpc": true, "http": true}
	validSinkTypes        = map[string]bool{"http": true, "grpc": true, "temporal": true, "kafka": true}
	validInterceptorTypes = map[string]bool{"wasm": true, "grpc": true, "wasmer-app": true}
)

// Validate checks the FlowDefinition for configuration errors.
// Returns all errors found, not just the first.
func (f *FlowDefinition) Validate() error {
	var errs []error

	if f.Name == "" {
		errs = append(errs, fmt.Errorf("name is required"))
	}

	if f.Source.Type == "" {
		errs = append(errs, fmt.Errorf("source.type is required"))
	} else if !validSourceTypes[f.Source.Type] {
		errs = append(errs, fmt.Errorf("source.type %q is not valid (must be one of: kafka, grpc, http)", f.Source.Type))
	}

	if f.Sink.Type == "" {
		errs = append(errs, fmt.Errorf("sink.type is required"))
	} else if !validSinkTypes[f.Sink.Type] {
		errs = append(errs, fmt.Errorf("sink.type %q is not valid (must be one of: http, grpc, temporal, kafka)", f.Sink.Type))
	}

	// Transform validation
	if f.Transform != nil && len(f.Transform.Fields) == 0 {
		errs = append(errs, fmt.Errorf("transform: 'fields' is required when transform is defined"))
	}

	// Temporal sink validation.
	if f.Sink.Type == "temporal" {
		if f.Sink.Config == nil {
			errs = append(errs, fmt.Errorf("sink.config is required for temporal sink"))
		} else {
			if _, ok := f.Sink.Config["taskQueue"].(string); !ok {
				errs = append(errs, fmt.Errorf("sink.config.taskQueue is required for temporal sink"))
			}
			if _, ok := f.Sink.Config["workflowType"].(string); !ok {
				errs = append(errs, fmt.Errorf("sink.config.workflowType is required for temporal sink"))
			}
			mode, _ := f.Sink.Config["mode"].(string)
			if mode == "signal" {
				if _, ok := f.Sink.Config["signalName"].(string); !ok {
					errs = append(errs, fmt.Errorf("sink.config.signalName is required when mode is 'signal'"))
				}
			}
		}
	}

	// Interceptor validation.
	for i, ic := range f.Interceptors {
		if ic.Type == "" {
			errs = append(errs, fmt.Errorf("interceptors[%d].type is required", i))
		} else if !validInterceptorTypes[ic.Type] {
			errs = append(errs, fmt.Errorf("interceptors[%d].type %q is not valid (must be one of: wasm, grpc)", i, ic.Type))
		}
		if ic.Type == "wasm" {
			if _, ok := ic.Config["module"].(string); !ok {
				errs = append(errs, fmt.Errorf("interceptors[%d].config.module is required for wasm interceptor", i))
			}
			// Validate runtime if specified
			if runtime, ok := ic.Config["runtime"].(string); ok {
				validRuntimes := map[string]bool{"wazero": true, "wasmer": true}
				if !validRuntimes[runtime] {
					errs = append(errs, fmt.Errorf("interceptors[%d].config.runtime must be 'wazero' or 'wasmer', got %q", i, runtime))
				}
			}
		}
		if ic.Type == "wasmer-app" {
			if _, ok := ic.Config["module"].(string); !ok {
				errs = append(errs, fmt.Errorf("interceptors[%d].config.module is required for wasmer-app interceptor", i))
			}
			// Validate execution mode if specified
			if exec, ok := ic.Config["execution"].(string); ok {
				validModes := map[string]bool{"perRequest": true, "longRunning": true, "pooled": true}
				if !validModes[exec] {
					errs = append(errs, fmt.Errorf("interceptors[%d].config.execution must be 'perRequest', 'longRunning', or 'pooled', got %q", i, exec))
				}
			}
		}
	}

	if f.ErrorHandling.MaxRetries < 0 {
		errs = append(errs, fmt.Errorf("errorHandling.maxRetries must be >= 0, got %d", f.ErrorHandling.MaxRetries))
	}

	// Validate Kafka clusters if defined
	if err := f.Kafka.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("kafka: %w", err))
	}

	return errors.Join(errs...)
}

// FlowDefinition represents a complete inbound pipeline configuration.
type FlowDefinition struct {
	Name          string                  `yaml:"name"`
	Kafka         kafka.KafkaGlobalConfig `yaml:"kafka,omitempty"` // Named Kafka clusters
	Source        SourceConfig            `yaml:"source"`
	CloudEvents   *CloudEventsConfig      `yaml:"cloudevents,omitempty"`
	Transform     *TransformConfig        `yaml:"transform,omitempty"`
	Interceptors  []InterceptorConfig     `yaml:"interceptors,omitempty"`
	Sink          SinkConfig              `yaml:"sink"`
	ErrorHandling ErrorHandlingConfig     `yaml:"errorHandling"`
}

// InterceptorConfig holds configuration for a pipeline interceptor.
type InterceptorConfig struct {
	Type   string                 `yaml:"type"`
	Config map[string]interface{} `yaml:"config"`
}

// CloudEventsConfig holds overrides for the CloudEvents envelope fields.
// All fields support CEL expressions evaluated against the ORIGINAL input event (before
// transforms). CEL enables field combination, conditionals, and computations.
//
// CEL examples:
//
//	id: 'data.eventId + "-" + data.CTN'                    # Combine fields for idempotency
//	type: 'data.amount > 1000 ? "high-value" : "standard"' # Conditional type
//	source: '"service-" + data.region'                     # Dynamic source
//	subject: 'data.customerId'                             # Extract field
//	data: 'data.payload'                                   # Use specific nested field as data
//	datacontenttype: '"application/json"'                  # Static content type
//	dataschema: '"https://example.com/schemas/" + data.type + ".json"'  # Dynamic schema
//
// Literal values (non-CEL):
//
//	source: "my-service"    # Static string
//	type: "order.created"   # Static type
type CloudEventsConfig struct {
	ID              string `yaml:"id,omitempty"`              // CloudEvent ID for idempotency
	Type            string `yaml:"type,omitempty"`            // CloudEvent type
	Source          string `yaml:"source,omitempty"`          // CloudEvent source
	Subject         string `yaml:"subject,omitempty"`         // CloudEvent subject (optional)
	Data            string `yaml:"data,omitempty"`            // CloudEvent data (if empty, uses transformed payload)
	DataContentType string `yaml:"datacontenttype,omitempty"` // CloudEvent datacontenttype (optional, default: application/json)
	DataSchema      string `yaml:"dataschema,omitempty"`      // CloudEvent dataschema (optional)
}

// SourceConfig holds source configuration.
type SourceConfig struct {
	Type   string                 `yaml:"type"`
	Config map[string]interface{} `yaml:"config"`
}

// TransformConfig holds transform configuration using the unified fields syntax.
// Each field value is a CEL expression that produces the output field value.
type TransformConfig struct {
	Fields map[string]string `yaml:"fields,omitempty"`
}

// SinkConfig holds sink configuration.
type SinkConfig struct {
	Type   string                 `yaml:"type"`
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

	if err := flow.Validate(); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}

	return &flow, nil
}
