package interceptor

import "context"

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
