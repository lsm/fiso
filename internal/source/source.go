package source

import "context"

// Event represents a raw event consumed from a source.
type Event struct {
	Key           []byte
	Value         []byte
	Headers       map[string]string
	Offset        int64
	Topic         string
	CorrelationID string
}

// Source consumes events from an external system.
type Source interface {
	// Start begins consuming events. Blocks until ctx is cancelled.
	// Events are delivered to the handler function.
	Start(ctx context.Context, handler func(context.Context, Event) error) error

	// Close performs graceful shutdown.
	Close() error
}
