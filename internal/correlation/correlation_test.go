package correlation

import (
	"testing"
)

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
