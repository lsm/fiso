//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	temporalsink "github.com/lsm/fiso/internal/sink/temporal"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

func temporalHostPort() string {
	hp := os.Getenv("TEMPORAL_HOST_PORT")
	if hp == "" {
		hp = "localhost:7233"
	}
	return hp
}

func skipIfNoTemporal(t *testing.T) {
	if os.Getenv("TEMPORAL_HOST_PORT") == "" {
		t.Skip("Skipping Temporal integration test: TEMPORAL_HOST_PORT not set (run locally with: docker run -d -p 7233:7233 temporalio/auto-setup:latest)")
	}
}

func TestTemporalAuth_NoAuth(t *testing.T) {
	skipIfNoTemporal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// No auth - plain connection
	c, err := client.Dial(client.Options{
		HostPort:  temporalHostPort(),
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("failed to dial Temporal: %v", err)
	}
	defer c.Close()

	// Verify connection by describing the namespace
	resp, err := c.WorkflowService().DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("failed to describe namespace: %v", err)
	}

	if resp.NamespaceInfo.GetName() != "default" {
		t.Errorf("expected namespace 'default', got %q", resp.NamespaceInfo.GetName())
	}
}

func TestTemporalAuth_StaticAPIKey(t *testing.T) {
	skipIfNoTemporal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Build credentials with static API key
	cfg := temporalsink.Config{
		HostPort:  temporalHostPort(),
		Namespace: "default",
		Auth: temporalsink.AuthConfig{
			APIKey: "test-integration-key-12345",
		},
	}

	creds, err := temporalsink.BuildCredentials(cfg)
	if err != nil {
		t.Fatalf("failed to build credentials: %v", err)
	}

	c, err := client.Dial(client.Options{
		HostPort:    cfg.HostPort,
		Namespace:   cfg.Namespace,
		Credentials: creds,
	})
	if err != nil {
		t.Fatalf("failed to dial Temporal: %v", err)
	}
	defer c.Close()

	// Verify connection by describing the namespace
	resp, err := c.WorkflowService().DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("failed to describe namespace: %v", err)
	}

	if resp.NamespaceInfo.GetName() != "default" {
		t.Errorf("expected namespace 'default', got %q", resp.NamespaceInfo.GetName())
	}
}

func TestTemporalAuth_DynamicAPIKeyFromEnv(t *testing.T) {
	skipIfNoTemporal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Set environment variable for dynamic API key
	t.Setenv("TEMPORAL_INTEGRATION_API_KEY", "dynamic-key-from-env")

	// Build credentials with env-based API key
	cfg := temporalsink.Config{
		HostPort:  temporalHostPort(),
		Namespace: "default",
		Auth: temporalsink.AuthConfig{
			APIKeyEnv: "TEMPORAL_INTEGRATION_API_KEY",
		},
	}

	creds, err := temporalsink.BuildCredentials(cfg)
	if err != nil {
		t.Fatalf("failed to build credentials: %v", err)
	}

	c, err := client.Dial(client.Options{
		HostPort:    cfg.HostPort,
		Namespace:   cfg.Namespace,
		Credentials: creds,
	})
	if err != nil {
		t.Fatalf("failed to dial Temporal: %v", err)
	}
	defer c.Close()

	// Verify connection by describing the namespace
	resp, err := c.WorkflowService().DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("failed to describe namespace: %v", err)
	}

	if resp.NamespaceInfo.GetName() != "default" {
		t.Errorf("expected namespace 'default', got %q", resp.NamespaceInfo.GetName())
	}
}

func TestTemporalAuth_TokenFile(t *testing.T) {
	skipIfNoTemporal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Create temp directory and token file
	tempDir := t.TempDir()
	tokenFilePath := filepath.Join(tempDir, "token.txt")
	if err := os.WriteFile(tokenFilePath, []byte("file-based-bearer-token"), 0600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}

	// Build credentials with token file
	cfg := temporalsink.Config{
		HostPort:  temporalHostPort(),
		Namespace: "default",
		Auth: temporalsink.AuthConfig{
			TokenFile: tokenFilePath,
		},
	}

	creds, err := temporalsink.BuildCredentials(cfg)
	if err != nil {
		t.Fatalf("failed to build credentials: %v", err)
	}

	c, err := client.Dial(client.Options{
		HostPort:    cfg.HostPort,
		Namespace:   cfg.Namespace,
		Credentials: creds,
	})
	if err != nil {
		t.Fatalf("failed to dial Temporal: %v", err)
	}
	defer c.Close()

	// Verify connection by describing the namespace
	resp, err := c.WorkflowService().DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("failed to describe namespace: %v", err)
	}

	if resp.NamespaceInfo.GetName() != "default" {
		t.Errorf("expected namespace 'default', got %q", resp.NamespaceInfo.GetName())
	}
}
