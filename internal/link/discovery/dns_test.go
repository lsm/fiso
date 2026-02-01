package discovery

import (
	"context"
	"testing"
	"time"
)

func TestDNSResolver_ResolvesLocalhost(t *testing.T) {
	r := NewDNSResolver(WithTTL(1 * time.Second))
	addr, err := r.Resolve(context.Background(), "localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// localhost should resolve to 127.0.0.1 or ::1
	if addr != "127.0.0.1" && addr != "::1" {
		t.Errorf("expected 127.0.0.1 or ::1, got %s", addr)
	}
}

func TestDNSResolver_CachesTTL(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	r := NewDNSResolver(WithTTL(10*time.Second), WithDNSClock(clock))

	// First call should resolve
	addr1, err := r.Resolve(context.Background(), "localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second call within TTL should return cached
	addr2, err := r.Resolve(context.Background(), "localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr1 != addr2 {
		t.Errorf("expected cached result %s, got %s", addr1, addr2)
	}
}

func TestDNSResolver_CacheExpires(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	r := NewDNSResolver(WithTTL(5*time.Second), WithDNSClock(clock))

	_, err := r.Resolve(context.Background(), "localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Advance past TTL
	now = now.Add(6 * time.Second)

	// Should re-resolve (not use stale cache)
	addr, err := r.Resolve(context.Background(), "localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "127.0.0.1" && addr != "::1" {
		t.Errorf("expected 127.0.0.1 or ::1 after cache expiry, got %s", addr)
	}
}

func TestDNSResolver_InvalidHost(t *testing.T) {
	r := NewDNSResolver()
	_, err := r.Resolve(context.Background(), "this-host-definitely-does-not-exist.invalid")
	if err == nil {
		t.Fatal("expected error for invalid host")
	}
}

func TestDNSResolver_ContextCancelled(t *testing.T) {
	r := NewDNSResolver()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.Resolve(ctx, "localhost")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
