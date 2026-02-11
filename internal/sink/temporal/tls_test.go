package temporal

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateTestCert generates a self-signed CA certificate for testing.
func generateTestCert(t *testing.T) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
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
		t.Fatalf("failed to create certificate: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

func TestBuildTLSConfig_Minimal(t *testing.T) {
	cfg := TLSConfig{
		Enabled: true,
	}

	tlsCfg, err := BuildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("BuildTLSConfig() error = %v", err)
	}
	if tlsCfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %v, want TLS 1.2", tlsCfg.MinVersion)
	}
	if tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be false by default")
	}
	if len(tlsCfg.Certificates) > 0 {
		t.Error("should not include any client certificates")
	}
}

func TestBuildTLSConfig_WithCA(t *testing.T) {
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "ca.pem")

	caPEM := generateTestCert(t)
	if err := os.WriteFile(caFile, caPEM, 0600); err != nil {
		t.Fatalf("failed to write CA file: %v", err)
	}

	cfg := TLSConfig{
		Enabled: true,
		CAFile:  caFile,
	}

	tlsCfg, err := BuildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("BuildTLSConfig() error = %v", err)
	}
	if tlsCfg.RootCAs == nil {
		t.Error("RootCAs should be set when CA file is provided")
	}
}

func TestBuildTLSConfig_WithSkipVerify(t *testing.T) {
	cfg := TLSConfig{
		Enabled:    true,
		SkipVerify: true,
	}

	tlsCfg, err := BuildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("BuildTLSConfig() error = %v", err)
	}
	if !tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true when skipVerify is set")
	}
}

func TestBuildTLSConfig_MissingCAFile(t *testing.T) {
	cfg := TLSConfig{
		Enabled: true,
		CAFile:  "/nonexistent/ca.crt",
	}

	_, err := BuildTLSConfig(cfg)
	if err == nil {
		t.Fatal("BuildTLSConfig() should fail with missing CA file")
	}
}

func TestBuildTLSConfig_InvalidCAContent(t *testing.T) {
	tmpDir := t.TempDir()
	badCAFile := filepath.Join(tmpDir, "bad-ca.crt")

	if err := os.WriteFile(badCAFile, []byte("not a certificate"), 0600); err != nil {
		t.Fatalf("failed to write bad CA file: %v", err)
	}

	cfg := TLSConfig{
		Enabled: true,
		CAFile:  badCAFile,
	}

	_, err := BuildTLSConfig(cfg)
	if err == nil {
		t.Fatal("BuildTLSConfig() should fail with invalid CA content")
	}
}

func TestBuildTLSConfig_NeverIncludesClientCerts(t *testing.T) {
	// Even if CertFile and KeyFile are set in TLSConfig,
	// BuildTLSConfig must NOT load them — they go through
	// client.NewMTLSCredentials() separately.
	cfg := TLSConfig{
		Enabled:  true,
		CertFile: "/some/cert.pem",
		KeyFile:  "/some/key.pem",
	}

	tlsCfg, err := BuildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("BuildTLSConfig() error = %v", err)
	}
	if len(tlsCfg.Certificates) > 0 {
		t.Error("BuildTLSConfig() must not include client certificates")
	}
}
