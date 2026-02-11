//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lsm/fiso/internal/kafka"
	"github.com/twmb/franz-go/pkg/kgo"
)

// TestKafkaOAuth_Azure tests OAuth authentication against a real Azure-integrated Kafka cluster.
// This test is skipped if KAFKA_OAUTH_BROKERS is not set.
func TestKafkaOAuth_Azure(t *testing.T) {
	brokers := os.Getenv("KAFKA_OAUTH_BROKERS")
	if brokers == "" {
		t.Skip("Skipping Kafka OAuth integration test: KAFKA_OAUTH_BROKERS not set")
	}

	tenantID := os.Getenv("AZURE_TENANT_ID")
	clientID := os.Getenv("AZURE_CLIENT_ID")
	secretEnv := os.Getenv("AZURE_CLIENT_SECRET_ENV")
	scope := os.Getenv("KAFKA_OAUTH_SCOPE")

	if tenantID == "" || clientID == "" || secretEnv == "" || scope == "" {
		t.Skip("Skipping: Azure OAuth environment variables not fully configured")
	}

	cfg := kafka.ClusterConfig{
		Brokers: []string{brokers},
		Auth: kafka.AuthConfig{
			Mechanism: "OAUTHBEARER",
			OAuth: &kafka.OAuthConfig{
				Provider:        "azure",
				TenantID:        tenantID,
				ClientID:        clientID,
				ClientSecretEnv: secretEnv,
				Scope:           scope,
			},
		},
		TLS: kafka.TLSConfig{
			Enabled: true,
		},
	}

	opts, err := kafka.ClientOptions(&cfg)
	if err != nil {
		t.Fatalf("failed to create client options: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := kgo.NewClient(opts...)
	if err != nil {
		t.Fatalf("failed to create kafka client: %v", err)
	}
	defer client.Close()

	// Ping the cluster
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("failed to ping kafka cluster: %v", err)
	}

	t.Log("Successfully authenticated to Kafka cluster using Azure OAuth")
}
