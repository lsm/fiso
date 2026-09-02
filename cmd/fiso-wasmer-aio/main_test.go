//go:build wasmer

package main

import (
	"fmt"
	"log/slog"
	"net"
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

			p, err := buildPipeline(flowDef, slog.Default(), httpsource.NewServerPool(slog.Default()), noop.NewTracerProvider().Tracer("test"), "")
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
	_, err := buildPipeline(flowDef, slog.Default(), httpsource.NewServerPool(slog.Default()), noop.NewTracerProvider().Tracer("test"), "")
	if err == nil {
		t.Fatal("expected unimplemented interceptor type to fail construction, got nil (silently dropped)")
	}
	if !strings.Contains(err.Error(), "wasmer-app") {
		t.Fatalf("expected error to name wasmer-app, got %v", err)
	}
}

// TestHostHTTPConfig_DefaultLinkAddr pins that an HTTP-enabled interceptor
// omitting linkAddr calls the Link aio actually bound, not a hard-coded port:
// with link.listenAddr overridden (e.g. :3600), the derived default must
// follow that listener.
func TestHostHTTPConfig_DefaultLinkAddr(t *testing.T) {
	base := map[string]interface{}{"http": true, "httpTargets": []interface{}{"fraud-api"}}
	tests := []struct {
		name       string
		cfg        map[string]interface{}
		defaultArg string
		want       string
	}{
		{
			name:       "embedded link default used when linkAddr omitted",
			cfg:        base,
			defaultArg: "http://127.0.0.1:3600",
			want:       "http://127.0.0.1:3600",
		},
		{
			name:       "explicit linkAddr wins over embedded default",
			cfg:        map[string]interface{}{"http": true, "httpTargets": []interface{}{"fraud-api"}, "linkAddr": "http://elsewhere:9090"},
			defaultArg: "http://127.0.0.1:3600",
			want:       "http://elsewhere:9090",
		},
		{
			name:       "documented default when no embedded link",
			cfg:        base,
			defaultArg: "",
			want:       "http://127.0.0.1:3500",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostHTTPConfig(tt.cfg, tt.defaultArg)
			if got.LinkAddr != tt.want {
				t.Fatalf("LinkAddr = %q, want %q", got.LinkAddr, tt.want)
			}
		})
	}
}

// TestLoopbackLinkAddr pins the listener-to-origin derivation: an unspecified
// bind host is dialed through the loopback (the proxy is in-process), and the
// actual bound port is used (a :0 bind must not produce :0 in the URL).
func TestLoopbackLinkAddr(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port
	if got := loopbackLinkAddr(ln); got != fmt.Sprintf("http://127.0.0.1:%d", port) {
		t.Fatalf("loopbackLinkAddr = %q, want http://127.0.0.1:%d", got, port)
	}

	wild, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen wildcard: %v", err)
	}
	defer func() { _ = wild.Close() }()
	wildPort := wild.Addr().(*net.TCPAddr).Port
	if got := loopbackLinkAddr(wild); got != fmt.Sprintf("http://127.0.0.1:%d", wildPort) {
		t.Fatalf("loopbackLinkAddr(wildcard) = %q, want http://127.0.0.1:%d", got, wildPort)
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
	p, err := buildPipeline(flowDef, slog.Default(), httpsource.NewServerPool(slog.Default()), noop.NewTracerProvider().Tracer("test"), "")
	if err != nil {
		t.Fatalf("grpc interceptor must be constructible: %v", err)
	}
	if p == nil {
		t.Fatal("expected a pipeline")
	}
}
