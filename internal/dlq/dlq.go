package dlq

import (
	"context"
	"fmt"
	"time"
)

// Publisher is the interface for publishing messages to a broker.
type Publisher interface {
	Publish(ctx context.Context, topic string, key, value []byte, headers map[string]string) error
	Close() error
}

// FailureInfo contains metadata about why an event failed processing.
type FailureInfo struct {
	OriginalTopic string
	ErrorCode     string
	ErrorMessage  string
	RetryCount    int
	FlowName      string
	CorrelationID string
}

// Handler publishes failed events to a Dead Letter Queue topic.
type Handler struct {
	publisher Publisher
	topicFn   func(flowName string) string
}

// Option configures a Handler.
type Option func(*Handler)

// WithTopicFunc overrides the default DLQ topic naming function.
func WithTopicFunc(fn func(flowName string) string) Option {
	return func(h *Handler) {
		h.topicFn = fn
	}
}

// NewHandler creates a new DLQ handler.
func NewHandler(pub Publisher, opts ...Option) *Handler {
	h := &Handler{
		publisher: pub,
		topicFn:   func(flowName string) string { return "fiso-dlq-" + flowName },
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Send publishes a failed event to the appropriate DLQ topic.
func (h *Handler) Send(ctx context.Context, key, value []byte, info FailureInfo) error {
	topic := h.topicFn(info.FlowName)

	headers := map[string]string{
		"fiso-original-topic": info.OriginalTopic,
		"fiso-error-code":     info.ErrorCode,
		"fiso-error-message":  info.ErrorMessage,
		"fiso-retry-count":    fmt.Sprintf("%d", info.RetryCount),
		"fiso-failed-at":      time.Now().UTC().Format(time.RFC3339),
		"fiso-flow-name":      info.FlowName,
		"fiso-correlation-id": info.CorrelationID,
	}

	if err := h.publisher.Publish(ctx, topic, key, value, headers); err != nil {
		return fmt.Errorf("dlq publish to %s: %w", topic, err)
	}
	return nil
}

// Close releases resources held by the handler.
func (h *Handler) Close() error {
	return h.publisher.Close()
}

// NoopPublisher is a Publisher that discards all messages.
// Used when no message broker is configured (e.g., HTTP/gRPC sources).
type NoopPublisher struct{}

func (*NoopPublisher) Publish(context.Context, string, []byte, []byte, map[string]string) error {
	return nil
}

func (*NoopPublisher) Close() error { return nil }
