//go:build wasmer

package main

import (
	"log/slog"
	"testing"

	"github.com/lsm/fiso/internal/config"
	httpsource "github.com/lsm/fiso/internal/source/http"
	"go.opentelemetry.io/otel/trace/noop"
)

// TestBuildPipeline_GRPCSink pins that a flow with sink.type grpc — accepted by
// the shared FlowDefinition validator — is constructible by this builder.
func TestBuildPipeline_GRPCSink(t *testing.T) {
	flowDef := &config.FlowDefinition{
		Name:   "grpc-sink-flow",
		Source: config.SourceConfig{Type: "grpc", Config: map[string]interface{}{"listenAddr": "127.0.0.1:0"}},
		Sink:   config.SinkConfig{Type: "grpc", Config: map[string]interface{}{"address": "127.0.0.1:19090"}},
	}

	p, err := buildPipeline(flowDef, slog.Default(), httpsource.NewServerPool(slog.Default()), noop.NewTracerProvider().Tracer("test"))
	if err != nil {
		t.Fatalf("grpc sink must be constructible: %v", err)
	}
	if p == nil {
		t.Fatal("expected a pipeline")
	}
}
