package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SecretConfig maps target names to secret file paths or env vars.
type SecretConfig struct {
	TargetName string
	Type       string // "Bearer", "APIKey", "Basic"
	FilePath   string // path to file containing the secret
	EnvVar     string // env var containing the secret (fallback if FilePath empty)
	HeaderName string // header name for APIKey type (default: "Authorization")
}

// SecretProvider reads credentials from mounted files or environment variables.
// This supports K8s Secrets mounted as volumes.
type SecretProvider struct {
	mu      sync.RWMutex
	configs map[string]SecretConfig
}

// NewSecretProvider creates a provider from a set of secret configs.
func NewSecretProvider(configs []SecretConfig) *SecretProvider {
	m := make(map[string]SecretConfig, len(configs))
	for _, c := range configs {
		m[c.TargetName] = c
	}
	return &SecretProvider{configs: m}
}

// GetCredentials reads the secret for the given target.
func (s *SecretProvider) GetCredentials(_ context.Context, targetName string) (*Credentials, error) {
	s.mu.RLock()
	cfg, ok := s.configs[targetName]
	s.mu.RUnlock()

	if !ok {
		return nil, nil
	}

	token, err := s.readSecret(cfg)
	if err != nil {
		return nil, fmt.Errorf("auth secret for %s: %w", targetName, err)
	}

	creds := &Credentials{
		Type:    cfg.Type,
		Token:   token,
		Headers: make(map[string]string),
	}

	switch cfg.Type {
	case "Bearer":
		creds.Headers["Authorization"] = "Bearer " + token
	case "APIKey":
		header := cfg.HeaderName
		if header == "" {
			header = "Authorization"
		}
		creds.Headers[header] = token
	case "Basic":
		creds.Headers["Authorization"] = "Basic " + token
	}

	return creds, nil
}

func (s *SecretProvider) readSecret(cfg SecretConfig) (string, error) {
	if cfg.FilePath != "" {
		data, err := os.ReadFile(filepath.Clean(cfg.FilePath))
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", cfg.FilePath, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if cfg.EnvVar != "" {
		val := os.Getenv(cfg.EnvVar)
		if val == "" {
			return "", fmt.Errorf("env var %s is empty", cfg.EnvVar)
		}
		return val, nil
	}
	return "", fmt.Errorf("no file path or env var configured")
}
