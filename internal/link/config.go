package link

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

var validProtocols = map[string]bool{"http": true, "https": true, "grpc": true}

// LinkTarget defines an outbound target endpoint.
type LinkTarget struct {
	Name           string               `yaml:"name"`
	Protocol       string               `yaml:"protocol"` // http, https, grpc
	Host           string               `yaml:"host"`
	Auth           AuthConfig           `yaml:"auth"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuitBreaker"`
	Retry          RetryConfig          `yaml:"retry"`
	RateLimit      RateLimitConfig      `yaml:"rateLimit"`
	AllowedPaths   []string             `yaml:"allowedPaths"`
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

// CircuitBreakerConfig holds circuit breaker settings.
type CircuitBreakerConfig struct {
	Enabled          bool   `yaml:"enabled"`
	FailureThreshold int    `yaml:"failureThreshold"`
	SuccessThreshold int    `yaml:"successThreshold"`
	ResetTimeout     string `yaml:"resetTimeout"` // e.g., "30s"
}

// RetryConfig holds retry settings.
type RetryConfig struct {
	MaxAttempts     int     `yaml:"maxAttempts"`
	Backoff         string  `yaml:"backoff"`         // "exponential", "constant", "linear"
	InitialInterval string  `yaml:"initialInterval"` // e.g., "200ms"
	MaxInterval     string  `yaml:"maxInterval"`     // e.g., "30s"
	Jitter          float64 `yaml:"jitter"`
}

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	RequestsPerSecond float64 `yaml:"requestsPerSecond"`
	Burst             int     `yaml:"burst"`
}

// Config is the top-level Fiso-Link configuration.
type Config struct {
	ListenAddr   string       `yaml:"listenAddr"`
	MetricsAddr  string       `yaml:"metricsAddr"`
	Targets      []LinkTarget `yaml:"targets"`
	AsyncBrokers []string     `yaml:"asyncBrokers"`
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
		if t.Host == "" {
			errs = append(errs, fmt.Errorf("%s: host is required", prefix))
		}

		if t.Protocol != "" && !validProtocols[t.Protocol] {
			errs = append(errs, fmt.Errorf("%s: protocol %q is not valid (must be one of: http, https, grpc)", prefix, t.Protocol))
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
