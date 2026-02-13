package correlation

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/lsm/fiso/internal/tracing"
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

// headerCarrier implements propagation.TextMapCarrier for map[string]string.
type headerCarrier map[string]string

// Get returns the value associated with the passed key.
func (h headerCarrier) Get(key string) string {
	if h == nil {
		return ""
	}
	return h[key]
}

// Set stores the key-value pair.
func (h headerCarrier) Set(key string, value string) {
	if h == nil {
		return
	}
	h[key] = value
}

// Keys lists the keys stored in this carrier.
func (h headerCarrier) Keys() []string {
	if h == nil {
		return nil
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	return keys
}

// ExtractTraceContext extracts W3C trace context from headers into the context.
// This allows trace propagation across service boundaries.
func ExtractTraceContext(ctx context.Context, headers map[string]string) context.Context {
	if headers == nil {
		return ctx
	}
	return tracing.Propagator().Extract(ctx, headerCarrier(headers))
}

// InjectTraceContext injects the current trace context from ctx into headers.
// This should be called before making outbound requests to propagate tracing.
func InjectTraceContext(ctx context.Context, headers map[string]string) map[string]string {
	if headers == nil {
		headers = make(map[string]string)
	}
	tracing.Propagator().Inject(ctx, headerCarrier(headers))
	return headers
}
