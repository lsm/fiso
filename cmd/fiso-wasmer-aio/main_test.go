//go:build wasmer

package main

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/lsm/fiso/internal/config"
	httpsource "github.com/lsm/fiso/internal/source/http"
	"go.opentelemetry.io/otel/trace/noop"
)

// TestBuildPipeline_GRPCSink pins that a flow with sink.type grpc — accepted by
// the shared FlowDefinition validator — is constructible by this builder, and
// that unusable tls/timeout settings fail construction instead of silently
// downgrading to insecure or expired-deadline behavior.
func TestBuildPipeline_GRPCSink(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr string
	}{
		{
			name:   "address only",
			config: map[string]interface{}{"address": "127.0.0.1:19090"},
		},
		{
			name:   "tls disabled",
			config: map[string]interface{}{"address": "127.0.0.1:19090", "tls": false},
		},
		{
			name:   "valid timeout",
			config: map[string]interface{}{"address": "127.0.0.1:19090", "timeout": "5s"},
		},
		{
			name:    "tls enabled",
			config:  map[string]interface{}{"address": "127.0.0.1:19090", "tls": true},
			wantErr: "sink config: tls is not supported until gRPC TLS credentials are configurable",
		},
		{
			name:    "tls string value",
			config:  map[string]interface{}{"address": "127.0.0.1:19090", "tls": "true"},
			wantErr: "sink config: tls is not supported until gRPC TLS credentials are configurable",
		},
		{
			name:    "non-string timeout",
			config:  map[string]interface{}{"address": "127.0.0.1:19090", "timeout": 30},
			wantErr: "sink config: timeout must be a duration string",
		},
		{
			name:    "negative timeout",
			config:  map[string]interface{}{"address": "127.0.0.1:19090", "timeout": "-1s"},
			wantErr: `sink config: timeout "-1s" must not be negative`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flowDef := &config.FlowDefinition{
				Name:   "grpc-sink-flow",
				Source: config.SourceConfig{Type: "grpc", Config: map[string]interface{}{"listenAddr": "127.0.0.1:0"}},
				Sink:   config.SinkConfig{Type: "grpc", Config: tt.config},
			}

			p, err := buildPipeline(flowDef, slog.Default(), httpsource.NewServerPool(slog.Default()), noop.NewTracerProvider().Tracer("test"))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("grpc sink must be constructible: %v", err)
				}
				if p == nil {
					t.Fatal("expected a pipeline")
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error to contain %q, got %v", tt.wantErr, err)
			}
		})
	}
}
