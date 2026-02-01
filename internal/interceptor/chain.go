package interceptor

import (
	"context"
	"fmt"
)

// Chain executes a sequence of interceptors in order.
type Chain struct {
	interceptors []Interceptor
}

// NewChain creates a new interceptor chain.
func NewChain(interceptors ...Interceptor) *Chain {
	return &Chain{interceptors: interceptors}
}

// Process runs all interceptors in order, passing the output of each to the next.
func (c *Chain) Process(ctx context.Context, req *Request) (*Request, error) {
	current := req
	for i, ic := range c.interceptors {
		result, err := ic.Process(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("interceptor %d: %w", i, err)
		}
		current = result
	}
	return current, nil
}

// Close closes all interceptors in the chain.
func (c *Chain) Close() error {
	var firstErr error
	for _, ic := range c.interceptors {
		if err := ic.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Len returns the number of interceptors in the chain.
func (c *Chain) Len() int {
	return len(c.interceptors)
}
