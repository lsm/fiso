package link

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	data := `
listenAddr: "127.0.0.1:4000"
metricsAddr: ":9091"
targets:
  - name: crm
    protocol: https
    host: api.salesforce.com
    auth:
      type: bearer
      secretRef:
        filePath: /secrets/crm-token
    circuitBreaker:
      enabled: true
      failureThreshold: 5
      resetTimeout: "30s"
    retry:
      maxAttempts: 3
      backoff: exponential
      initialInterval: "200ms"
      maxInterval: "30s"
      jitter: 0.2
    allowedPaths:
      - /api/v2/**
asyncBrokers:
  - kafka.infra.svc:9092
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:4000" {
		t.Errorf("expected listen addr 127.0.0.1:4000, got %s", cfg.ListenAddr)
	}
	if cfg.MetricsAddr != ":9091" {
		t.Errorf("expected metrics addr :9091, got %s", cfg.MetricsAddr)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(cfg.Targets))
	}
	if cfg.Targets[0].Name != "crm" {
		t.Errorf("expected target name crm, got %s", cfg.Targets[0].Name)
	}
	if cfg.Targets[0].Auth.Type != "bearer" {
		t.Errorf("expected auth type bearer, got %s", cfg.Targets[0].Auth.Type)
	}
	if len(cfg.AsyncBrokers) != 1 {
		t.Errorf("expected 1 async broker, got %d", len(cfg.AsyncBrokers))
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	data := `
targets:
  - name: svc
    host: api.example.com
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:3500" {
		t.Errorf("expected default listen addr, got %s", cfg.ListenAddr)
	}
	if cfg.MetricsAddr != ":9090" {
		t.Errorf("expected default metrics addr, got %s", cfg.MetricsAddr)
	}
	if cfg.Targets[0].Protocol != "https" {
		t.Errorf("expected default protocol https, got %s", cfg.Targets[0].Protocol)
	}
}

func TestLoadConfig_MissingName(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	data := `
targets:
  - host: api.example.com
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for missing target name")
	}
}

func TestLoadConfig_MissingHost(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	data := `
targets:
  - name: svc
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("{{invalid"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadConfig_NonexistentFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestTargetStore_GetAndUpdate(t *testing.T) {
	targets := []LinkTarget{
		{Name: "a", Host: "a.example.com"},
		{Name: "b", Host: "b.example.com"},
	}
	store := NewTargetStore(targets)

	a := store.Get("a")
	if a == nil || a.Host != "a.example.com" {
		t.Errorf("expected target a, got %v", a)
	}

	if store.Get("c") != nil {
		t.Error("expected nil for nonexistent target")
	}

	names := store.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}

	// Update
	store.Update([]LinkTarget{{Name: "c", Host: "c.example.com"}})
	if store.Get("a") != nil {
		t.Error("expected nil for removed target a")
	}
	c := store.Get("c")
	if c == nil || c.Host != "c.example.com" {
		t.Errorf("expected target c, got %v", c)
	}
}
