package correlation

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func init() {
	// Initialize the global propagator for tests
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

func TestExtractTraceContext_NilHeaders(t *testing.T) {
	ctx := context.Background()

	result := ExtractTraceContext(ctx, nil)

	if result != ctx {
		t.Error("expected same context when headers is nil")
	}
}

func TestExtractTraceContext_EmptyHeaders(t *testing.T) {
	ctx := context.Background()
	headers := map[string]string{}

	result := ExtractTraceContext(ctx, headers)

	// Should return a valid context even with empty headers
	if result == nil {
		t.Error("expected non-nil context")
	}
}

func TestExtractTraceContext_WithTraceparent(t *testing.T) {
	ctx := context.Background()
	headers := map[string]string{
		HeaderTraceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}

	result := ExtractTraceContext(ctx, headers)

	// Verify trace context was extracted
	span := trace.SpanFromContext(result)
	if !span.SpanContext().IsValid() {
		t.Error("expected valid span context from traceparent header")
	}

	expectedTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	if span.SpanContext().TraceID().String() != expectedTraceID {
		t.Errorf("expected trace ID %s, got %s", expectedTraceID, span.SpanContext().TraceID().String())
	}
}

func TestInjectTraceContext_NilHeaders(t *testing.T) {
	ctx := context.Background()

	result := InjectTraceContext(ctx, nil)

	if result == nil {
		t.Error("expected headers map to be created")
	}
}

func TestInjectTraceContext_ExistingHeaders(t *testing.T) {
	ctx := context.Background()
	headers := map[string]string{
		"existing-header": "existing-value",
	}

	result := InjectTraceContext(ctx, headers)

	if result["existing-header"] != "existing-value" {
		t.Error("expected existing header to be preserved")
	}
}

func TestInjectTraceContext_WithSpan(t *testing.T) {
	// Create a context with a valid trace context
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	headers := map[string]string{}
	result := InjectTraceContext(ctx, headers)

	// Verify traceparent header was injected
	if result[HeaderTraceparent] == "" {
		t.Error("expected traceparent header to be injected")
	}

	// Verify the injected traceparent contains the trace ID
	expected := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	if result[HeaderTraceparent] != expected {
		t.Errorf("expected traceparent %s, got %s", expected, result[HeaderTraceparent])
	}
}

func TestInjectExtractRoundTrip(t *testing.T) {
	// Create a context with a valid trace context
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	originalCtx := trace.ContextWithSpanContext(context.Background(), spanContext)

	// Inject trace context into headers
	headers := map[string]string{}
	headers = InjectTraceContext(originalCtx, headers)

	// Extract trace context from headers into a new context
	newCtx := ExtractTraceContext(context.Background(), headers)

	// Verify the trace context was preserved
	originalSpan := trace.SpanFromContext(originalCtx)
	newSpan := trace.SpanFromContext(newCtx)

	if originalSpan.SpanContext().TraceID() != newSpan.SpanContext().TraceID() {
		t.Errorf("expected trace ID to be preserved, original: %s, new: %s",
			originalSpan.SpanContext().TraceID(), newSpan.SpanContext().TraceID())
	}
}

func TestExtractOrGenerate_FisoHeader(t *testing.T) {
	headers := map[string]string{
		HeaderCorrelationID:  "fiso-id-123",
		HeaderXCorrelationID: "x-corr-id-456",
		HeaderXRequestID:     "req-id-789",
	}

	id := ExtractOrGenerate(headers)

	if id.Value != "fiso-id-123" {
		t.Errorf("expected fiso-id-123, got %s", id.Value)
	}
	if id.Source != HeaderCorrelationID {
		t.Errorf("expected source %s, got %s", HeaderCorrelationID, id.Source)
	}
}

func TestExtractOrGenerate_XCorrelationID(t *testing.T) {
	headers := map[string]string{
		HeaderXCorrelationID: "x-corr-id-456",
		HeaderXRequestID:     "req-id-789",
	}

	id := ExtractOrGenerate(headers)

	if id.Value != "x-corr-id-456" {
		t.Errorf("expected x-corr-id-456, got %s", id.Value)
	}
	if id.Source != HeaderXCorrelationID {
		t.Errorf("expected source %s, got %s", HeaderXCorrelationID, id.Source)
	}
}

func TestExtractOrGenerate_XRequestID(t *testing.T) {
	headers := map[string]string{
		HeaderXRequestID: "req-id-789",
	}

	id := ExtractOrGenerate(headers)

	if id.Value != "req-id-789" {
		t.Errorf("expected req-id-789, got %s", id.Value)
	}
	if id.Source != HeaderXRequestID {
		t.Errorf("expected source %s, got %s", HeaderXRequestID, id.Source)
	}
}

func TestExtractOrGenerate_Traceparent(t *testing.T) {
	headers := map[string]string{
		HeaderTraceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}

	id := ExtractOrGenerate(headers)

	expectedTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	if id.Value != expectedTraceID {
		t.Errorf("expected %s, got %s", expectedTraceID, id.Value)
	}
	if id.Source != HeaderTraceparent {
		t.Errorf("expected source %s, got %s", HeaderTraceparent, id.Source)
	}
}

func TestExtractOrGenerate_Generated(t *testing.T) {
	headers := map[string]string{}

	id := ExtractOrGenerate(headers)

	if id.Value == "" {
		t.Error("expected non-empty generated ID")
	}
	if id.Source != "generated" {
		t.Errorf("expected source generated, got %s", id.Source)
	}
	// Verify it's a valid UUID format (36 chars with dashes)
	if len(id.Value) != 36 {
		t.Errorf("expected UUID length 36, got %d", len(id.Value))
	}
}

func TestExtractOrGenerate_Priority(t *testing.T) {
	tests := []struct {
		name           string
		headers        map[string]string
		expectedValue  string
		expectedSource string
	}{
		{
			name: "fiso header takes priority over x-correlation-id",
			headers: map[string]string{
				HeaderCorrelationID:  "fiso-id",
				HeaderXCorrelationID: "x-corr-id",
			},
			expectedValue:  "fiso-id",
			expectedSource: HeaderCorrelationID,
		},
		{
			name: "x-correlation-id takes priority over x-request-id",
			headers: map[string]string{
				HeaderXCorrelationID: "x-corr-id",
				HeaderXRequestID:     "req-id",
			},
			expectedValue:  "x-corr-id",
			expectedSource: HeaderXCorrelationID,
		},
		{
			name: "x-request-id takes priority over traceparent",
			headers: map[string]string{
				HeaderXRequestID:  "req-id",
				HeaderTraceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			},
			expectedValue:  "req-id",
			expectedSource: HeaderXRequestID,
		},
		{
			name: "traceparent takes priority over generated",
			headers: map[string]string{
				HeaderTraceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			},
			expectedValue:  "4bf92f3577b34da6a3ce929d0e0e4736",
			expectedSource: HeaderTraceparent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := ExtractOrGenerate(tt.headers)
			if id.Value != tt.expectedValue {
				t.Errorf("expected value %s, got %s", tt.expectedValue, id.Value)
			}
			if id.Source != tt.expectedSource {
				t.Errorf("expected source %s, got %s", tt.expectedSource, id.Source)
			}
		})
	}
}

func TestExtractTraceID(t *testing.T) {
	tests := []struct {
		name        string
		traceparent string
		expected    string
	}{
		{
			name:        "valid W3C traceparent",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			expected:    "4bf92f3577b34da6a3ce929d0e0e4736",
		},
		{
			name:        "invalid traceparent - too short trace ID",
			traceparent: "00-4bf92f3577b34da6-00f067aa0ba902b7-01",
			expected:    "",
		},
		{
			name:        "invalid traceparent - missing parent ID",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736",
			expected:    "4bf92f3577b34da6a3ce929d0e0e4736",
		},
		{
			name:        "empty string",
			traceparent: "",
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTraceID(tt.traceparent)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestAddToHeaders_NilMap(t *testing.T) {
	id := ID{Value: "test-id-123", Source: "test"}

	headers := AddToHeaders(nil, id)

	if headers == nil {
		t.Error("expected headers to be created")
	}
	if headers[HeaderCorrelationID] != "test-id-123" {
		t.Errorf("expected test-id-123, got %s", headers[HeaderCorrelationID])
	}
}

func TestAddToHeaders_ExistingMap(t *testing.T) {
	id := ID{Value: "test-id-123", Source: "test"}
	headers := map[string]string{
		"existing-header": "existing-value",
	}

	headers = AddToHeaders(headers, id)

	if headers[HeaderCorrelationID] != "test-id-123" {
		t.Errorf("expected test-id-123, got %s", headers[HeaderCorrelationID])
	}
	if headers["existing-header"] != "existing-value" {
		t.Errorf("expected existing-value, got %s", headers["existing-header"])
	}
}

func TestAddToHeaders_Overwrites(t *testing.T) {
	id := ID{Value: "new-id-456", Source: "test"}
	headers := map[string]string{
		HeaderCorrelationID: "old-id-123",
	}

	headers = AddToHeaders(headers, id)

	if headers[HeaderCorrelationID] != "new-id-456" {
		t.Errorf("expected new-id-456, got %s", headers[HeaderCorrelationID])
	}
}

func TestHeaderCarrier_Get(t *testing.T) {
	tests := []struct {
		name     string
		carrier  headerCarrier
		key      string
		expected string
	}{
		{
			name:     "nil carrier returns empty",
			carrier:  nil,
			key:      "test-key",
			expected: "",
		},
		{
			name:     "key exists",
			carrier:  headerCarrier{"test-key": "test-value"},
			key:      "test-key",
			expected: "test-value",
		},
		{
			name:     "key does not exist",
			carrier:  headerCarrier{"other-key": "other-value"},
			key:      "test-key",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.carrier.Get(tt.key)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestHeaderCarrier_Set(t *testing.T) {
	t.Run("nil carrier does nothing", func(t *testing.T) {
		var carrier headerCarrier = nil
		carrier.Set("key", "value")
		// Should not panic
	})

	t.Run("set value on carrier", func(t *testing.T) {
		carrier := headerCarrier{}
		carrier.Set("key", "value")
		if carrier["key"] != "value" {
			t.Errorf("expected value, got %s", carrier["key"])
		}
	})
}

func TestHeaderCarrier_Keys(t *testing.T) {
	t.Run("nil carrier returns nil", func(t *testing.T) {
		var carrier headerCarrier = nil
		keys := carrier.Keys()
		if keys != nil {
			t.Errorf("expected nil, got %v", keys)
		}
	})

	t.Run("empty carrier returns empty slice", func(t *testing.T) {
		carrier := headerCarrier{}
		keys := carrier.Keys()
		if len(keys) != 0 {
			t.Errorf("expected empty slice, got %v", keys)
		}
	})

	t.Run("carrier with keys returns all keys", func(t *testing.T) {
		carrier := headerCarrier{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
		}
		keys := carrier.Keys()
		if len(keys) != 3 {
			t.Errorf("expected 3 keys, got %d", len(keys))
		}
		// Verify all keys are present
		keySet := make(map[string]bool)
		for _, k := range keys {
			keySet[k] = true
		}
		for k := range carrier {
			if !keySet[k] {
				t.Errorf("key %s not found in Keys()", k)
			}
		}
	})
}

func TestHeaderConstants(t *testing.T) {
	if HeaderCorrelationID != "fiso-correlation-id" {
		t.Errorf("expected fiso-correlation-id, got %s", HeaderCorrelationID)
	}
	if HeaderXCorrelationID != "x-correlation-id" {
		t.Errorf("expected x-correlation-id, got %s", HeaderXCorrelationID)
	}
	if HeaderXRequestID != "x-request-id" {
		t.Errorf("expected x-request-id, got %s", HeaderXRequestID)
	}
	if HeaderTraceparent != "traceparent" {
		t.Errorf("expected traceparent, got %s", HeaderTraceparent)
	}
}
