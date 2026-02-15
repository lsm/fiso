package link

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lsm/fiso/internal/kafka"
	"gopkg.in/yaml.v3"
)

var validProtocols = map[string]bool{"http": true, "https": true, "grpc": true, "kafka": true}

// LinkTarget defines an outbound target endpoint.
type LinkTarget struct {
	Name           string               `yaml:"name"`
	Protocol       string               `yaml:"protocol"` // http, https, grpc, kafka
	Host           string               `yaml:"host"`
	Auth           AuthConfig           `yaml:"auth"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuitBreaker"`
	Retry          RetryConfig          `yaml:"retry"`
	RateLimit      RateLimitConfig      `yaml:"rateLimit"`
	AllowedPaths   []string             `yaml:"allowedPaths"`
	Kafka          *KafkaConfig         `yaml:"kafka,omitempty"`   // Kafka-specific settings
	Interceptors   []InterceptorConfig  `yaml:"interceptors"`      // Interceptor chain configuration
}

// InterceptorConfig defines a single interceptor in the chain.
type InterceptorConfig struct {
	Type   string                 `yaml:"type"`             // "wasm" (future: "lua", "native")
	Config map[string]interface{} `yaml:"config"`           // Interceptor-specific configuration
}

// WASMInterceptorConfig defines WASM-specific interceptor configuration.
type WASMInterceptorConfig struct {
	Module    string `yaml:"module"`    // Path to WASM module file
	Phase     string `yaml:"phase"`     // "outbound" or "inbound" (default: outbound)
	FailOpen  bool   `yaml:"failOpen"`  // Continue on error if true (default: false)
}

// AuthConfig defines authentication settings for a target.
type AuthConfig struct {
	Type      string     `yaml:"type"` // bearer, apikey, basic, none
	SecretRef *SecretRef `yaml:"secretRef,omitempty"`
	VaultRef  *VaultRef  `yaml:"vaultRef,omitempty"`
}

// SecretRef references a K8s Secret mounted as a file.
type SecretRef struct {
	FilePath string `yaml:"filePath"`
	EnvVar   string `yaml:"envVar"`
}

// VaultRef references a Vault secret.
type VaultRef struct {
	Path string `yaml:"path"`
	Role string `yaml:"role"`
}

// KafkaConfig defines Kafka-specific settings for a target.
type KafkaConfig struct {
	Cluster      string            `yaml:"cluster,omitempty"`      // Reference to named cluster in kafka.clusters
	Topic        string            `yaml:"topic"`                  // Required: Kafka topic
	Key          KeyStrategy       `yaml:"key"`                    // Optional: Key generation strategy
	Headers      map[string]string `yaml:"headers,omitempty"`      // Optional: Static Kafka headers
	RequiredAcks string            `yaml:"requiredAcks,omitempty"` // Optional: "all", "1" (default)
}

// KeyStrategy defines how to generate Kafka message keys.
type KeyStrategy struct {
	Type  string `yaml:"type"`  // uuid, header, payload, static, random
	Field string `yaml:"field"` // For "header" or "payload" types
	Value string `yaml:"value"` // For "static" type
}

// CircuitBreakerConfig holds circuit breaker settings.
type CircuitBreakerConfig struct {
	Enabled          bool   `yaml:"enabled"`
	FailureThreshold int    `yaml:"failureThreshold"`
	SuccessThreshold int    `yaml:"successThreshold"`
	ResetTimeout     string `yaml:"resetTimeout"` // e.g., "30s"
}

// UnmarshalYAML implements custom unmarshaling for CircuitBreakerConfig.
// It handles both string and integer duration formats for ResetTimeout.
func (c *CircuitBreakerConfig) UnmarshalYAML(value *yaml.Node) error {
	// Decode into raw map to handle type flexibility
	var raw map[string]interface{}
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("decode circuitBreaker: %w", err)
	}

	// Set defaults
	c.Enabled = false
	c.FailureThreshold = 0
	c.SuccessThreshold = 0
	c.ResetTimeout = ""

	// Parse each field
	if v, ok := raw["enabled"]; ok {
		if b, ok := v.(bool); ok {
			c.Enabled = b
		}
	}
	if v, ok := raw["failureThreshold"]; ok {
		switch tv := v.(type) {
		case int:
			c.FailureThreshold = tv
		case float64:
			c.FailureThreshold = int(tv)
		}
	}
	if v, ok := raw["successThreshold"]; ok {
		switch tv := v.(type) {
		case int:
			c.SuccessThreshold = tv
		case float64:
			c.SuccessThreshold = int(tv)
		}
	}
	if v, ok := raw["resetTimeout"]; ok {
		switch tv := v.(type) {
		case string:
			c.ResetTimeout = tv
		case int:
			c.ResetTimeout = fmt.Sprintf("%dms", tv)
		case float64:
			c.ResetTimeout = fmt.Sprintf("%.0fms", tv)
		}
	}

	return nil
}

// RetryConfig holds retry settings.
type RetryConfig struct {
	MaxAttempts     int     `yaml:"maxAttempts"`
	Backoff         string  `yaml:"backoff"`         // "exponential", "constant", "linear"
	InitialInterval string  `yaml:"initialInterval"` // e.g., "200ms"
	MaxInterval     string  `yaml:"maxInterval"`     // e.g., "30s"
	Jitter          float64 `yaml:"jitter"`
}

// UnmarshalYAML implements custom unmarshaling for RetryConfig.
// It handles both string and integer formats for duration fields.
func (r *RetryConfig) UnmarshalYAML(value *yaml.Node) error {
	// Decode into raw map to handle type flexibility
	var raw map[string]interface{}
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("decode retry: %w", err)
	}

	// Set defaults
	r.MaxAttempts = 0
	r.Backoff = ""
	r.InitialInterval = ""
	r.MaxInterval = ""
	r.Jitter = 0

	// Parse each field
	if v, ok := raw["maxAttempts"]; ok {
		switch tv := v.(type) {
		case int:
			r.MaxAttempts = tv
		case float64:
			r.MaxAttempts = int(tv)
		}
	}
	if v, ok := raw["backoff"]; ok {
		if s, ok := v.(string); ok {
			r.Backoff = s
		}
	}
	if v, ok := raw["initialInterval"]; ok {
		switch tv := v.(type) {
		case string:
			r.InitialInterval = tv
		case int:
			r.InitialInterval = fmt.Sprintf("%dms", tv)
		case float64:
			r.InitialInterval = fmt.Sprintf("%.0fms", tv)
		}
	}
	if v, ok := raw["maxInterval"]; ok {
		switch tv := v.(type) {
		case string:
			r.MaxInterval = tv
		case int:
			r.MaxInterval = fmt.Sprintf("%dms", tv)
		case float64:
			r.MaxInterval = fmt.Sprintf("%.0fms", tv)
		}
	}
	if v, ok := raw["jitter"]; ok {
		switch tv := v.(type) {
		case float64:
			r.Jitter = tv
		case int:
			r.Jitter = float64(tv)
		}
	}

	return nil
}

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	RequestsPerSecond float64 `yaml:"requestsPerSecond"`
	Burst             int     `yaml:"burst"`
}

// UnmarshalYAML implements custom unmarshaling for RateLimitConfig.
// It handles numeric types flexibly and provides clear error messages.
func (r *RateLimitConfig) UnmarshalYAML(value *yaml.Node) error {
	// Decode into raw map to handle type flexibility
	var raw map[string]interface{}
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("decode rateLimit: %w", err)
	}

	// Set defaults
	r.RequestsPerSecond = 0
	r.Burst = 0

	// Parse each field
	if v, ok := raw["requestsPerSecond"]; ok {
		switch tv := v.(type) {
		case int:
			r.RequestsPerSecond = float64(tv)
		case float64:
			r.RequestsPerSecond = tv
		case int64:
			r.RequestsPerSecond = float64(tv)
		}
	}
	if v, ok := raw["burst"]; ok {
		switch tv := v.(type) {
		case int:
			r.Burst = tv
		case float64:
			r.Burst = int(tv)
		case int64:
			r.Burst = int(tv)
		}
	}

	return nil
}

// Config is the top-level Fiso-Link configuration.
type Config struct {
	ListenAddr  string                  `yaml:"listenAddr"`
	MetricsAddr string                  `yaml:"metricsAddr"`
	Targets     []LinkTarget            `yaml:"targets"`
	Kafka       kafka.KafkaGlobalConfig `yaml:"kafka,omitempty"`
}

// LoadConfig reads Fiso-Link configuration from a YAML file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:3500"
	}
	if cfg.MetricsAddr == "" {
		cfg.MetricsAddr = ":9090"
	}

	for i, t := range cfg.Targets {
		if t.Protocol == "" {
			cfg.Targets[i].Protocol = "https"
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}

	return &cfg, nil
}

// Validate checks the link Config for configuration errors.
// Returns all errors found, not just the first.
func (c *Config) Validate() error {
	var errs []error

	for i, t := range c.Targets {
		prefix := fmt.Sprintf("target[%d] %q", i, t.Name)

		if t.Name == "" {
			errs = append(errs, fmt.Errorf("target[%d]: name is required", i))
			continue
		}
		if t.Host == "" && t.Protocol != "kafka" {
			errs = append(errs, fmt.Errorf("%s: host is required (except for kafka protocol)", prefix))
		}

		if t.Protocol != "" && !validProtocols[t.Protocol] {
			errs = append(errs, fmt.Errorf("%s: protocol %q is not valid (must be one of: http, https, grpc, kafka)", prefix, t.Protocol))
		}

		// Kafka-specific validation
		if t.Protocol == "kafka" {
			if t.Kafka == nil || t.Kafka.Topic == "" {
				errs = append(errs, fmt.Errorf("%s: kafka protocol requires topic in kafka.topic field", prefix))
			}
			if t.Host != "" {
				errs = append(errs, fmt.Errorf("%s: kafka protocol does not use host field (use kafka.clusters in config)", prefix))
			}
			// Require cluster reference
			if t.Kafka == nil || t.Kafka.Cluster == "" {
				errs = append(errs, fmt.Errorf("%s: kafka protocol requires cluster reference in kafka.cluster field", prefix))
			} else if _, ok := c.Kafka.Clusters[t.Kafka.Cluster]; !ok {
				errs = append(errs, fmt.Errorf("%s: kafka.cluster %q not found in kafka.clusters", prefix, t.Kafka.Cluster))
			}
			// Validate key strategy
			if t.Kafka != nil && t.Kafka.Key.Type != "" {
				validKeyTypes := map[string]bool{"uuid": true, "header": true, "payload": true, "static": true, "random": true}
				if !validKeyTypes[t.Kafka.Key.Type] {
					errs = append(errs, fmt.Errorf("%s: invalid key type %q (must be one of: uuid, header, payload, static, random)", prefix, t.Kafka.Key.Type))
				}
				if (t.Kafka.Key.Type == "header" || t.Kafka.Key.Type == "payload") && t.Kafka.Key.Field == "" {
					errs = append(errs, fmt.Errorf("%s: key type %q requires field parameter", prefix, t.Kafka.Key.Type))
				}
				if t.Kafka.Key.Type == "static" && t.Kafka.Key.Value == "" {
					errs = append(errs, fmt.Errorf("%s: key type static requires value parameter", prefix))
				}
			}
		}

		if t.CircuitBreaker.ResetTimeout != "" {
			if _, err := time.ParseDuration(t.CircuitBreaker.ResetTimeout); err != nil {
				errs = append(errs, fmt.Errorf("%s: circuitBreaker.resetTimeout %q is not a valid duration", prefix, t.CircuitBreaker.ResetTimeout))
			}
		}
		if t.Retry.InitialInterval != "" {
			if _, err := time.ParseDuration(t.Retry.InitialInterval); err != nil {
				errs = append(errs, fmt.Errorf("%s: retry.initialInterval %q is not a valid duration", prefix, t.Retry.InitialInterval))
			}
		}
		if t.Retry.MaxInterval != "" {
			if _, err := time.ParseDuration(t.Retry.MaxInterval); err != nil {
				errs = append(errs, fmt.Errorf("%s: retry.maxInterval %q is not a valid duration", prefix, t.Retry.MaxInterval))
			}
		}

		if t.Retry.Jitter < 0 || t.Retry.Jitter > 1.0 {
			errs = append(errs, fmt.Errorf("%s: retry.jitter must be between 0.0 and 1.0, got %f", prefix, t.Retry.Jitter))
		}

		if t.RateLimit.RequestsPerSecond < 0 {
			errs = append(errs, fmt.Errorf("%s: rateLimit.requestsPerSecond must be >= 0", prefix))
		}
		if t.RateLimit.Burst < 0 {
			errs = append(errs, fmt.Errorf("%s: rateLimit.burst must be >= 0", prefix))
		}

		// Validate interceptors
		for j, ic := range t.Interceptors {
			icPrefix := fmt.Sprintf("%s: interceptors[%d]", prefix, j)
			if ic.Type == "" {
				errs = append(errs, fmt.Errorf("%s: type is required", icPrefix))
				continue
			}
			validInterceptorTypes := map[string]bool{"wasm": true}
			if !validInterceptorTypes[ic.Type] {
				errs = append(errs, fmt.Errorf("%s: type %q is not valid (must be one of: wasm)", icPrefix, ic.Type))
			}
		}
	}

	// Validate Kafka clusters
	if err := c.Kafka.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("kafka: %w", err))
	}

	return errors.Join(errs...)
}

// TargetStore provides thread-safe access to link targets by name.
type TargetStore struct {
	mu      sync.RWMutex
	targets map[string]*LinkTarget
}

// NewTargetStore creates a store from a list of targets.
func NewTargetStore(targets []LinkTarget) *TargetStore {
	m := make(map[string]*LinkTarget, len(targets))
	for i := range targets {
		m[targets[i].Name] = &targets[i]
	}
	return &TargetStore{targets: m}
}

// Get returns a target by name, or nil if not found.
func (s *TargetStore) Get(name string) *LinkTarget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.targets[name]
}

// Update replaces the target set.
func (s *TargetStore) Update(targets []LinkTarget) {
	m := make(map[string]*LinkTarget, len(targets))
	for i := range targets {
		m[targets[i].Name] = &targets[i]
	}
	s.mu.Lock()
	s.targets = m
	s.mu.Unlock()
}

// Names returns all target names.
func (s *TargetStore) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.targets))
	for k := range s.targets {
		names = append(names, k)
	}
	return names
}
