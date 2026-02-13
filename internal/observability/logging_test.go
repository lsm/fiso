package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestNewLogger(t *testing.T) {
	logger := NewLogger("test-component", slog.LevelInfo)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	// Verify it can log without panicking
	logger.Info("test message", "key", "value")
}

func TestNewLogger_DebugLevel(t *testing.T) {
	logger := NewLogger("debug-component", slog.LevelDebug)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	logger.Debug("debug message")
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"invalid", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseLogLevel(tt.input)
			if got != tt.expected {
				t.Errorf("ParseLogLevel(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGetLogLevel(t *testing.T) {
	tests := []struct {
		name      string
		flagLevel string
		envLevel  string
		expected  slog.Level
	}{
		{"flag takes precedence", "debug", "error", slog.LevelDebug},
		{"env used when flag empty", "", "warn", slog.LevelWarn},
		{"default when both empty", "", "", slog.LevelInfo},
		{"flag overrides env", "error", "debug", slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset env var for each test
			if tt.envLevel != "" {
				t.Setenv("FISO_LOG_LEVEL", tt.envLevel)
			}
			got := GetLogLevel(tt.flagLevel)
			if got != tt.expected {
				t.Errorf("GetLogLevel(%q) = %v, want %v (env=%q)", tt.flagLevel, got, tt.expected, tt.envLevel)
			}
		})
	}
}

// setupTracer creates a real SDK tracer provider for testing
func setupTracer() func() {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	return func() { _ = tp.Shutdown(context.Background()) }
}

func TestTraceLogger_WithTraceContext(t *testing.T) {
	cleanup := setupTracer()
	defer cleanup()

	// Create a buffer to capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)
	tl := NewTraceLogger(logger)

	// Create a tracer and start a span
	tracer := otel.GetTracerProvider().Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	// Get logger with trace context
	loggerWithTrace := tl.WithTraceContext(ctx)

	// Log a message
	loggerWithTrace.Info("test message")

	// Parse the output
	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log output: %v", err)
	}

	// Verify trace_id and span_id are present
	if _, ok := entry["trace_id"]; !ok {
		t.Error("expected trace_id in log output")
	}
	if _, ok := entry["span_id"]; !ok {
		t.Error("expected span_id in log output")
	}
}

func TestTraceLogger_WithTraceContext_NoSpan(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)
	tl := NewTraceLogger(logger)

	// Context without a span
	ctx := context.Background()

	// Get logger with trace context
	loggerWithTrace := tl.WithTraceContext(ctx)

	// Log a message
	loggerWithTrace.Info("test message")

	// Parse the output
	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log output: %v", err)
	}

	// Verify trace_id and span_id are NOT present
	if _, ok := entry["trace_id"]; ok {
		t.Error("expected no trace_id in log output when no span")
	}
	if _, ok := entry["span_id"]; ok {
		t.Error("expected no span_id in log output when no span")
	}
}

func TestTraceLogger_Info(t *testing.T) {
	cleanup := setupTracer()
	defer cleanup()

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)
	tl := NewTraceLogger(logger)

	// Create a span
	tracer := otel.GetTracerProvider().Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	tl.Info(ctx, "info message", "key", "value")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log output: %v", err)
	}

	if entry["msg"] != "info message" {
		t.Errorf("expected msg 'info message', got %v", entry["msg"])
	}
	if entry["key"] != "value" {
		t.Errorf("expected key 'value', got %v", entry["key"])
	}
	if _, ok := entry["trace_id"]; !ok {
		t.Error("expected trace_id in log output")
	}
}

func TestTraceLogger_Error(t *testing.T) {
	cleanup := setupTracer()
	defer cleanup()

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)
	tl := NewTraceLogger(logger)

	// Create a span
	tracer := otel.GetTracerProvider().Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	tl.Error(ctx, "error message", "error", "test-error")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log output: %v", err)
	}

	if entry["msg"] != "error message" {
		t.Errorf("expected msg 'error message', got %v", entry["msg"])
	}
	if _, ok := entry["trace_id"]; !ok {
		t.Error("expected trace_id in log output")
	}
}

func TestTraceLogger_Debug(t *testing.T) {
	cleanup := setupTracer()
	defer cleanup()

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)
	tl := NewTraceLogger(logger)

	// Create a span
	tracer := otel.GetTracerProvider().Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	tl.Debug(ctx, "debug message")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log output: %v", err)
	}

	if entry["msg"] != "debug message" {
		t.Errorf("expected msg 'debug message', got %v", entry["msg"])
	}
	if _, ok := entry["trace_id"]; !ok {
		t.Error("expected trace_id in log output")
	}
}

func TestTraceLogger_Warn(t *testing.T) {
	cleanup := setupTracer()
	defer cleanup()

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)
	tl := NewTraceLogger(logger)

	// Create a span
	tracer := otel.GetTracerProvider().Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	tl.Warn(ctx, "warn message")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log output: %v", err)
	}

	if entry["msg"] != "warn message" {
		t.Errorf("expected msg 'warn message', got %v", entry["msg"])
	}
	if _, ok := entry["trace_id"]; !ok {
		t.Error("expected trace_id in log output")
	}
}

func TestTraceLogger_With(t *testing.T) {
	cleanup := setupTracer()
	defer cleanup()

	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(handler)
	tl := NewTraceLogger(logger)

	// Create a new logger with additional fields
	tl2 := tl.With("service", "test-service")
	if tl2 == nil {
		t.Fatal("expected non-nil TraceLogger")
	}

	// Create a span
	tracer := otel.GetTracerProvider().Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	tl2.Info(ctx, "with message")

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log output: %v", err)
	}

	if entry["service"] != "test-service" {
		t.Errorf("expected service 'test-service', got %v", entry["service"])
	}
	if _, ok := entry["trace_id"]; !ok {
		t.Error("expected trace_id in log output")
	}
}
