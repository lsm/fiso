package main

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"

	"github.com/lsm/fiso/internal/config"
	"github.com/lsm/fiso/internal/kafka"
	"github.com/lsm/fiso/internal/pipeline"
	httpsource "github.com/lsm/fiso/internal/source/http"
)

// wasmModulePath is a pre-compiled guest checked in for the interceptor tests;
// the builder reads and instantiates it, so the file must exist.
var wasmModulePath = filepath.Join("..", "..", "internal", "interceptor", "wasm", "testdata", "partial-output", "module.wasm")

// build runs the default builder with throwaway dependencies. Each call gets
// its own HTTP server pool so path reservations cannot collide across cases.
//
// A constructed pipeline owns real resources — a Temporal connection, gRPC
// client connections, Kafka clients, a wazero runtime — that only Shutdown
// releases, so every successful build is closed during cleanup. Without it a
// -count=N run would accumulate connections, goroutines and runtime memory.
func build(t *testing.T, flowDef *config.FlowDefinition) (*pipeline.Pipeline, error) {
	t.Helper()
	logger := slog.Default()
	p, err := buildPipeline(flowDef, logger, httpsource.NewServerPool(logger), noop.NewTracerProvider().Tracer("test"))
	if p != nil {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := p.Shutdown(ctx); err != nil {
				t.Errorf("pipeline shutdown: %v", err)
			}
		})
	}
	return p, err
}

// assertCoversSupportedTypes fails when the cases under test are not exactly
// the set validation accepts. Hard-coded tables would not notice a type added
// to the validator later: every existing case would still pass while the new
// value went unbuilt, which is the validator/runtime drift ADR 0003 exists to
// prevent. Enumerating the validator's own set makes that addition fail here.
func assertCoversSupportedTypes(t *testing.T, kind string, tested []string, supported []string) {
	t.Helper()
	slices.Sort(tested)
	if !slices.Equal(tested, supported) {
		t.Fatalf("%s cases %v do not match the types validation accepts %v: every supported value needs construction evidence (ADR 0003)", kind, tested, supported)
	}
}

// testKafkaClusters returns the named cluster the kafka source and sink cases
// reference. No broker runs: both construct their clients lazily.
func testKafkaClusters() kafka.KafkaGlobalConfig {
	return kafka.KafkaGlobalConfig{Clusters: map[string]kafka.ClusterConfig{
		"primary": {Name: "primary", Brokers: []string{"127.0.0.1:19092"}},
	}}
}

// stubTemporalServer starts a bare gRPC server and returns its host:port. The
// temporal sink's client dials eagerly and fetches server capabilities; an
// Unimplemented reply is the documented "older server" case the SDK accepts,
// so an empty server is enough to exercise construction without a Temporal
// deployment.
func stubTemporalServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(srv.Stop)
	return ln.Addr().String()
}

func httpSinkConfig() config.SinkConfig {
	return config.SinkConfig{Type: "http", Config: map[string]interface{}{"url": "http://127.0.0.1:19090"}}
}

// TestBuildPipeline_SupportedSourceTypes pins that every source type the
// shared FlowDefinition validator accepts is constructible by this builder
// (ADR 0003): validation and the shipped runtime path must agree.
func TestBuildPipeline_SupportedSourceTypes(t *testing.T) {
	tests := []struct {
		name   string
		source config.SourceConfig
	}{
		{
			name: "kafka",
			source: config.SourceConfig{Type: "kafka", Config: map[string]interface{}{
				"cluster":       "primary",
				"topic":         "orders",
				"consumerGroup": "fiso",
			}},
		},
		{
			name:   "grpc",
			source: config.SourceConfig{Type: "grpc", Config: map[string]interface{}{"listenAddr": "127.0.0.1:0"}},
		},
		{
			name:   "http",
			source: config.SourceConfig{Type: "http", Config: map[string]interface{}{"listenAddr": "127.0.0.1:0", "path": "/events"}},
		},
	}

	tested := make([]string, 0, len(tests))
	for _, tt := range tests {
		tested = append(tested, tt.source.Type)
	}
	assertCoversSupportedTypes(t, "source", tested, config.SupportedSourceTypes())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flowDef := &config.FlowDefinition{
				Name:   tt.name + "-source-flow",
				Source: tt.source,
				Sink:   httpSinkConfig(),
				Kafka:  testKafkaClusters(),
			}
			if err := flowDef.Validate(); err != nil {
				t.Fatalf("flow must validate: %v", err)
			}
			p, err := build(t, flowDef)
			if err != nil {
				t.Fatalf("%s source must be constructible: %v", tt.name, err)
			}
			if p == nil {
				t.Fatal("expected a pipeline")
			}
		})
	}
}

// TestBuildPipeline_SupportedSinkTypes pins that every sink type the shared
// validator accepts is constructible by this builder (ADR 0003).
func TestBuildPipeline_SupportedSinkTypes(t *testing.T) {
	temporalHostPort := stubTemporalServer(t)

	tests := []struct {
		name string
		sink config.SinkConfig
	}{
		{
			name: "http",
			sink: httpSinkConfig(),
		},
		{
			name: "grpc",
			sink: config.SinkConfig{Type: "grpc", Config: map[string]interface{}{"address": "127.0.0.1:19090"}},
		},
		{
			name: "temporal",
			sink: config.SinkConfig{Type: "temporal", Config: map[string]interface{}{
				"taskQueue":    "orders",
				"workflowType": "ProcessOrder",
				"hostPort":     temporalHostPort,
				"tls":          map[string]interface{}{"disabled": true},
			}},
		},
		{
			name: "kafka",
			sink: config.SinkConfig{Type: "kafka", Config: map[string]interface{}{
				"cluster": "primary",
				"topic":   "orders-out",
			}},
		},
	}

	tested := make([]string, 0, len(tests))
	for _, tt := range tests {
		tested = append(tested, tt.sink.Type)
	}
	assertCoversSupportedTypes(t, "sink", tested, config.SupportedSinkTypes())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flowDef := &config.FlowDefinition{
				Name:   tt.name + "-sink-flow",
				Source: config.SourceConfig{Type: "grpc", Config: map[string]interface{}{"listenAddr": "127.0.0.1:0"}},
				Sink:   tt.sink,
				Kafka:  testKafkaClusters(),
			}
			if err := flowDef.Validate(); err != nil {
				t.Fatalf("flow must validate: %v", err)
			}
			p, err := build(t, flowDef)
			if err != nil {
				t.Fatalf("%s sink must be constructible: %v", tt.name, err)
			}
			if p == nil {
				t.Fatal("expected a pipeline")
			}
		})
	}
}

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

			p, err := build(t, flowDef)
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

// TestBuildPipeline_SupportedInterceptorTypes pins that every interceptor type
// the shared validator accepts is constructible by this builder (ADR 0003).
func TestBuildPipeline_SupportedInterceptorTypes(t *testing.T) {
	tests := []struct {
		name        string
		interceptor config.InterceptorConfig
	}{
		{
			name:        "wasm",
			interceptor: config.InterceptorConfig{Type: "wasm", Config: map[string]interface{}{"module": wasmModulePath}},
		},
		{
			name:        "grpc",
			interceptor: config.InterceptorConfig{Type: "grpc", Config: map[string]interface{}{"address": "127.0.0.1:19091"}},
		},
	}

	tested := make([]string, 0, len(tests))
	for _, tt := range tests {
		tested = append(tested, tt.interceptor.Type)
	}
	assertCoversSupportedTypes(t, "interceptor", tested, config.SupportedInterceptorTypes())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flowDef := &config.FlowDefinition{
				Name:         tt.name + "-interceptor-flow",
				Source:       config.SourceConfig{Type: "grpc", Config: map[string]interface{}{"listenAddr": "127.0.0.1:0"}},
				Sink:         httpSinkConfig(),
				Interceptors: []config.InterceptorConfig{tt.interceptor},
			}
			if err := flowDef.Validate(); err != nil {
				t.Fatalf("flow must validate: %v", err)
			}
			p, err := build(t, flowDef)
			if err != nil {
				t.Fatalf("%s interceptor must be constructible: %v", tt.name, err)
			}
			if p == nil {
				t.Fatal("expected a pipeline")
			}
		})
	}
}

// TestBuildPipeline_UnsupportedTypes pins that values outside the supported
// sets fail construction with a naming error instead of being silently
// dropped or defaulted.
func TestBuildPipeline_UnsupportedTypes(t *testing.T) {
	tests := []struct {
		name    string
		flowDef *config.FlowDefinition
		wantErr string
	}{
		{
			name: "source",
			flowDef: &config.FlowDefinition{
				Name:   "bad-source-flow",
				Source: config.SourceConfig{Type: "redis", Config: map[string]interface{}{}},
				Sink:   httpSinkConfig(),
			},
			wantErr: "unsupported source type: redis",
		},
		{
			name: "sink",
			flowDef: &config.FlowDefinition{
				Name:   "bad-sink-flow",
				Source: config.SourceConfig{Type: "grpc", Config: map[string]interface{}{"listenAddr": "127.0.0.1:0"}},
				Sink:   config.SinkConfig{Type: "redis", Config: map[string]interface{}{}},
			},
			wantErr: "unsupported sink type: redis",
		},
		{
			name: "interceptor",
			flowDef: &config.FlowDefinition{
				Name:   "bad-interceptor-flow",
				Source: config.SourceConfig{Type: "grpc", Config: map[string]interface{}{"listenAddr": "127.0.0.1:0"}},
				Sink:   httpSinkConfig(),
				Interceptors: []config.InterceptorConfig{{
					Type:   "wasmer-app",
					Config: map[string]interface{}{"module": "app.wasm"},
				}},
			},
			wantErr: "unsupported interceptor type: wasmer-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.flowDef.Validate(); err == nil {
				t.Fatal("expected the shared validator to reject this flow too")
			}
			_, err := build(t, tt.flowDef)
			if err == nil {
				t.Fatalf("expected %q, got nil (silently accepted)", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error to contain %q, got %v", tt.wantErr, err)
			}
		})
	}
}
