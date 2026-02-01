package discovery

import "context"

// StaticResolver returns the host as-is. Used when the host is already a
// full URL or IP address.
type StaticResolver struct{}

// Resolve returns the host unchanged.
func (s *StaticResolver) Resolve(_ context.Context, host string) (string, error) {
	return host, nil
}
