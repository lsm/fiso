package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type mockVaultClient struct {
	secret *VaultSecret
	err    error
	calls  int
}

func (m *mockVaultClient) ReadSecret(_ context.Context, _ string) (*VaultSecret, error) {
	m.calls++
	return m.secret, m.err
}

func TestVaultProvider_GetCredentials_Bearer(t *testing.T) {
	mc := &mockVaultClient{
		secret: &VaultSecret{
			Data:     map[string]interface{}{"token": "vault-token-123"},
			LeaseTTL: 5 * time.Minute,
		},
	}
	p, err := NewVaultProvider(mc, []VaultConfig{
		{TargetName: "crm", SecretPath: "secret/data/crm", Type: "Bearer"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	creds, err := p.GetCredentials(context.Background(), "crm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Token != "vault-token-123" {
		t.Errorf("expected token 'vault-token-123', got %q", creds.Token)
	}
	if creds.Headers["Authorization"] != "Bearer vault-token-123" {
		t.Errorf("unexpected Authorization header: %q", creds.Headers["Authorization"])
	}
}

func TestVaultProvider_GetCredentials_APIKey(t *testing.T) {
	mc := &mockVaultClient{
		secret: &VaultSecret{
			Data:     map[string]interface{}{"token": "api-key-456"},
			LeaseTTL: 3 * time.Minute,
		},
	}
	p, _ := NewVaultProvider(mc, []VaultConfig{
		{TargetName: "api", SecretPath: "secret/data/api", Type: "APIKey", HeaderName: "X-API-Key"},
	})

	creds, err := p.GetCredentials(context.Background(), "api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Headers["X-API-Key"] != "api-key-456" {
		t.Errorf("expected X-API-Key header, got %v", creds.Headers)
	}
}

func TestVaultProvider_CachesResults(t *testing.T) {
	now := time.Now()
	mc := &mockVaultClient{
		secret: &VaultSecret{
			Data:     map[string]interface{}{"token": "cached-token"},
			LeaseTTL: 10 * time.Minute,
		},
	}
	p, _ := NewVaultProvider(mc, []VaultConfig{
		{TargetName: "svc", SecretPath: "secret/data/svc", Type: "Bearer"},
	}, WithVaultClock(func() time.Time { return now }))

	_, _ = p.GetCredentials(context.Background(), "svc")
	if mc.calls != 1 {
		t.Fatalf("expected 1 call, got %d", mc.calls)
	}

	// Second call should use cache
	_, _ = p.GetCredentials(context.Background(), "svc")
	if mc.calls != 1 {
		t.Errorf("expected still 1 call (cached), got %d", mc.calls)
	}
}

func TestVaultProvider_RefreshesAt80PercentTTL(t *testing.T) {
	now := time.Now()
	mc := &mockVaultClient{
		secret: &VaultSecret{
			Data:     map[string]interface{}{"token": "refreshed"},
			LeaseTTL: 10 * time.Minute,
		},
	}
	p, _ := NewVaultProvider(mc, []VaultConfig{
		{TargetName: "svc", SecretPath: "secret/data/svc", Type: "Bearer"},
	}, WithVaultClock(func() time.Time { return now }))

	_, _ = p.GetCredentials(context.Background(), "svc")
	if mc.calls != 1 {
		t.Fatalf("expected 1 call, got %d", mc.calls)
	}

	// Advance to 79% TTL — still cached
	now = now.Add(7*time.Minute + 54*time.Second) // 7.9min = 79% of 10min
	_, _ = p.GetCredentials(context.Background(), "svc")
	if mc.calls != 1 {
		t.Errorf("expected still 1 call at 79%% TTL, got %d", mc.calls)
	}

	// Advance past 80% TTL — should refresh
	now = now.Add(10 * time.Second) // now at ~8.07min = 80.7% of 10min
	_, _ = p.GetCredentials(context.Background(), "svc")
	if mc.calls != 2 {
		t.Errorf("expected 2 calls after 80%% TTL, got %d", mc.calls)
	}
}

func TestVaultProvider_UnknownTarget(t *testing.T) {
	mc := &mockVaultClient{}
	p, _ := NewVaultProvider(mc, []VaultConfig{})

	creds, err := p.GetCredentials(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds != nil {
		t.Error("expected nil credentials for unknown target")
	}
}

func TestVaultProvider_ReadError(t *testing.T) {
	mc := &mockVaultClient{err: errors.New("vault sealed")}
	p, _ := NewVaultProvider(mc, []VaultConfig{
		{TargetName: "svc", SecretPath: "secret/data/svc", Type: "Bearer"},
	})

	_, err := p.GetCredentials(context.Background(), "svc")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "vault sealed") {
		t.Errorf("expected 'vault sealed' error, got %q", err.Error())
	}
}

func TestVaultProvider_MissingField(t *testing.T) {
	mc := &mockVaultClient{
		secret: &VaultSecret{
			Data:     map[string]interface{}{"wrong_field": "value"},
			LeaseTTL: 5 * time.Minute,
		},
	}
	p, _ := NewVaultProvider(mc, []VaultConfig{
		{TargetName: "svc", SecretPath: "secret/data/svc", Type: "Bearer"},
	})

	_, err := p.GetCredentials(context.Background(), "svc")
	if err == nil {
		t.Fatal("expected error for missing token field")
	}
}

func TestVaultProvider_NonStringToken(t *testing.T) {
	mc := &mockVaultClient{
		secret: &VaultSecret{
			Data:     map[string]interface{}{"token": 12345},
			LeaseTTL: 5 * time.Minute,
		},
	}
	p, _ := NewVaultProvider(mc, []VaultConfig{
		{TargetName: "svc", SecretPath: "secret/data/svc", Type: "Bearer"},
	})

	_, err := p.GetCredentials(context.Background(), "svc")
	if err == nil {
		t.Fatal("expected error for non-string token")
	}
}

func TestNewVaultProvider_NilClient(t *testing.T) {
	_, err := NewVaultProvider(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestVaultProvider_CustomTokenField(t *testing.T) {
	mc := &mockVaultClient{
		secret: &VaultSecret{
			Data:     map[string]interface{}{"api_key": "custom-key"},
			LeaseTTL: 5 * time.Minute,
		},
	}
	p, _ := NewVaultProvider(mc, []VaultConfig{
		{TargetName: "svc", SecretPath: "secret/data/svc", Type: "Bearer", TokenField: "api_key"},
	})

	creds, err := p.GetCredentials(context.Background(), "svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Token != "custom-key" {
		t.Errorf("expected 'custom-key', got %q", creds.Token)
	}
}

func TestVaultProvider_DefaultTTL(t *testing.T) {
	now := time.Now()
	mc := &mockVaultClient{
		secret: &VaultSecret{
			Data:     map[string]interface{}{"token": "t"},
			LeaseTTL: 0, // no TTL — should use default 5min
		},
	}
	p, _ := NewVaultProvider(mc, []VaultConfig{
		{TargetName: "svc", SecretPath: "secret/data/svc", Type: "Bearer"},
	}, WithVaultClock(func() time.Time { return now }))

	_, _ = p.GetCredentials(context.Background(), "svc")

	// At 3 minutes (60% of 5min) — still cached
	now = now.Add(3 * time.Minute)
	_, _ = p.GetCredentials(context.Background(), "svc")
	if mc.calls != 1 {
		t.Errorf("expected 1 call at 60%% of default TTL, got %d", mc.calls)
	}

	// At 4.5 minutes (90% of 5min) — should refresh
	now = now.Add(90 * time.Second)
	_, _ = p.GetCredentials(context.Background(), "svc")
	if mc.calls != 2 {
		t.Errorf("expected 2 calls past 80%% of default TTL, got %d", mc.calls)
	}
}
