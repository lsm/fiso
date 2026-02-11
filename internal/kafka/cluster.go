// Package kafka provides shared Kafka cluster configuration and connection management.
package kafka

import (
	"errors"
	"fmt"
)

// ClusterConfig defines a Kafka cluster with authentication and TLS settings.
type ClusterConfig struct {
	Name    string     `yaml:"name,omitempty"` // Populated from map key when loaded
	Brokers []string   `yaml:"brokers"`
	Auth    AuthConfig `yaml:"auth,omitempty"`
	TLS     TLSConfig  `yaml:"tls,omitempty"`
}

// AuthConfig defines SASL authentication for Kafka.
type AuthConfig struct {
	Mechanism string       `yaml:"mechanism"` // PLAIN, SCRAM-SHA-256, SCRAM-SHA-512, OAUTHBEARER
	Username  string       `yaml:"username"`
	Password  string       `yaml:"password"`
	OAuth     *OAuthConfig `yaml:"oauth,omitempty"` // For OAUTHBEARER
}

// OAuthConfig defines OAuth/OIDC authentication settings for OAUTHBEARER.
type OAuthConfig struct {
	Provider        string            `yaml:"provider"` // "azure" (future: "custom")
	TenantID        string            `yaml:"tenantId,omitempty"`
	ClientID        string            `yaml:"clientId"`
	ClientSecretEnv string            `yaml:"clientSecretEnv,omitempty"` // Env var name
	Scope           string            `yaml:"scope"`
	Extensions      map[string]string `yaml:"extensions,omitempty"` // For Confluent Cloud
}

// TLSConfig defines TLS settings for Kafka connections.
type TLSConfig struct {
	Enabled    bool   `yaml:"enabled"`
	CAFile     string `yaml:"caFile,omitempty"`
	CertFile   string `yaml:"certFile,omitempty"` // For mTLS
	KeyFile    string `yaml:"keyFile,omitempty"`  // For mTLS
	SkipVerify bool   `yaml:"skipVerify,omitempty"`
}

// Validate checks the cluster configuration for errors.
func (c *ClusterConfig) Validate() error {
	var errs []error

	if len(c.Brokers) == 0 {
		errs = append(errs, fmt.Errorf("at least one broker is required"))
	}

	// Validate SASL mechanism
	if c.Auth.Mechanism != "" {
		validMechanisms := map[string]bool{
			"PLAIN":         true,
			"SCRAM-SHA-256": true,
			"SCRAM-SHA-512": true,
			"OAUTHBEARER":   true,
		}
		if !validMechanisms[c.Auth.Mechanism] {
			errs = append(errs, fmt.Errorf("unsupported SASL mechanism: %s (valid: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512, OAUTHBEARER)", c.Auth.Mechanism))
		}

		// Validate credentials based on mechanism
		if c.Auth.Mechanism == "OAUTHBEARER" {
			if c.Auth.OAuth == nil {
				errs = append(errs, fmt.Errorf("oauth config is required for OAUTHBEARER mechanism"))
			} else {
				// Validate OAuth config
				if c.Auth.OAuth.Provider == "" {
					errs = append(errs, fmt.Errorf("oauth.provider is required (e.g., \"azure\")"))
				}
				if c.Auth.OAuth.ClientID == "" {
					errs = append(errs, fmt.Errorf("oauth.clientId is required"))
				}
				if c.Auth.OAuth.Scope == "" {
					errs = append(errs, fmt.Errorf("oauth.scope is required"))
				}
				if c.Auth.OAuth.Provider == "azure" && c.Auth.OAuth.TenantID == "" {
					errs = append(errs, fmt.Errorf("oauth.tenantId is required for Azure provider"))
				}
			}
		} else {
			// Username/password required for PLAIN and SCRAM
			if c.Auth.Username == "" {
				errs = append(errs, fmt.Errorf("username is required when mechanism is %s", c.Auth.Mechanism))
			}
			if c.Auth.Password == "" {
				errs = append(errs, fmt.Errorf("password is required when mechanism is %s", c.Auth.Mechanism))
			}
		}
	}

	// TLS validation (unchanged)
	if (c.TLS.CertFile != "" && c.TLS.KeyFile == "") || (c.TLS.CertFile == "" && c.TLS.KeyFile != "") {
		errs = append(errs, fmt.Errorf("both certFile and keyFile must be specified together for mTLS"))
	}

	return errors.Join(errs...)
}

// KafkaGlobalConfig holds named Kafka cluster configurations.
type KafkaGlobalConfig struct {
	Clusters map[string]ClusterConfig `yaml:"clusters"`
}

// Validate checks all cluster configurations.
func (c *KafkaGlobalConfig) Validate() error {
	var errs []error
	for name, cluster := range c.Clusters {
		if err := cluster.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("cluster %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}
