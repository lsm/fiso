package temporal

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"strings"

	"go.temporal.io/sdk/client"
	"golang.org/x/oauth2/clientcredentials"
)

// BuildCredentials creates Temporal credentials from config.
// Returns nil if no authentication is configured.
func BuildCredentials(cfg Config) (client.Credentials, error) {
	// API key authentication (static)
	if cfg.Auth.APIKey != "" {
		return client.NewAPIKeyStaticCredentials(cfg.Auth.APIKey), nil
	}

	// API key authentication (dynamic, from env var — enables rotation)
	if cfg.Auth.APIKeyEnv != "" {
		envVar := cfg.Auth.APIKeyEnv
		return client.NewAPIKeyDynamicCredentials(func(ctx context.Context) (string, error) {
			key := os.Getenv(envVar)
			if key == "" {
				return "", fmt.Errorf("environment variable %s is not set or empty", envVar)
			}
			return key, nil
		}), nil
	}

	// Bearer token from file (K8s Workload Identity / sidecar-refreshed tokens)
	if cfg.Auth.TokenFile != "" {
		filePath := cfg.Auth.TokenFile
		return client.NewAPIKeyDynamicCredentials(func(ctx context.Context) (string, error) {
			token, err := os.ReadFile(filePath)
			if err != nil {
				return "", fmt.Errorf("read token file %s: %w", filePath, err)
			}
			return strings.TrimSpace(string(token)), nil
		}), nil
	}

	// OIDC client credentials flow (Entra ID / Azure AD / any OIDC provider)
	if cfg.Auth.OIDC != nil {
		secret := cfg.Auth.OIDC.ClientSecret
		if cfg.Auth.OIDC.ClientSecretEnv != "" {
			secret = os.Getenv(cfg.Auth.OIDC.ClientSecretEnv)
			if secret == "" {
				return nil, fmt.Errorf("environment variable %s is not set or empty", cfg.Auth.OIDC.ClientSecretEnv)
			}
		}
		ccCfg := clientcredentials.Config{
			ClientID:     cfg.Auth.OIDC.ClientID,
			ClientSecret: secret,
			TokenURL:     cfg.Auth.OIDC.TokenURL,
			Scopes:       cfg.Auth.OIDC.Scopes,
		}
		tokenSource := ccCfg.TokenSource(context.Background())
		return client.NewAPIKeyDynamicCredentials(func(ctx context.Context) (string, error) {
			token, err := tokenSource.Token()
			if err != nil {
				return "", fmt.Errorf("acquire OIDC token: %w", err)
			}
			return token.AccessToken, nil
		}), nil
	}

	// mTLS authentication (client certificate)
	if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		return client.NewMTLSCredentials(cert), nil
	}

	return nil, nil
}
