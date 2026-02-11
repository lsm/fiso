package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/oauth"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// ClientOptions returns kgo.Opt slice for the given cluster configuration.
func ClientOptions(cfg *ClusterConfig) ([]kgo.Opt, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
	}

	// Add SASL authentication
	if cfg.Auth.Mechanism != "" {
		saslOpt, err := saslOption(cfg.Auth)
		if err != nil {
			return nil, fmt.Errorf("sasl config: %w", err)
		}
		opts = append(opts, saslOpt)
	}

	// Add TLS configuration
	if cfg.TLS.Enabled {
		tlsConfig, err := buildTLSConfig(cfg.TLS)
		if err != nil {
			return nil, fmt.Errorf("tls config: %w", err)
		}
		opts = append(opts, kgo.DialTLSConfig(tlsConfig))
	}

	return opts, nil
}

// saslOption creates a SASL kgo.Opt from AuthConfig.
func saslOption(auth AuthConfig) (kgo.Opt, error) {
	var mechanism sasl.Mechanism

	switch auth.Mechanism {
	case "PLAIN":
		mechanism = plain.Auth{
			User: auth.Username,
			Pass: auth.Password,
		}.AsMechanism()

	case "SCRAM-SHA-256":
		mechanism = scram.Auth{
			User: auth.Username,
			Pass: auth.Password,
		}.AsSha256Mechanism()

	case "SCRAM-SHA-512":
		mechanism = scram.Auth{
			User: auth.Username,
			Pass: auth.Password,
		}.AsSha512Mechanism()

	case "OAUTHBEARER":
		if auth.OAuth == nil {
			return nil, fmt.Errorf("oauth config required for OAUTHBEARER")
		}
		return createOAuthMechanism(auth.OAuth)

	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s", auth.Mechanism)
	}

	return kgo.SASL(mechanism), nil
}

// createOAuthMechanism creates a Kafka SASL/OAUTHBEARER mechanism using the specified OAuth provider.
func createOAuthMechanism(cfg *OAuthConfig) (kgo.Opt, error) {
	switch cfg.Provider {
	case "azure":
		return createAzureOAuthMechanism(cfg)
	default:
		return nil, fmt.Errorf("unsupported oauth provider: %s", cfg.Provider)
	}
}

// createAzureOAuthMechanism creates an OAUTHBEARER mechanism using Azure AD tokens.
func createAzureOAuthMechanism(cfg *OAuthConfig) (kgo.Opt, error) {
	var cred azcore.TokenCredential
	var err error

	if cfg.ClientSecretEnv != "" {
		// Client credentials flow with secret from environment variable
		clientSecret := os.Getenv(cfg.ClientSecretEnv)
		if clientSecret == "" {
			return nil, fmt.Errorf("environment variable %s is not set or empty", cfg.ClientSecretEnv)
		}

		cred, err = azidentity.NewClientSecretCredential(
			cfg.TenantID,
			cfg.ClientID,
			clientSecret,
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("create azure client secret credential: %w", err)
		}
	} else {
		// Managed identity or default credential chain (for AKS)
		cred, err = azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("create azure default credential: %w", err)
		}
	}

	// Return OAUTHBEARER mechanism with token acquisition callback
	mechanism := oauth.Oauth(func(ctx context.Context) (oauth.Auth, error) {
		token, err := cred.GetToken(ctx, policy.TokenRequestOptions{
			Scopes: []string{cfg.Scope},
		})
		if err != nil {
			return oauth.Auth{}, fmt.Errorf("acquire azure token: %w", err)
		}

		return oauth.Auth{
			Token:      token.Token,
			Extensions: cfg.Extensions, // For Confluent Cloud (logicalCluster, identityPoolId)
		}, nil
	})

	return kgo.SASL(mechanism), nil
}

// buildTLSConfig creates a tls.Config from TLSConfig.
func buildTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.SkipVerify, //nolint:gosec // User-configurable option for dev/testing
	}

	// Load CA certificate if specified
	if cfg.CAFile != "" {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file %s: %w", cfg.CAFile, err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", cfg.CAFile)
		}
		tlsCfg.RootCAs = caCertPool
	}

	// Load client certificate for mTLS
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}
