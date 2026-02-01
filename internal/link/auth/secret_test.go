package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSecretProvider_BearerFromFile(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("my-secret-token\n"), 0600); err != nil {
		t.Fatal(err)
	}

	p := NewSecretProvider([]SecretConfig{
		{TargetName: "crm", Type: "Bearer", FilePath: tokenFile},
	})

	creds, err := p.GetCredentials(context.Background(), "crm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Type != "Bearer" {
		t.Errorf("expected Bearer, got %s", creds.Type)
	}
	if creds.Token != "my-secret-token" {
		t.Errorf("expected my-secret-token, got %s", creds.Token)
	}
	if creds.Headers["Authorization"] != "Bearer my-secret-token" {
		t.Errorf("expected Bearer header, got %s", creds.Headers["Authorization"])
	}
}

func TestSecretProvider_APIKeyFromFile(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	if err := os.WriteFile(keyFile, []byte("api-key-123"), 0600); err != nil {
		t.Fatal(err)
	}

	p := NewSecretProvider([]SecretConfig{
		{TargetName: "svc", Type: "APIKey", FilePath: keyFile, HeaderName: "X-API-Key"},
	})

	creds, err := p.GetCredentials(context.Background(), "svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Headers["X-API-Key"] != "api-key-123" {
		t.Errorf("expected X-API-Key header, got %v", creds.Headers)
	}
}

func TestSecretProvider_APIKeyDefaultHeader(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	if err := os.WriteFile(keyFile, []byte("key-val"), 0600); err != nil {
		t.Fatal(err)
	}

	p := NewSecretProvider([]SecretConfig{
		{TargetName: "svc", Type: "APIKey", FilePath: keyFile},
	})

	creds, err := p.GetCredentials(context.Background(), "svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Headers["Authorization"] != "key-val" {
		t.Errorf("expected Authorization header, got %v", creds.Headers)
	}
}

func TestSecretProvider_BasicFromEnv(t *testing.T) {
	t.Setenv("TEST_BASIC_AUTH", "dXNlcjpwYXNz")

	p := NewSecretProvider([]SecretConfig{
		{TargetName: "legacy", Type: "Basic", EnvVar: "TEST_BASIC_AUTH"},
	})

	creds, err := p.GetCredentials(context.Background(), "legacy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Headers["Authorization"] != "Basic dXNlcjpwYXNz" {
		t.Errorf("expected Basic header, got %s", creds.Headers["Authorization"])
	}
}

func TestSecretProvider_UnknownTarget(t *testing.T) {
	p := NewSecretProvider(nil)
	creds, err := p.GetCredentials(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds != nil {
		t.Fatalf("expected nil creds for unknown target, got %v", creds)
	}
}

func TestSecretProvider_MissingFile(t *testing.T) {
	p := NewSecretProvider([]SecretConfig{
		{TargetName: "svc", Type: "Bearer", FilePath: "/nonexistent/token"},
	})
	_, err := p.GetCredentials(context.Background(), "svc")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSecretProvider_EmptyEnvVar(t *testing.T) {
	t.Setenv("EMPTY_VAR", "")

	p := NewSecretProvider([]SecretConfig{
		{TargetName: "svc", Type: "Bearer", EnvVar: "EMPTY_VAR"},
	})
	_, err := p.GetCredentials(context.Background(), "svc")
	if err == nil {
		t.Fatal("expected error for empty env var")
	}
}

func TestSecretProvider_NoSourceConfigured(t *testing.T) {
	p := NewSecretProvider([]SecretConfig{
		{TargetName: "svc", Type: "Bearer"},
	})
	_, err := p.GetCredentials(context.Background(), "svc")
	if err == nil {
		t.Fatal("expected error when no file or env configured")
	}
}

func TestNoopProvider(t *testing.T) {
	p := &NoopProvider{}
	creds, err := p.GetCredentials(context.Background(), "anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds != nil {
		t.Fatal("expected nil from NoopProvider")
	}
}
