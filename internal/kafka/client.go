package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
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

	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s", auth.Mechanism)
	}

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
