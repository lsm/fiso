package link

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// LinkTarget defines an outbound target endpoint.
type LinkTarget struct {
	Name           string              `yaml:"name"`
	Protocol       string              `yaml:"protocol"` // http, https, grpc
	Host           string              `yaml:"host"`
	Auth           AuthConfig          `yaml:"auth"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuitBreaker"`
	Retry          RetryConfig         `yaml:"retry"`
	AllowedPaths   []string            `yaml:"allowedPaths"`
}

// AuthConfig defines authentication settings for a target.
type AuthConfig struct {
	Type       string     `yaml:"type"` // bearer, apikey, basic, none
	SecretRef  *SecretRef `yaml:"secretRef,omitempty"`
	VaultRef   *VaultRef  `yaml:"vaultRef,omitempty"`
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
	MaxAttempts     int    `yaml:"maxAttempts"`
	Backoff         string `yaml:"backoff"`         // "exponential", "constant", "linear"
	InitialInterval string `yaml:"initialInterval"` // e.g., "200ms"
	MaxInterval     string `yaml:"maxInterval"`     // e.g., "30s"
	Jitter          float64 `yaml:"jitter"`
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
		if t.Name == "" {
			return nil, fmt.Errorf("target %d: name is required", i)
		}
		if t.Host == "" {
			return nil, fmt.Errorf("target %q: host is required", t.Name)
		}
		if t.Protocol == "" {
			cfg.Targets[i].Protocol = "https"
		}
	}

	return &cfg, nil
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
