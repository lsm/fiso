package tracing

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
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

	// Verify the tracer can create spans
	_, span := tracer.Start(context.Background(), "test-span")
	if span == nil {
		t.Error("expected non-nil span")
	}
	span.End()

	// Shutdown should be a no-op for disabled tracing
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("unexpected shutdown error: %v", err)
	}
}

func TestPropagator(t *testing.T) {
	// Set up a real tracer provider with propagator
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	prop := Propagator()
	if prop == nil {
		t.Error("expected non-nil propagator")
	}
}

func TestInitialize_Enabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg := Config{
		Enabled:     true,
		Endpoint:    "localhost:4317",
		ServiceName: "test-service",
	}

	tracer, shutdown, err := Initialize(cfg, logger)
	if err != nil {
		// If connection fails, that's OK - we're just testing the initialization path
		t.Logf("initialize returned error (expected if no OTLP endpoint): %v", err)
		return
	}

	if tracer == nil {
		t.Fatal("expected non-nil tracer")
	}

	// Verify the tracer can create spans
	_, span := tracer.Start(context.Background(), "test-span")
	if span == nil {
		t.Error("expected non-nil span")
	}
	span.End()

	// Verify propagator was set
	prop := otel.GetTextMapPropagator()
	if prop == nil {
		t.Error("expected non-nil global propagator")
	}

	// Clean shutdown
	if err := shutdown(context.Background()); err != nil {
		t.Logf("shutdown returned error: %v", err)
	}
}

func TestPropagator_ExtractsAndInjects(t *testing.T) {
	// Set up a real tracer provider with propagator
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	prop := Propagator()
	if prop == nil {
		t.Fatal("expected non-nil propagator")
	}

	// Create a span and inject trace context
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	carrier := propagation.MapCarrier{}
	prop.Inject(ctx, carrier)

	// Verify traceparent was injected
	if _, ok := carrier["traceparent"]; !ok {
		t.Error("expected traceparent header to be injected")
	}

	// Now extract from carrier into new context
	newCtx := prop.Extract(context.Background(), carrier)
	spanCtx := span.SpanContext()

	// Verify span context from extracted context
	_, extractedSpan := otel.Tracer("test2").Start(newCtx, "child")
	defer extractedSpan.End()
	if !extractedSpan.SpanContext().TraceID().IsValid() {
		t.Error("expected valid trace ID from extracted context")
	}
	if extractedSpan.SpanContext().TraceID() != spanCtx.TraceID() {
		t.Error("expected trace IDs to match")
	}
}

func TestStartSpan_WithNilTracer(t *testing.T) {
	ctx := context.Background()
	newCtx, span := StartSpan(ctx, nil, "test-span")
	defer span.End()

	// Should return no-op span
	if span == nil {
		t.Error("expected non-nil span even with nil tracer")
	}
	if newCtx != ctx {
		t.Error("expected same context with nil tracer")
	}
}

func TestStartSpan_WithTracer(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	ctx := context.Background()

	newCtx, span := StartSpan(ctx, tracer, "test-span")
	defer span.End()

	if span == nil {
		t.Fatal("expected non-nil span")
	}
	if newCtx == ctx {
		t.Error("expected new context with span")
	}
}

func TestSetSpanError(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")

	// Test with valid error
	testErr := errors.New("test error")
	SetSpanError(span, testErr)
	span.End()

	// Test with nil span (should not panic)
	SetSpanError(nil, testErr)

	// Test with nil error (should not panic)
	SetSpanError(span, nil)
}

func TestSetSpanOK(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")

	// Test with valid span
	SetSpanOK(span)
	span.End()

	// Test with nil span (should not panic)
	SetSpanOK(nil)
}

func TestSetSpanOKWithMessage(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")

	// Test with valid span
	SetSpanOKWithMessage(span, "success message")
	span.End()

	// Test with nil span (should not panic)
	SetSpanOKWithMessage(nil, "success message")
}

func TestAttributeConstructors(t *testing.T) {
	tests := []struct {
		name    string
		attr    attribute.KeyValue
		wantKey string
		wantVal interface{}
	}{
		{"FlowAttr", FlowAttr("my-flow"), AttrFlowName, "my-flow"},
		{"CorrelationAttr", CorrelationAttr("corr-123"), AttrCorrelationID, "corr-123"},
		{"KafkaTopicAttr", KafkaTopicAttr("my-topic"), AttrKafkaTopic, "my-topic"},
		{"KafkaPartitionAttr", KafkaPartitionAttr(5), AttrKafkaPartition, int64(5)},
		{"KafkaOffsetAttr", KafkaOffsetAttr(12345), AttrKafkaOffset, int64(12345)},
		{"HTTPTargetAttr", HTTPTargetAttr("/api/v1/users"), AttrHTTPTarget, "/api/v1/users"},
		{"HTTPMethodAttr", HTTPMethodAttr("POST"), AttrHTTPMethod, "POST"},
		{"HTTPStatusAttr", HTTPStatusAttr(200), AttrHTTPStatus, 200},
		{"GRPCMethodAttr", GRPCMethodAttr("GetUser"), AttrGRPCMethod, "GetUser"},
		{"WorkflowTypeAttr", WorkflowTypeAttr("MyWorkflow"), AttrWorkflowType, "MyWorkflow"},
		{"WorkflowIDAttr", WorkflowIDAttr("wf-123"), AttrWorkflowID, "wf-123"},
		{"SignalNameAttr", SignalNameAttr("complete"), AttrSignalName, "complete"},
		{"TargetNameAttr", TargetNameAttr("my-target"), AttrTargetName, "my-target"},
		{"ErrorTypeAttr", ErrorTypeAttr("ValidationError"), AttrErrorType, "ValidationError"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.attr.Key) != tt.wantKey {
				t.Errorf("expected key %s, got %s", tt.wantKey, tt.attr.Key)
			}
			// Compare values based on type
			switch v := tt.wantVal.(type) {
			case string:
				if tt.attr.Value.AsString() != v {
					t.Errorf("expected value %v, got %v", v, tt.attr.Value.AsString())
				}
			case int:
				if tt.attr.Value.AsInt64() != int64(v) {
					t.Errorf("expected value %v, got %v", v, tt.attr.Value.AsInt64())
				}
			case int64:
				if tt.attr.Value.AsInt64() != v {
					t.Errorf("expected value %v, got %v", v, tt.attr.Value.AsInt64())
				}
			}
		})
	}
}

func TestSpanFromContext(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// Without span
	ctx := context.Background()
	span := SpanFromContext(ctx)
	if span == nil {
		t.Error("expected non-nil span even without active span")
	}

	// With span
	tracer := tp.Tracer("test")
	ctx2, createdSpan := tracer.Start(context.Background(), "test-span")
	defer createdSpan.End()
	span2 := SpanFromContext(ctx2)
	if span2 == nil {
		t.Error("expected non-nil span")
	}
}

func TestIsTraced(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// Without span
	ctx := context.Background()
	if IsTraced(ctx) {
		t.Error("expected IsTraced to be false without span")
	}

	// With span
	tracer := tp.Tracer("test")
	ctx2, span := tracer.Start(context.Background(), "test-span")
	if !IsTraced(ctx2) {
		t.Error("expected IsTraced to be true with active span")
	}
	span.End()
}

func TestInitialize_EnabledWithTimeout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg := Config{
		Enabled:     true,
		Endpoint:    "localhost:4317", // Won't actually connect in test
		ServiceName: "test-service",
	}

	// This will test the enabled path with a connection timeout
	tracer, shutdown, err := Initialize(cfg, logger)

	// The connection might fail or succeed depending on environment
	if err != nil {
		t.Logf("initialize returned error (expected if no OTLP endpoint): %v", err)
		return
	}

	if tracer == nil {
		t.Fatal("expected non-nil tracer")
	}

	// Clean shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Logf("shutdown returned error: %v", err)
	}
}

func TestStartSpan_WithOptions(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	ctx := context.Background()

	// Test with span options
	_, span := StartSpan(ctx, tracer, "test-span-with-attrs", trace.WithAttributes(attribute.String("key", "value")))
	defer span.End()

	if span == nil {
		t.Fatal("expected non-nil span")
	}
}

func TestSetSpanError_EdgeCases(t *testing.T) {
	// Test with nil span and nil error - should not panic
	SetSpanError(nil, nil)

	// Test with valid span and nil error - should not set error
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	SetSpanError(span, nil)
	span.End()
}

func TestSetSpanOK_EdgeCases(t *testing.T) {
	// Test with nil span - should not panic
	SetSpanOK(nil)
	SetSpanOKWithMessage(nil, "")
}

func TestSpanFromContext_NoSpan(t *testing.T) {
	// Test with empty context
	ctx := context.Background()
	span := SpanFromContext(ctx)

	// Should return a no-op span, not nil
	if span == nil {
		t.Error("expected non-nil span from empty context")
	}

	// Span should not be recording
	if span.IsRecording() {
		t.Error("expected no-op span to not be recording")
	}
}
