package correlation

import (
	"strings"

	"github.com/google/uuid"
)

const (
	HeaderCorrelationID  = "fiso-correlation-id"
	HeaderXCorrelationID = "x-correlation-id"
	HeaderXRequestID     = "x-request-id"
	HeaderTraceparent    = "traceparent"
)

type ID struct {
	Value  string
	Source string
}

// ExtractOrGenerate extracts correlation ID from headers or generates a new UUID.
// Priority: fiso-correlation-id > x-correlation-id > x-request-id > traceparent > new UUID
func ExtractOrGenerate(headers map[string]string) ID {
	if id := headers[HeaderCorrelationID]; id != "" {
		return ID{Value: id, Source: HeaderCorrelationID}
	}
	if id := headers[HeaderXCorrelationID]; id != "" {
		return ID{Value: id, Source: HeaderXCorrelationID}
	}
	if id := headers[HeaderXRequestID]; id != "" {
		return ID{Value: id, Source: HeaderXRequestID}
	}
	if tp := headers[HeaderTraceparent]; tp != "" {
		if traceID := extractTraceID(tp); traceID != "" {
			return ID{Value: traceID, Source: HeaderTraceparent}
		}
	}
	return ID{Value: uuid.New().String(), Source: "generated"}
}

// extractTraceID parses W3C traceparent format: version-traceid-parentid-flags
func extractTraceID(traceparent string) string {
	parts := strings.Split(traceparent, "-")
	if len(parts) >= 2 && len(parts[1]) == 32 {
		return parts[1]
	}
	return ""
}

// AddToHeaders adds correlation ID to headers map (creates map if nil)
func AddToHeaders(headers map[string]string, id ID) map[string]string {
	if headers == nil {
		headers = make(map[string]string, 1)
	}
	headers[HeaderCorrelationID] = id.Value
	return headers
}
