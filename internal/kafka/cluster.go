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
	Mechanism string `yaml:"mechanism"` // PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
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
		errs = append(errs, errors.New("brokers are required"))
	}

	if c.Auth.Mechanism != "" {
		validMechanisms := map[string]bool{
			"PLAIN":         true,
			"SCRAM-SHA-256": true,
			"SCRAM-SHA-512": true,
		}
		if !validMechanisms[c.Auth.Mechanism] {
			errs = append(errs, fmt.Errorf("auth.mechanism %q is not valid (must be PLAIN, SCRAM-SHA-256, or SCRAM-SHA-512)", c.Auth.Mechanism))
		}
		if c.Auth.Username == "" {
			errs = append(errs, errors.New("auth.username is required when mechanism is set"))
		}
		if c.Auth.Password == "" {
			errs = append(errs, errors.New("auth.password is required when mechanism is set"))
		}
	}

	if c.TLS.CertFile != "" && c.TLS.KeyFile == "" {
		errs = append(errs, errors.New("tls.keyFile is required when certFile is specified"))
	}
	if c.TLS.KeyFile != "" && c.TLS.CertFile == "" {
		errs = append(errs, errors.New("tls.certFile is required when keyFile is specified"))
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
