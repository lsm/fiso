package tracing

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func TestGetConfig_Defaults(t *testing.T) {
	cfg := GetConfig("test-service")

	if cfg.Enabled {
		t.Error("expected tracing to be disabled by default")
	}
	if cfg.Endpoint != "localhost:4317" {
		t.Errorf("expected endpoint localhost:4317, got %s", cfg.Endpoint)
	}
	if cfg.ServiceName != "test-service" {
		t.Errorf("expected service name test-service, got %s", cfg.ServiceName)
	}
}

func TestGetConfig_EnabledFromEnv(t *testing.T) {
	t.Setenv("FISO_OTEL_ENABLED", "true")

	cfg := GetConfig("test-service")

	if !cfg.Enabled {
		t.Error("expected tracing to be enabled")
	}
}

func TestGetConfig_CustomEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "custom-endpoint:4317")

	cfg := GetConfig("test-service")

	if cfg.Endpoint != "custom-endpoint:4317" {
		t.Errorf("expected endpoint custom-endpoint:4317, got %s", cfg.Endpoint)
	}
}

func TestGetConfig_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		enabled bool
	}{
		{"lowercase true", "true", true},
		{"uppercase TRUE", "TRUE", true},
		{"mixed case True", "True", true},
		{"false", "false", false},
		{"empty", "", false},
		{"random", "random", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVal != "" {
				t.Setenv("FISO_OTEL_ENABLED", tt.envVal)
			}
			// When envVal is empty, we don't set the env var at all (it's unset by default)

			cfg := GetConfig("test-service")

			if cfg.Enabled != tt.enabled {
				t.Errorf("expected enabled %v, got %v", tt.enabled, cfg.Enabled)
			}
		})
	}
}

func TestInitialize_Disabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg := Config{
		Enabled:     false,
		Endpoint:    "localhost:4317",
		ServiceName: "test-service",
	}

	tracer, shutdown, err := Initialize(cfg, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tracer == nil {
		t.Error("expected non-nil tracer")
	}

	// Shutdown should be a no-op for disabled tracing
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("unexpected shutdown error: %v", err)
	}
}

func TestPropagator(t *testing.T) {
	prop := Propagator()
	if prop == nil {
		t.Error("expected non-nil propagator")
	}
}
