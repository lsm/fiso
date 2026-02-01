package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lsm/fiso/internal/interceptor"
)

// Client abstracts the gRPC call for the interceptor sidecar service.
type Client interface {
	// Call invokes the interceptor service with JSON-encoded request, returns JSON-encoded response.
	Call(ctx context.Context, data []byte) ([]byte, error)
	Close() error
}

// Interceptor calls an external gRPC sidecar service to process requests.
type Interceptor struct {
	client  Client
	timeout time.Duration
}

// Config holds gRPC interceptor configuration.
type Config struct {
	Address string
	Timeout time.Duration
}

// New creates a new gRPC interceptor.
func New(client Client, timeout time.Duration) *Interceptor {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &Interceptor{client: client, timeout: timeout}
}

// requestPayload is the JSON structure sent to the sidecar.
type requestPayload struct {
	Payload   json.RawMessage   `json:"payload"`
	Headers   map[string]string `json:"headers"`
	Direction string            `json:"direction"`
}

// responsePayload is the JSON structure received from the sidecar.
type responsePayload struct {
	Payload json.RawMessage   `json:"payload"`
	Headers map[string]string `json:"headers"`
}

// Process sends the request to the gRPC sidecar service and returns the modified result.
func (i *Interceptor) Process(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
	ctx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	rp := requestPayload{
		Payload:   req.Payload,
		Headers:   req.Headers,
		Direction: string(req.Direction),
	}

	data, err := json.Marshal(rp)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	respData, err := i.client.Call(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("grpc interceptor call: %w", err)
	}

	var resp responsePayload
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &interceptor.Request{
		Payload:   resp.Payload,
		Headers:   resp.Headers,
		Direction: req.Direction,
	}, nil
}

// Close closes the gRPC client.
func (i *Interceptor) Close() error {
	return i.client.Close()
}
