package sink

import "context"

// Sink delivers processed events to a destination.
type Sink interface {
	// Deliver sends a processed event to the destination.
	// Returns nil on success. The pipeline commits the source offset
	// only after Deliver returns nil.
	Deliver(ctx context.Context, event []byte, headers map[string]string) error

	// Close performs graceful shutdown.
	Close() error
}
