package link

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsm/fiso/internal/kafka"
)

func TestLoadConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	data := `
listenAddr: "127.0.0.1:4000"
metricsAddr: ":9091"
kafka:
  clusters:
    main:
      brokers:
        - kafka.infra.svc:9092
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
	if len(cfg.Kafka.Clusters) != 1 {
		t.Errorf("expected 1 kafka cluster, got %d", len(cfg.Kafka.Clusters))
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

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "valid config",
			cfg:  Config{Targets: []LinkTarget{{Name: "svc", Host: "api.example.com", Protocol: "https"}}},
		},
		{
			name:    "missing name",
			cfg:     Config{Targets: []LinkTarget{{Host: "api.example.com"}}},
			wantErr: "name is required",
		},
		{
			name:    "missing host",
			cfg:     Config{Targets: []LinkTarget{{Name: "svc", Protocol: "https"}}},
			wantErr: "host is required",
		},
		{
			name:    "invalid protocol",
			cfg:     Config{Targets: []LinkTarget{{Name: "svc", Host: "api.example.com", Protocol: "ftp"}}},
			wantErr: "protocol \"ftp\" is not valid",
		},
		{
			name: "invalid reset timeout",
			cfg: Config{Targets: []LinkTarget{{
				Name: "svc", Host: "api.example.com",
				CircuitBreaker: CircuitBreakerConfig{ResetTimeout: "not-a-duration"},
			}}},
			wantErr: "resetTimeout",
		},
		{
			name: "invalid initial interval",
			cfg: Config{Targets: []LinkTarget{{
				Name: "svc", Host: "api.example.com",
				Retry: RetryConfig{InitialInterval: "bad"},
			}}},
			wantErr: "initialInterval",
		},
		{
			name: "invalid max interval",
			cfg: Config{Targets: []LinkTarget{{
				Name: "svc", Host: "api.example.com",
				Retry: RetryConfig{MaxInterval: "bad"},
			}}},
			wantErr: "maxInterval",
		},
		{
			name: "jitter too high",
			cfg: Config{Targets: []LinkTarget{{
				Name: "svc", Host: "api.example.com",
				Retry: RetryConfig{Jitter: 1.5},
			}}},
			wantErr: "jitter must be between",
		},
		{
			name: "negative jitter",
			cfg: Config{Targets: []LinkTarget{{
				Name: "svc", Host: "api.example.com",
				Retry: RetryConfig{Jitter: -0.1},
			}}},
			wantErr: "jitter must be between",
		},
		{
			name: "negative rate limit",
			cfg: Config{Targets: []LinkTarget{{
				Name: "svc", Host: "api.example.com",
				RateLimit: RateLimitConfig{RequestsPerSecond: -1},
			}}},
			wantErr: "requestsPerSecond must be >= 0",
		},
		{
			name: "negative burst",
			cfg: Config{Targets: []LinkTarget{{
				Name: "svc", Host: "api.example.com",
				RateLimit: RateLimitConfig{Burst: -1},
			}}},
			wantErr: "burst must be >= 0",
		},
		{
			name: "valid durations",
			cfg: Config{Targets: []LinkTarget{{
				Name: "svc", Host: "api.example.com",
				CircuitBreaker: CircuitBreakerConfig{ResetTimeout: "30s"},
				Retry:          RetryConfig{InitialInterval: "200ms", MaxInterval: "30s", Jitter: 0.2},
			}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
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

func TestLoadConfig_IntegerDurations(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	data := `
targets:
  - name: svc
    host: api.example.com
    circuitBreaker:
      enabled: true
      resetTimeout: 30000
    retry:
      maxAttempts: 3
      backoff: exponential
      initialInterval: 200
      maxInterval: 30000
      jitter: 0.2
    rateLimit:
      requestsPerSecond: 100
      burst: 50
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that integer durations were converted to string with "ms" suffix
	if cfg.Targets[0].CircuitBreaker.ResetTimeout != "30000ms" {
		t.Errorf("expected resetTimeout \"30000ms\", got %q", cfg.Targets[0].CircuitBreaker.ResetTimeout)
	}
	if cfg.Targets[0].Retry.InitialInterval != "200ms" {
		t.Errorf("expected initialInterval \"200ms\", got %q", cfg.Targets[0].Retry.InitialInterval)
	}
	if cfg.Targets[0].Retry.MaxInterval != "30000ms" {
		t.Errorf("expected maxInterval \"30000ms\", got %q", cfg.Targets[0].Retry.MaxInterval)
	}
	// Check that int RequestsPerSecond was converted to float64
	if cfg.Targets[0].RateLimit.RequestsPerSecond != 100.0 {
		t.Errorf("expected requestsPerSecond 100.0, got %f", cfg.Targets[0].RateLimit.RequestsPerSecond)
	}
}

func TestLoadConfig_FloatDurations(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	data := `
targets:
  - name: svc
    host: api.example.com
    circuitBreaker:
      enabled: true
      resetTimeout: 30000.0
    retry:
      maxAttempts: 3
      backoff: exponential
      initialInterval: 200.5
      maxInterval: 30000.0
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that float durations were converted to string with "ms" suffix
	if cfg.Targets[0].CircuitBreaker.ResetTimeout != "30000ms" {
		t.Errorf("expected resetTimeout \"30000ms\", got %q", cfg.Targets[0].CircuitBreaker.ResetTimeout)
	}
	if cfg.Targets[0].Retry.InitialInterval != "200ms" {
		t.Errorf("expected initialInterval \"200ms\", got %q", cfg.Targets[0].Retry.InitialInterval)
	}
}

func TestRateLimitConfig_UnmarshalYAML_Int64(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test with very large numbers that would be int64
	data := `
targets:
  - name: svc
    host: api.example.com
    rateLimit:
      requestsPerSecond: 9223372036854775807
      burst: 9223372036854775807
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify int64 values were handled correctly
	if cfg.Targets[0].RateLimit.RequestsPerSecond == 0 {
		t.Error("expected non-zero requestsPerSecond")
	}
	if cfg.Targets[0].RateLimit.Burst == 0 {
		t.Error("expected non-zero burst")
	}
}

func TestCircuitBreakerConfig_UnmarshalYAML_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Invalid YAML structure for circuitBreaker
	data := `
targets:
  - name: svc
    host: api.example.com
    circuitBreaker: "invalid-string-value"
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid circuitBreaker YAML")
	}
	if !strings.Contains(err.Error(), "decode circuitBreaker") {
		t.Errorf("expected decode error, got: %v", err)
	}
}

func TestRetryConfig_UnmarshalYAML_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Invalid YAML structure for retry
	data := `
targets:
  - name: svc
    host: api.example.com
    retry: "invalid-string-value"
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid retry YAML")
	}
	if !strings.Contains(err.Error(), "decode retry") {
		t.Errorf("expected decode error, got: %v", err)
	}
}

func TestRateLimitConfig_UnmarshalYAML_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Invalid YAML structure for rateLimit
	data := `
targets:
  - name: svc
    host: api.example.com
    rateLimit: "invalid-string-value"
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid rateLimit YAML")
	}
	if !strings.Contains(err.Error(), "decode rateLimit") {
		t.Errorf("expected decode error, got: %v", err)
	}
}

func TestConfig_Validate_KafkaProtocol(t *testing.T) {
	// Helper to create a config with kafka cluster
	kafkaGlobal := kafka.KafkaGlobalConfig{
		Clusters: map[string]kafka.ClusterConfig{
			"main": {Brokers: []string{"localhost:9092"}},
		},
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "kafka without topic",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka:    &KafkaConfig{Cluster: "main"},
				}},
			},
			wantErr: "requires topic",
		},
		{
			name: "kafka with empty topic",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka:    &KafkaConfig{Cluster: "main", Topic: ""},
				}},
			},
			wantErr: "requires topic",
		},
		{
			name: "kafka with host specified",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Host:     "kafka.example.com",
					Kafka:    &KafkaConfig{Cluster: "main", Topic: "events"},
				}},
			},
			wantErr: "does not use host field",
		},
		{
			name: "kafka without cluster reference",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka:    &KafkaConfig{Topic: "events"},
				}},
			},
			wantErr: "requires cluster reference",
		},
		{
			name: "kafka valid config",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka:    &KafkaConfig{Cluster: "main", Topic: "events"},
				}},
			},
		},
		{
			name: "kafka with invalid key type",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka: &KafkaConfig{
						Cluster: "main",
						Topic:   "events",
						Key:     KeyStrategy{Type: "invalid"},
					},
				}},
			},
			wantErr: "invalid key type",
		},
		{
			name: "kafka with header key type missing field",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka: &KafkaConfig{
						Cluster: "main",
						Topic:   "events",
						Key:     KeyStrategy{Type: "header"},
					},
				}},
			},
			wantErr: "requires field parameter",
		},
		{
			name: "kafka with payload key type missing field",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka: &KafkaConfig{
						Cluster: "main",
						Topic:   "events",
						Key:     KeyStrategy{Type: "payload"},
					},
				}},
			},
			wantErr: "requires field parameter",
		},
		{
			name: "kafka with static key type missing value",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka: &KafkaConfig{
						Cluster: "main",
						Topic:   "events",
						Key:     KeyStrategy{Type: "static"},
					},
				}},
			},
			wantErr: "requires value parameter",
		},
		{
			name: "kafka with valid uuid key type",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka: &KafkaConfig{
						Cluster: "main",
						Topic:   "events",
						Key:     KeyStrategy{Type: "uuid"},
					},
				}},
			},
		},
		{
			name: "kafka with valid header key type",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka: &KafkaConfig{
						Cluster: "main",
						Topic:   "events",
						Key:     KeyStrategy{Type: "header", Field: "X-Request-ID"},
					},
				}},
			},
		},
		{
			name: "kafka with valid payload key type",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka: &KafkaConfig{
						Cluster: "main",
						Topic:   "events",
						Key:     KeyStrategy{Type: "payload", Field: "userId"},
					},
				}},
			},
		},
		{
			name: "kafka with valid static key type",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka: &KafkaConfig{
						Cluster: "main",
						Topic:   "events",
						Key:     KeyStrategy{Type: "static", Value: "partition-key"},
					},
				}},
			},
		},
		{
			name: "kafka with valid random key type",
			cfg: Config{
				Kafka: kafkaGlobal,
				Targets: []LinkTarget{{
					Name:     "kafka-target",
					Protocol: "kafka",
					Kafka: &KafkaConfig{
						Cluster: "main",
						Topic:   "events",
						Key:     KeyStrategy{Type: "random"},
					},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadConfig_KafkaProtocol(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	data := `
listenAddr: "127.0.0.1:4000"
metricsAddr: ":9091"
kafka:
  clusters:
    main:
      brokers:
        - kafka.infra.svc:9092
targets:
  - name: events
    protocol: kafka
    kafka:
      cluster: main
      topic: user-events
      key:
        type: uuid
      headers:
        source: fiso-link
        version: "1.0"
      requiredAcks: all
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(cfg.Targets))
	}
	if cfg.Targets[0].Protocol != "kafka" {
		t.Errorf("expected protocol kafka, got %s", cfg.Targets[0].Protocol)
	}
	if cfg.Targets[0].Kafka == nil {
		t.Fatal("expected kafka config to be set")
	}
	if cfg.Targets[0].Kafka.Topic != "user-events" {
		t.Errorf("expected topic user-events, got %s", cfg.Targets[0].Kafka.Topic)
	}
	if cfg.Targets[0].Kafka.Key.Type != "uuid" {
		t.Errorf("expected key type uuid, got %s", cfg.Targets[0].Kafka.Key.Type)
	}
	if cfg.Targets[0].Kafka.RequiredAcks != "all" {
		t.Errorf("expected requiredAcks all, got %s", cfg.Targets[0].Kafka.RequiredAcks)
	}
	if len(cfg.Kafka.Clusters) != 1 {
		t.Errorf("expected 1 kafka cluster, got %d", len(cfg.Kafka.Clusters))
	}
}

func TestCircuitBreakerConfig_UnmarshalYAML_NonBoolEnabled(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test non-bool value for enabled field (should be ignored)
	data := `
targets:
  - name: svc
    host: api.example.com
    circuitBreaker:
      enabled: 123
      failureThreshold: 5
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-bool enabled should default to false
	if cfg.Targets[0].CircuitBreaker.Enabled {
		t.Error("expected enabled to be false for non-bool value")
	}
	if cfg.Targets[0].CircuitBreaker.FailureThreshold != 5 {
		t.Errorf("expected failureThreshold 5, got %d", cfg.Targets[0].CircuitBreaker.FailureThreshold)
	}
}

func TestRetryConfig_UnmarshalYAML_IntJitter(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test int value for jitter (should be converted to float64)
	data := `
targets:
  - name: svc
    host: api.example.com
    retry:
      maxAttempts: 3
      jitter: 1
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Targets[0].Retry.Jitter != 1.0 {
		t.Errorf("expected jitter 1.0, got %f", cfg.Targets[0].Retry.Jitter)
	}
}

func TestRetryConfig_UnmarshalYAML_NonStringBackoff(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test non-string value for backoff (should be ignored)
	data := `
targets:
  - name: svc
    host: api.example.com
    retry:
      maxAttempts: 3
      backoff: 123
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-string backoff should remain empty
	if cfg.Targets[0].Retry.Backoff != "" {
		t.Errorf("expected empty backoff for non-string value, got %q", cfg.Targets[0].Retry.Backoff)
	}
}

func TestCircuitBreakerConfig_UnmarshalYAML_AllTypes(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test all field types for circuit breaker
	data := `
targets:
  - name: svc1
    host: api.example.com
    circuitBreaker:
      enabled: true
      failureThreshold: 5.0
      successThreshold: 3.0
      resetTimeout: 30000.0
  - name: svc2
    host: api2.example.com
    circuitBreaker:
      enabled: false
      failureThreshold: 10
      successThreshold: 2
      resetTimeout: 5000
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check float64 conversions for thresholds
	if cfg.Targets[0].CircuitBreaker.FailureThreshold != 5 {
		t.Errorf("expected failureThreshold 5, got %d", cfg.Targets[0].CircuitBreaker.FailureThreshold)
	}
	if cfg.Targets[0].CircuitBreaker.SuccessThreshold != 3 {
		t.Errorf("expected successThreshold 3, got %d", cfg.Targets[0].CircuitBreaker.SuccessThreshold)
	}
	if cfg.Targets[0].CircuitBreaker.ResetTimeout != "30000ms" {
		t.Errorf("expected resetTimeout \"30000ms\", got %q", cfg.Targets[0].CircuitBreaker.ResetTimeout)
	}

	// Check int conversions
	if cfg.Targets[1].CircuitBreaker.FailureThreshold != 10 {
		t.Errorf("expected failureThreshold 10, got %d", cfg.Targets[1].CircuitBreaker.FailureThreshold)
	}
	if cfg.Targets[1].CircuitBreaker.SuccessThreshold != 2 {
		t.Errorf("expected successThreshold 2, got %d", cfg.Targets[1].CircuitBreaker.SuccessThreshold)
	}
	if cfg.Targets[1].CircuitBreaker.ResetTimeout != "5000ms" {
		t.Errorf("expected resetTimeout \"5000ms\", got %q", cfg.Targets[1].CircuitBreaker.ResetTimeout)
	}
}

func TestCircuitBreakerConfig_UnmarshalYAML_NonNumericThresholds(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test non-numeric values for thresholds (should default to 0)
	data := `
targets:
  - name: svc
    host: api.example.com
    circuitBreaker:
      enabled: true
      failureThreshold: "invalid"
      successThreshold: "invalid"
      resetTimeout: "30s"
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-numeric thresholds should default to 0
	if cfg.Targets[0].CircuitBreaker.FailureThreshold != 0 {
		t.Errorf("expected failureThreshold 0 for non-numeric value, got %d", cfg.Targets[0].CircuitBreaker.FailureThreshold)
	}
	if cfg.Targets[0].CircuitBreaker.SuccessThreshold != 0 {
		t.Errorf("expected successThreshold 0 for non-numeric value, got %d", cfg.Targets[0].CircuitBreaker.SuccessThreshold)
	}
}

func TestCircuitBreakerConfig_UnmarshalYAML_NonStringResetTimeout(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test non-string, non-numeric resetTimeout (should default to empty)
	data := `
targets:
  - name: svc
    host: api.example.com
    circuitBreaker:
      enabled: true
      resetTimeout: []
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-string, non-numeric resetTimeout should remain empty
	if cfg.Targets[0].CircuitBreaker.ResetTimeout != "" {
		t.Errorf("expected empty resetTimeout for invalid type, got %q", cfg.Targets[0].CircuitBreaker.ResetTimeout)
	}
}

func TestRateLimitConfig_UnmarshalYAML_AllTypes(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test all field types for rate limit
	data := `
targets:
  - name: svc1
    host: api.example.com
    rateLimit:
      requestsPerSecond: 100
      burst: 50
  - name: svc2
    host: api2.example.com
    rateLimit:
      requestsPerSecond: 100.5
      burst: 50.8
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check int conversions
	if cfg.Targets[0].RateLimit.RequestsPerSecond != 100.0 {
		t.Errorf("expected requestsPerSecond 100.0, got %f", cfg.Targets[0].RateLimit.RequestsPerSecond)
	}
	if cfg.Targets[0].RateLimit.Burst != 50 {
		t.Errorf("expected burst 50, got %d", cfg.Targets[0].RateLimit.Burst)
	}

	// Check float64 conversions
	if cfg.Targets[1].RateLimit.RequestsPerSecond != 100.5 {
		t.Errorf("expected requestsPerSecond 100.5, got %f", cfg.Targets[1].RateLimit.RequestsPerSecond)
	}
	if cfg.Targets[1].RateLimit.Burst != 50 {
		t.Errorf("expected burst 50, got %d", cfg.Targets[1].RateLimit.Burst)
	}
}

func TestRateLimitConfig_UnmarshalYAML_NonNumericValues(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test non-numeric values (should default to 0)
	data := `
targets:
  - name: svc
    host: api.example.com
    rateLimit:
      requestsPerSecond: "invalid"
      burst: "invalid"
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-numeric values should default to 0
	if cfg.Targets[0].RateLimit.RequestsPerSecond != 0 {
		t.Errorf("expected requestsPerSecond 0 for non-numeric value, got %f", cfg.Targets[0].RateLimit.RequestsPerSecond)
	}
	if cfg.Targets[0].RateLimit.Burst != 0 {
		t.Errorf("expected burst 0 for non-numeric value, got %d", cfg.Targets[0].RateLimit.Burst)
	}
}

func TestRetryConfig_UnmarshalYAML_AllTypes(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test all field types for retry
	data := `
targets:
  - name: svc1
    host: api.example.com
    retry:
      maxAttempts: 3.0
      backoff: exponential
      initialInterval: "200ms"
      maxInterval: 30000
      jitter: 0.2
  - name: svc2
    host: api2.example.com
    retry:
      maxAttempts: 5
      backoff: constant
      initialInterval: 100.5
      maxInterval: "5s"
      jitter: 1
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check float64 conversion for maxAttempts
	if cfg.Targets[0].Retry.MaxAttempts != 3 {
		t.Errorf("expected maxAttempts 3, got %d", cfg.Targets[0].Retry.MaxAttempts)
	}
	if cfg.Targets[0].Retry.MaxInterval != "30000ms" {
		t.Errorf("expected maxInterval \"30000ms\", got %q", cfg.Targets[0].Retry.MaxInterval)
	}

	// Check int conversion for maxAttempts
	if cfg.Targets[1].Retry.MaxAttempts != 5 {
		t.Errorf("expected maxAttempts 5, got %d", cfg.Targets[1].Retry.MaxAttempts)
	}
	if cfg.Targets[1].Retry.InitialInterval != "100ms" {
		t.Errorf("expected initialInterval \"100ms\", got %q", cfg.Targets[1].Retry.InitialInterval)
	}
	if cfg.Targets[1].Retry.Jitter != 1.0 {
		t.Errorf("expected jitter 1.0, got %f", cfg.Targets[1].Retry.Jitter)
	}
}

func TestRetryConfig_UnmarshalYAML_NonNumericMaxAttempts(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test non-numeric value for maxAttempts (should default to 0)
	data := `
targets:
  - name: svc
    host: api.example.com
    retry:
      maxAttempts: "invalid"
      backoff: exponential
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-numeric maxAttempts should default to 0
	if cfg.Targets[0].Retry.MaxAttempts != 0 {
		t.Errorf("expected maxAttempts 0 for non-numeric value, got %d", cfg.Targets[0].Retry.MaxAttempts)
	}
}

func TestRetryConfig_UnmarshalYAML_NonNumericIntervals(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test non-numeric, non-string intervals (should default to empty)
	data := `
targets:
  - name: svc
    host: api.example.com
    retry:
      maxAttempts: 3
      initialInterval: []
      maxInterval: []
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-numeric, non-string intervals should remain empty
	if cfg.Targets[0].Retry.InitialInterval != "" {
		t.Errorf("expected empty initialInterval for invalid type, got %q", cfg.Targets[0].Retry.InitialInterval)
	}
	if cfg.Targets[0].Retry.MaxInterval != "" {
		t.Errorf("expected empty maxInterval for invalid type, got %q", cfg.Targets[0].Retry.MaxInterval)
	}
}

func TestRetryConfig_UnmarshalYAML_NonNumericJitter(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	// Test non-numeric value for jitter (should default to 0)
	data := `
targets:
  - name: svc
    host: api.example.com
    retry:
      maxAttempts: 3
      jitter: "invalid"
`
	if err := os.WriteFile(cfgFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-numeric jitter should default to 0
	if cfg.Targets[0].Retry.Jitter != 0.0 {
		t.Errorf("expected jitter 0.0 for non-numeric value, got %f", cfg.Targets[0].Retry.Jitter)
	}
}
