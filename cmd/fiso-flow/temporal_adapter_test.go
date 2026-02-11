package main

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

	temporalsink "github.com/lsm/fiso/internal/sink/temporal"
)

// generateTestClientCert generates a self-signed client certificate for testing.
// Returns PEM-encoded certificate bytes, certificate file path, and key file path.
func generateTestClientCert(t *testing.T) ([]byte, string, string) {
	t.Helper()

	// Generate RSA key pair
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Client"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// Create self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	// Encode certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	// Encode private key to PEM
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	// Write to temp files
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "client.crt")
	keyFile := filepath.Join(tmpDir, "client.key")

	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	return certPEM, certFile, keyFile
}

func TestBuildTemporalCredentials(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) temporalsink.Config
		wantCreds bool // true if credentials should be returned (non-nil)
		wantErr   string
	}{
		{
			name: "no auth configured",
			setup: func(t *testing.T) temporalsink.Config {
				return temporalsink.Config{}
			},
			wantCreds: false,
			wantErr:   "",
		},
		{
			name: "static API key",
			setup: func(t *testing.T) temporalsink.Config {
				return temporalsink.Config{
					Auth: temporalsink.AuthConfig{
						APIKey: "test-api-key-12345",
					},
				}
			},
			wantCreds: true,
			wantErr:   "",
		},
		{
			name: "dynamic API key from env var (set)",
			setup: func(t *testing.T) temporalsink.Config {
				t.Setenv("TEMPORAL_API_KEY", "dynamic-test-key")
				return temporalsink.Config{
					Auth: temporalsink.AuthConfig{
						APIKeyEnv: "TEMPORAL_API_KEY",
					},
				}
			},
			wantCreds: true,
			wantErr:   "",
		},
		{
			name: "dynamic API key from env var (empty)",
			setup: func(t *testing.T) temporalsink.Config {
				// Don't set the env var - it will be empty at call time
				return temporalsink.Config{
					Auth: temporalsink.AuthConfig{
						APIKeyEnv: "TEMPORAL_API_KEY_MISSING",
					},
				}
			},
			wantCreds: true, // Credentials object is created, error happens at call time
			wantErr:   "",
		},
		{
			name: "token file (valid)",
			setup: func(t *testing.T) temporalsink.Config {
				tmpDir := t.TempDir()
				tokenFile := filepath.Join(tmpDir, "token.txt")
				if err := os.WriteFile(tokenFile, []byte("my-bearer-token\n"), 0600); err != nil {
					t.Fatalf("failed to write token file: %v", err)
				}
				return temporalsink.Config{
					Auth: temporalsink.AuthConfig{
						TokenFile: tokenFile,
					},
				}
			},
			wantCreds: true,
			wantErr:   "",
		},
		{
			name: "token file (nonexistent)",
			setup: func(t *testing.T) temporalsink.Config {
				return temporalsink.Config{
					Auth: temporalsink.AuthConfig{
						TokenFile: "/nonexistent/token.txt",
					},
				}
			},
			wantCreds: true, // Credentials object is created, error happens at call time
			wantErr:   "",
		},
		{
			name: "OIDC with clientSecret",
			setup: func(t *testing.T) temporalsink.Config {
				return temporalsink.Config{
					Auth: temporalsink.AuthConfig{
						OIDC: &temporalsink.OIDCConfig{
							TokenURL:     "https://login.example.com/oauth2/token",
							ClientID:     "test-client-id",
							ClientSecret: "test-client-secret",
							Scopes:       []string{"temporal:read", "temporal:write"},
						},
					},
				}
			},
			wantCreds: true,
			wantErr:   "",
		},
		{
			name: "OIDC with clientSecretEnv (set)",
			setup: func(t *testing.T) temporalsink.Config {
				t.Setenv("OIDC_CLIENT_SECRET", "secret-from-env")
				return temporalsink.Config{
					Auth: temporalsink.AuthConfig{
						OIDC: &temporalsink.OIDCConfig{
							TokenURL:        "https://login.example.com/oauth2/token",
							ClientID:        "test-client-id",
							ClientSecretEnv: "OIDC_CLIENT_SECRET",
							Scopes:          []string{"temporal:read"},
						},
					},
				}
			},
			wantCreds: true,
			wantErr:   "",
		},
		{
			name: "OIDC with clientSecretEnv (empty)",
			setup: func(t *testing.T) temporalsink.Config {
				// Don't set the env var - should fail immediately
				return temporalsink.Config{
					Auth: temporalsink.AuthConfig{
						OIDC: &temporalsink.OIDCConfig{
							TokenURL:        "https://login.example.com/oauth2/token",
							ClientID:        "test-client-id",
							ClientSecretEnv: "OIDC_CLIENT_SECRET_MISSING",
						},
					},
				}
			},
			wantCreds: false,
			wantErr:   "not set or empty",
		},
		{
			name: "mTLS (valid certs)",
			setup: func(t *testing.T) temporalsink.Config {
				_, certFile, keyFile := generateTestClientCert(t)
				return temporalsink.Config{
					TLS: temporalsink.TLSConfig{
						CertFile: certFile,
						KeyFile:  keyFile,
					},
				}
			},
			wantCreds: true,
			wantErr:   "",
		},
		{
			name: "mTLS (invalid cert files)",
			setup: func(t *testing.T) temporalsink.Config {
				tmpDir := t.TempDir()
				certFile := filepath.Join(tmpDir, "bad.crt")
				keyFile := filepath.Join(tmpDir, "bad.key")
				if err := os.WriteFile(certFile, []byte("not a certificate"), 0600); err != nil {
					t.Fatalf("failed to write bad cert: %v", err)
				}
				if err := os.WriteFile(keyFile, []byte("not a key"), 0600); err != nil {
					t.Fatalf("failed to write bad key: %v", err)
				}
				return temporalsink.Config{
					TLS: temporalsink.TLSConfig{
						CertFile: certFile,
						KeyFile:  keyFile,
					},
				}
			},
			wantCreds: false,
			wantErr:   "load client certificate",
		},
		{
			name: "mTLS (missing key file)",
			setup: func(t *testing.T) temporalsink.Config {
				_, certFile, _ := generateTestClientCert(t)
				return temporalsink.Config{
					TLS: temporalsink.TLSConfig{
						CertFile: certFile,
						KeyFile:  "/nonexistent/key.pem",
					},
				}
			},
			wantCreds: false,
			wantErr:   "load client certificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.setup(t)
			creds, err := buildTemporalCredentials(cfg)

			// Check error expectations
			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("buildTemporalCredentials() error = nil, want error containing %q", tt.wantErr)
					return
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("buildTemporalCredentials() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
				return
			}

			// No error expected
			if err != nil {
				t.Errorf("buildTemporalCredentials() unexpected error = %v", err)
				return
			}

			// Check credentials expectations
			if tt.wantCreds && creds == nil {
				t.Error("buildTemporalCredentials() returned nil credentials, want non-nil")
			}
			if !tt.wantCreds && creds != nil {
				t.Error("buildTemporalCredentials() returned non-nil credentials, want nil")
			}
		})
	}
}
