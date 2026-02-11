package kafka

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// generateTestCert generates a self-signed certificate for testing.
func generateTestCert(t *testing.T) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

// generateTestKeyPair generates a self-signed cert/key pair for mTLS testing.
func generateTestKeyPair(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}

func TestClientOptions_Basic(t *testing.T) {
	cfg := &ClusterConfig{
		Brokers: []string{"localhost:9092"},
	}

	opts, err := ClientOptions(cfg)
	if err != nil {
		t.Fatalf("ClientOptions() error = %v", err)
	}
	if len(opts) == 0 {
		t.Error("ClientOptions() returned empty options")
	}
}

func TestClientOptions_WithSASL(t *testing.T) {
	tests := []struct {
		name      string
		mechanism string
		wantErr   bool
	}{
		{"PLAIN", "PLAIN", false},
		{"SCRAM-SHA-256", "SCRAM-SHA-256", false},
		{"SCRAM-SHA-512", "SCRAM-SHA-512", false},
		{"unknown", "UNKNOWN", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ClusterConfig{
				Brokers: []string{"localhost:9092"},
				Auth: AuthConfig{
					Mechanism: tt.mechanism,
					Username:  "user",
					Password:  "pass",
				},
			}

			opts, err := ClientOptions(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ClientOptions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(opts) < 2 {
				t.Error("ClientOptions() should include SASL option")
			}
		})
	}
}

func TestClientOptions_WithTLS(t *testing.T) {
	cfg := &ClusterConfig{
		Brokers: []string{"localhost:9092"},
		TLS: TLSConfig{
			Enabled:    true,
			SkipVerify: true,
		},
	}

	opts, err := ClientOptions(cfg)
	if err != nil {
		t.Fatalf("ClientOptions() error = %v", err)
	}
	if len(opts) < 2 {
		t.Error("ClientOptions() should include TLS option")
	}
}

func TestClientOptions_TLSWithCA(t *testing.T) {
	// Create temp CA file with a valid self-signed certificate
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")

	// Generate a valid self-signed certificate for testing
	caPEM := generateTestCert(t)
	if err := os.WriteFile(caFile, caPEM, 0600); err != nil {
		t.Fatalf("Failed to write CA file: %v", err)
	}

	cfg := &ClusterConfig{
		Brokers: []string{"localhost:9092"},
		TLS: TLSConfig{
			Enabled: true,
			CAFile:  caFile,
		},
	}

	opts, err := ClientOptions(cfg)
	if err != nil {
		t.Fatalf("ClientOptions() error = %v", err)
	}
	if len(opts) < 2 {
		t.Error("ClientOptions() should include TLS option with CA")
	}
}

func TestClientOptions_TLSWithInvalidCA(t *testing.T) {
	cfg := &ClusterConfig{
		Brokers: []string{"localhost:9092"},
		TLS: TLSConfig{
			Enabled: true,
			CAFile:  "/nonexistent/ca.pem",
		},
	}

	_, err := ClientOptions(cfg)
	if err == nil {
		t.Error("ClientOptions() should fail with nonexistent CA file")
	}
}

func TestClientOptions_TLSWithInvalidCAPEM(t *testing.T) {
	// Create temp file with invalid PEM
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "bad-ca.pem")
	if err := os.WriteFile(caFile, []byte("not a valid certificate"), 0600); err != nil {
		t.Fatalf("Failed to write CA file: %v", err)
	}

	cfg := &ClusterConfig{
		Brokers: []string{"localhost:9092"},
		TLS: TLSConfig{
			Enabled: true,
			CAFile:  caFile,
		},
	}

	_, err := ClientOptions(cfg)
	if err == nil {
		t.Error("ClientOptions() should fail with invalid CA PEM")
	}
}

func TestSaslOption_AllMechanisms(t *testing.T) {
	tests := []struct {
		name      string
		mechanism string
		wantErr   bool
	}{
		{"PLAIN", "PLAIN", false},
		{"SCRAM-SHA-256", "SCRAM-SHA-256", false},
		{"SCRAM-SHA-512", "SCRAM-SHA-512", false},
		{"unknown", "GSSAPI", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := AuthConfig{
				Mechanism: tt.mechanism,
				Username:  "testuser",
				Password:  "testpass",
			}

			opt, err := saslOption(auth)
			if (err != nil) != tt.wantErr {
				t.Errorf("saslOption() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && opt == nil {
				t.Error("saslOption() returned nil option")
			}
		})
	}
}

func TestSaslOption_OAUTHBEARER(t *testing.T) {
	t.Run("missing oauth config", func(t *testing.T) {
		auth := AuthConfig{
			Mechanism: "OAUTHBEARER",
		}
		_, err := saslOption(auth)
		if err == nil {
			t.Fatal("expected error for missing oauth config")
		}
		if !strings.Contains(err.Error(), "oauth config required") {
			t.Errorf("expected 'oauth config required' error, got: %v", err)
		}
	})

	t.Run("unsupported provider", func(t *testing.T) {
		auth := AuthConfig{
			Mechanism: "OAUTHBEARER",
			OAuth: &OAuthConfig{
				Provider: "unsupported",
			},
		}
		_, err := saslOption(auth)
		if err == nil {
			t.Fatal("expected error for unsupported provider")
		}
		if !strings.Contains(err.Error(), "unsupported oauth provider") {
			t.Errorf("expected 'unsupported oauth provider' error, got: %v", err)
		}
	})

	t.Run("azure missing client secret env var", func(t *testing.T) {
		auth := AuthConfig{
			Mechanism: "OAUTHBEARER",
			OAuth: &OAuthConfig{
				Provider:        "azure",
				TenantID:        "tenant-123",
				ClientID:        "client-123",
				ClientSecretEnv: "NONEXISTENT_SECRET_VAR",
				Scope:           "api://kafka/.default",
			},
		}
		_, err := saslOption(auth)
		if err == nil {
			t.Fatal("expected error for missing environment variable")
		}
		if !strings.Contains(err.Error(), "not set or empty") {
			t.Errorf("expected 'not set or empty' error, got: %v", err)
		}
	})

	t.Run("azure valid config with env var", func(t *testing.T) {
		// Set test environment variable
		envVar := "TEST_KAFKA_OAUTH_SECRET"
		t.Setenv(envVar, "test-secret-value")

		auth := AuthConfig{
			Mechanism: "OAUTHBEARER",
			OAuth: &OAuthConfig{
				Provider:        "azure",
				TenantID:        "tenant-123",
				ClientID:        "client-123",
				ClientSecretEnv: envVar,
				Scope:           "api://kafka/.default",
			},
		}

		opt, err := saslOption(auth)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opt == nil {
			t.Fatal("expected non-nil option")
		}
	})
}

func TestBuildTLSConfig_Basic(t *testing.T) {
	cfg := TLSConfig{
		Enabled:    true,
		SkipVerify: true,
	}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig() error = %v", err)
	}
	if !tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true")
	}
}

func TestBuildTLSConfig_WithMTLS(t *testing.T) {
	// Create temp cert and key files
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")

	// Generate a valid cert/key pair for testing
	certPEM, keyPEM := generateTestKeyPair(t)

	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatalf("Failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("Failed to write key file: %v", err)
	}

	cfg := TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig() error = %v", err)
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Error("buildTLSConfig() should load client certificate")
	}
}

func TestBuildTLSConfig_WithInvalidCertPath(t *testing.T) {
	cfg := TLSConfig{
		Enabled:  true,
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}

	_, err := buildTLSConfig(cfg)
	if err == nil {
		t.Error("buildTLSConfig() should fail with nonexistent cert/key files")
	}
}
