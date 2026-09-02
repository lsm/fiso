package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lsm/fiso/internal/delivery"
	"github.com/lsm/fiso/internal/kafka"
	"gopkg.in/yaml.v3"
)

var (
	validSourceTypes      = map[string]bool{"kafka": true, "grpc": true, "http": true}
	validSinkTypes        = map[string]bool{"http": true, "grpc": true, "temporal": true, "kafka": true}
	validInterceptorTypes = map[string]bool{"wasm": true, "grpc": true}
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

	// gRPC sink validation: the wired sink constructs from address (+ optional
	// timeout duration), so require exactly those settings to be usable.
	if f.Sink.Type == "grpc" {
		address, ok := f.Sink.Config["address"].(string)
		if !ok || address == "" {
			errs = append(errs, fmt.Errorf("sink.config.address is required for grpc sink"))
		}
		// A present timeout must be a positive duration string; null and empty
		// values do not fall back to the default silently.
		if timeout, present := f.Sink.Config["timeout"]; present {
			timeoutStr, isStr := timeout.(string)
			if !isStr {
				errs = append(errs, fmt.Errorf("sink.config.timeout must be a duration string"))
			} else if d, err := time.ParseDuration(timeoutStr); err != nil {
				errs = append(errs, fmt.Errorf("sink.config.timeout %q is not a valid duration", timeoutStr))
			} else if d < 0 {
				errs = append(errs, fmt.Errorf("sink.config.timeout %q must not be negative", timeoutStr))
			} else if d == 0 {
				// The sink treats a zero timeout as unset (30s default), so an
				// explicit zero would not be honored.
				errs = append(errs, fmt.Errorf("sink.config.timeout %q must be positive", timeoutStr))
			}
		}
		// The gRPC sink has no credentials configuration yet, so TLS cannot be
		// enabled; reject every value except an explicit false — including null,
		// which would otherwise silently select plaintext.
		if tlsVal, present := f.Sink.Config["tls"]; present {
			if enabled, isBool := tlsVal.(bool); !isBool || enabled {
				errs = append(errs, fmt.Errorf("sink.config.tls is not supported until gRPC TLS credentials are configurable"))
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
		if ic.Type == "grpc" {
			// The wired interceptor dials a sidecar; it must have a usable
			// address and, when supplied, a positive timeout duration.
			address, ok := ic.Config["address"].(string)
			if !ok || address == "" {
				errs = append(errs, fmt.Errorf("interceptors[%d].config.address is required for grpc interceptor", i))
			}
			if timeout, present := ic.Config["timeout"]; present && timeout != nil {
				timeoutStr, isStr := timeout.(string)
				if !isStr {
					errs = append(errs, fmt.Errorf("interceptors[%d].config.timeout must be a duration string", i))
				} else if d, err := time.ParseDuration(timeoutStr); err != nil {
					errs = append(errs, fmt.Errorf("interceptors[%d].config.timeout %q is not a valid duration", i, timeoutStr))
				} else if d <= 0 {
					errs = append(errs, fmt.Errorf("interceptors[%d].config.timeout %q must be positive", i, timeoutStr))
				}
			}
		}
		if ic.Type == "wasm" {
			if _, ok := ic.Config["module"].(string); !ok {
				errs = append(errs, fmt.Errorf("interceptors[%d].config.module is required for wasm interceptor", i))
			}
			// Host HTTP capability (ADR 0006): opt-in, deny-by-default.
			if httpVal, present := ic.Config["http"]; present && httpVal != nil {
				if enabled, isBool := httpVal.(bool); !isBool || !enabled {
					errs = append(errs, fmt.Errorf("interceptors[%d].config.http must be exactly true to enable host HTTP calls", i))
				} else {
					targets, _ := ic.Config["httpTargets"].([]interface{})
					if len(targets) == 0 {
						errs = append(errs, fmt.Errorf("interceptors[%d].config.httpTargets is required when http is enabled (deny-by-default allowlist)", i))
					}
					for j, tv := range targets {
						name, isStr := tv.(string)
						// A target is a single URL path segment: route syntax
						// (slashes, dots, query) would compose into the
						// /link/{target}{path} URL unsafely.
						if !isStr || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/?#%%") || name != url.PathEscape(name) {
							errs = append(errs, fmt.Errorf("interceptors[%d].config.httpTargets[%d] must be a single URL path segment (the Link target name)", i, j))
						}
					}
					// linkAddr is optional but, when present, must be a
					// usable URL string — no silent fallback to the default.
					if la, present := ic.Config["linkAddr"]; present && la != nil {
						linkAddr, isStr := la.(string)
						if !isStr || linkAddr == "" {
							errs = append(errs, fmt.Errorf("interceptors[%d].config.linkAddr must be an absolute http(s) origin string", i))
						} else if u, err := url.Parse(linkAddr); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(linkAddr, "?#") || (u.Path != "" && u.Path != "/") {
							// A path/query/fragment on linkAddr would be
							// silently dropped or miscomposed into the
							// /link/{target}{path} URL.
							errs = append(errs, fmt.Errorf("interceptors[%d].config.linkAddr %q must be an absolute http(s) origin (no path, query, or fragment)", i, linkAddr))
						}
					}
				}
			}
			// Validate runtime if specified. A present, non-nil value must be
			// a known runtime string; null is treated as omitted.
			if runtime, present := ic.Config["runtime"]; present && runtime != nil {
				runtimeStr, isStr := runtime.(string)
				validRuntimes := map[string]bool{"wazero": true, "wasmer": true}
				if !isStr {
					errs = append(errs, fmt.Errorf("interceptors[%d].config.runtime must be 'wazero' or 'wasmer', got non-string value", i))
				} else if !validRuntimes[runtimeStr] {
					errs = append(errs, fmt.Errorf("interceptors[%d].config.runtime must be 'wazero' or 'wasmer', got %q", i, runtimeStr))
				}
			}
		}
	}

	if f.ErrorHandling.MaxRetries < 0 {
		errs = append(errs, fmt.Errorf("errorHandling.maxRetries must be >= 0, got %d", f.ErrorHandling.MaxRetries))
	}

	commitPolicy := delivery.NormalizeCommitPolicy(f.ErrorHandling.CommitPolicy)
	if !commitPolicy.Valid() {
		errs = append(errs, fmt.Errorf("errorHandling.commitPolicy %q is not valid (must be one of: sink, sink_or_dlq, kafka_transaction)", f.ErrorHandling.CommitPolicy))
	}
	if commitPolicy == delivery.CommitPolicyKafkaTransaction {
		if f.Source.Type != "kafka" {
			errs = append(errs, fmt.Errorf("errorHandling.commitPolicy kafka_transaction requires source.type to be kafka"))
		}
		if f.Sink.Type != "kafka" {
			errs = append(errs, fmt.Errorf("errorHandling.commitPolicy kafka_transaction requires sink.type to be kafka"))
		}
		if f.ErrorHandling.TransactionalID == "" {
			errs = append(errs, fmt.Errorf("errorHandling.transactionalId is required when commitPolicy is kafka_transaction"))
		}
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
	CommitPolicy    string `yaml:"commitPolicy,omitempty"`    // sink | sink_or_dlq | kafka_transaction (default: sink_or_dlq)
	TransactionalID string `yaml:"transactionalId,omitempty"` // required when commitPolicy=kafka_transaction
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

// Load reads all YAML files from the configured directory. Files that fail
// to parse or validate are logged and skipped.
func (l *Loader) Load() (map[string]*FlowDefinition, error) {
	flows, _, err := l.loadEntries(false)
	return flows, err
}

// LoadStrict reads all YAML files like Load but returns an error joining
// every parse or validation failure instead of logging and skipping the
// file. Binaries that must fail closed on invalid configuration use this.
func (l *Loader) LoadStrict() (map[string]*FlowDefinition, error) {
	flows, errs, err := l.loadEntries(true)
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return flows, nil
}

func (l *Loader) loadEntries(strict bool) (map[string]*FlowDefinition, []error, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read config dir %s: %w", l.dir, err)
	}

	flows := make(map[string]*FlowDefinition)
	var errs []error
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
			if strict {
				errs = append(errs, err)
			} else {
				l.logger.Error("failed to load config file", "path", path, "error", err)
			}
			continue
		}
		flows[flow.Name] = flow
	}

	l.mu.Lock()
	l.flows = flows
	l.mu.Unlock()

	return flows, errs, nil
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
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var flow FlowDefinition
	if err := yaml.Unmarshal(data, &flow); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if err := flow.Validate(); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}

	return &flow, nil
}
