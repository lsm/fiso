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
			name:    "null tls",
			config:  map[string]interface{}{"address": "127.0.0.1:19090", "tls": nil},
			wantErr: "sink config: tls is not supported until gRPC TLS credentials are configurable",
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
		{
			name:    "zero timeout",
			config:  map[string]interface{}{"address": "127.0.0.1:19090", "timeout": "0s"},
			wantErr: `sink config: timeout "0s" must be positive`,
		},
		{
			name:    "null timeout",
			config:  map[string]interface{}{"address": "127.0.0.1:19090", "timeout": nil},
			wantErr: "sink config: timeout must be a duration string",
		},
		{
			name:    "empty timeout",
			config:  map[string]interface{}{"address": "127.0.0.1:19090", "timeout": ""},
			wantErr: `sink config: timeout "" is not a valid duration`,
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

// TestBuildPipeline_UnimplementedInterceptorErrors pins that unsupported
// interceptor types fail construction instead of being silently dropped.
func TestBuildPipeline_UnimplementedInterceptorErrors(t *testing.T) {
	flowDef := &config.FlowDefinition{
		Name:   "wasmer-app-flow",
		Source: config.SourceConfig{Type: "grpc", Config: map[string]interface{}{"listenAddr": "127.0.0.1:0"}},
		Sink:   config.SinkConfig{Type: "http", Config: map[string]interface{}{"url": "http://127.0.0.1:19090"}},
		Interceptors: []config.InterceptorConfig{{
			Type:   "wasmer-app",
			Config: map[string]interface{}{"module": "app.wasm"},
		}},
	}
	_, err := buildPipeline(flowDef, slog.Default(), httpsource.NewServerPool(slog.Default()), noop.NewTracerProvider().Tracer("test"))
	if err == nil {
		t.Fatal("expected unimplemented interceptor type to fail construction, got nil (silently dropped)")
	}
	if !strings.Contains(err.Error(), "wasmer-app") {
		t.Fatalf("expected error to name wasmer-app, got %v", err)
	}
}

// TestBuildPipeline_GRPCInterceptor pins that a grpc interceptor — accepted
// by the shared validator — is constructible by this builder (ADR 0003).
func TestBuildPipeline_GRPCInterceptor(t *testing.T) {
	flowDef := &config.FlowDefinition{
		Name:   "grpc-interceptor-flow",
		Source: config.SourceConfig{Type: "grpc", Config: map[string]interface{}{"listenAddr": "127.0.0.1:0"}},
		Sink:   config.SinkConfig{Type: "http", Config: map[string]interface{}{"url": "http://127.0.0.1:19090"}},
		Interceptors: []config.InterceptorConfig{{
			Type:   "grpc",
			Config: map[string]interface{}{"address": "127.0.0.1:19091"},
		}},
	}
	p, err := buildPipeline(flowDef, slog.Default(), httpsource.NewServerPool(slog.Default()), noop.NewTracerProvider().Tracer("test"))
	if err != nil {
		t.Fatalf("grpc interceptor must be constructible: %v", err)
	}
	if p == nil {
		t.Fatal("expected a pipeline")
	}
}
