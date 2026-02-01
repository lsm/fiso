package transform

import "context"

// Transformer applies a transformation to an event payload.
type Transformer interface {
	// Transform takes a raw event payload and returns the transformed payload.
	// Returns an error if the transformation fails (routes to DLQ).
	Transform(ctx context.Context, input []byte) ([]byte, error)
}
