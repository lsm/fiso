package interceptor

import (
	"context"
	"errors"
	"fmt"
)

// Request represents an outbound request or inbound event passing through the interceptor chain.
type Request struct {
	// Payload is the raw event/request body.
	Payload []byte
	// Headers are key-value metadata.
	Headers map[string]string
	// Direction indicates if this is inbound or outbound.
	Direction Direction
}

// Direction indicates whether the interceptor is processing an inbound or outbound message.
type Direction string

const (
	Inbound  Direction = "inbound"
	Outbound Direction = "outbound"
)

// Interceptor processes requests/events in the interceptor chain.
type Interceptor interface {
	// Process processes a request and returns the modified request.
	// The interceptor may modify payload, headers, or reject the request by returning an error.
	Process(ctx context.Context, req *Request) (*Request, error)
	// Close performs cleanup.
	Close() error
}

// RejectedError is a deliberate refusal returned by an interceptor (e.g. a
// wasm guest answering with a `reject` object). It is not a failure: the
// event is terminally disposed of — no retries, no dead-letter — and
// request-response surfaces answer with Status instead of a generic error.
type RejectedError struct {
	// Status is the caller-facing HTTP status the refusal maps to
	// (400–599; gRPC surfaces translate it to the closest code).
	Status int
	// Reason is a short, log-safe explanation; it becomes the response
	// body for HTTP surfaces. It must not echo credentials.
	Reason string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("rejected with status %d: %s", e.Status, e.Reason)
}

// AsRejection reports whether err is (or wraps) a RejectedError and returns it.
func AsRejection(err error) (*RejectedError, bool) {
	var rej *RejectedError
	if errors.As(err, &rej) {
		return rej, true
	}
	return nil, false
}
