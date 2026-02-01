package discovery

import "context"

// Resolver resolves a target host to a network address.
type Resolver interface {
	Resolve(ctx context.Context, host string) (string, error)
}
