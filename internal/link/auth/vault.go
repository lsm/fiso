package auth

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// VaultSecret represents a secret returned by Vault.
type VaultSecret struct {
	Data     map[string]interface{}
	LeaseTTL time.Duration
}

// VaultClient abstracts the Vault API for testability.
type VaultClient interface {
	// ReadSecret reads a secret from the given path.
	ReadSecret(ctx context.Context, path string) (*VaultSecret, error)
}

// VaultConfig maps target names to Vault secret paths.
type VaultConfig struct {
	TargetName string
	SecretPath string // Vault path, e.g. "secret/data/myapp/crm-api"
	TokenField string // Field name within the secret data (default: "token")
	Type       string // "Bearer", "APIKey", "Basic"
	HeaderName string // For APIKey type
}

// VaultProvider reads credentials from HashiCorp Vault with caching
// and proactive refresh at 80% of the lease TTL.
type VaultProvider struct {
	client  VaultClient
	configs map[string]VaultConfig
	mu      sync.RWMutex
	cache   map[string]*vaultCacheEntry
	clock   func() time.Time
}

type vaultCacheEntry struct {
	creds     *Credentials
	fetchedAt time.Time
	ttl       time.Duration
}

// VaultProviderOption configures the VaultProvider.
type VaultProviderOption func(*VaultProvider)

// WithVaultClock sets the clock function (for testing).
func WithVaultClock(clock func() time.Time) VaultProviderOption {
	return func(p *VaultProvider) { p.clock = clock }
}

// NewVaultProvider creates a new Vault-based auth provider.
func NewVaultProvider(client VaultClient, configs []VaultConfig, opts ...VaultProviderOption) (*VaultProvider, error) {
	if client == nil {
		return nil, fmt.Errorf("vault client is required")
	}
	m := make(map[string]VaultConfig, len(configs))
	for _, c := range configs {
		if c.TokenField == "" {
			c.TokenField = "token"
		}
		m[c.TargetName] = c
	}
	p := &VaultProvider{
		client:  client,
		configs: m,
		cache:   make(map[string]*vaultCacheEntry),
		clock:   time.Now,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

// GetCredentials retrieves credentials for the target from Vault.
// Uses cached credentials when available and not expired (refreshes at 80% TTL).
func (p *VaultProvider) GetCredentials(ctx context.Context, targetName string) (*Credentials, error) {
	cfg, ok := p.configs[targetName]
	if !ok {
		return nil, nil
	}

	if creds := p.fromCache(targetName); creds != nil {
		return creds, nil
	}

	secret, err := p.client.ReadSecret(ctx, cfg.SecretPath)
	if err != nil {
		return nil, fmt.Errorf("vault read %s: %w", cfg.SecretPath, err)
	}

	tokenVal, ok := secret.Data[cfg.TokenField]
	if !ok {
		return nil, fmt.Errorf("vault secret %s missing field %q", cfg.SecretPath, cfg.TokenField)
	}
	token, ok := tokenVal.(string)
	if !ok {
		return nil, fmt.Errorf("vault secret field %q is not a string", cfg.TokenField)
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

	p.toCache(targetName, creds, secret.LeaseTTL)
	return creds, nil
}

func (p *VaultProvider) fromCache(targetName string) *Credentials {
	p.mu.RLock()
	defer p.mu.RUnlock()
	entry, ok := p.cache[targetName]
	if !ok {
		return nil
	}
	// Refresh at 80% of TTL
	elapsed := p.clock().Sub(entry.fetchedAt)
	refreshAt := time.Duration(float64(entry.ttl) * 0.8)
	if elapsed >= refreshAt {
		return nil
	}
	return entry.creds
}

func (p *VaultProvider) toCache(targetName string, creds *Credentials, ttl time.Duration) {
	if ttl == 0 {
		ttl = 5 * time.Minute // default TTL
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache[targetName] = &vaultCacheEntry{
		creds:     creds,
		fetchedAt: p.clock(),
		ttl:       ttl,
	}
}
