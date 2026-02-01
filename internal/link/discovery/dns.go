package discovery

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// DNSResolver resolves hostnames via DNS with a TTL cache.
type DNSResolver struct {
	mu    sync.RWMutex
	cache map[string]cacheEntry
	ttl   time.Duration
	clock func() time.Time
}

type cacheEntry struct {
	addr      string
	expiresAt time.Time
}

// DNSOption configures a DNSResolver.
type DNSOption func(*DNSResolver)

// WithTTL sets the cache TTL.
func WithTTL(ttl time.Duration) DNSOption {
	return func(r *DNSResolver) {
		r.ttl = ttl
	}
}

// WithDNSClock sets a custom clock for testing.
func WithDNSClock(clock func() time.Time) DNSOption {
	return func(r *DNSResolver) {
		r.clock = clock
	}
}

// NewDNSResolver creates a DNS resolver with optional TTL cache.
func NewDNSResolver(opts ...DNSOption) *DNSResolver {
	r := &DNSResolver{
		cache: make(map[string]cacheEntry),
		ttl:   30 * time.Second,
		clock: time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Resolve looks up the host via DNS, returning the first IP address.
// Results are cached for the configured TTL.
func (r *DNSResolver) Resolve(ctx context.Context, host string) (string, error) {
	r.mu.RLock()
	entry, ok := r.cache[host]
	r.mu.RUnlock()

	if ok && r.clock().Before(entry.expiresAt) {
		return entry.addr, nil
	}

	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return "", fmt.Errorf("dns lookup %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("dns lookup %s: no addresses found", host)
	}

	addr := addrs[0]
	r.mu.Lock()
	r.cache[host] = cacheEntry{
		addr:      addr,
		expiresAt: r.clock().Add(r.ttl),
	}
	r.mu.Unlock()

	return addr, nil
}
