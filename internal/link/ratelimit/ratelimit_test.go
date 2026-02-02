package ratelimit

import (
	"testing"
)

func TestLimiter_Allow_NoConfig(t *testing.T) {
	l := New()
	// No rate limit configured — should always allow
	for i := 0; i < 100; i++ {
		if !l.Allow("unknown") {
			t.Fatal("expected allow for unconfigured target")
		}
	}
}

func TestLimiter_Allow_Configured(t *testing.T) {
	l := New()
	l.Set("svc", 1, 1) // 1 req/sec, burst 1

	// First request should be allowed
	if !l.Allow("svc") {
		t.Fatal("expected first request to be allowed")
	}

	// Second request should be rate limited (burst=1, already consumed)
	if l.Allow("svc") {
		t.Fatal("expected second request to be rate limited")
	}
}

func TestLimiter_ZeroRPS_NoLimit(t *testing.T) {
	l := New()
	l.Set("svc", 0, 0) // Zero rps = no limit

	for i := 0; i < 100; i++ {
		if !l.Allow("svc") {
			t.Fatal("expected allow for zero rps target")
		}
	}
}

func TestLimiter_Set_RemovesOnZero(t *testing.T) {
	l := New()
	l.Set("svc", 10, 10)

	if !l.Allow("svc") {
		t.Fatal("expected allow for configured target")
	}

	// Remove by setting zero
	l.Set("svc", 0, 0)

	// Should now always allow (no limit)
	for i := 0; i < 100; i++ {
		if !l.Allow("svc") {
			t.Fatal("expected allow after removing rate limit")
		}
	}
}

func TestLimiter_Burst(t *testing.T) {
	l := New()
	l.Set("svc", 1, 3) // 1 req/sec, burst 3

	// Should allow 3 requests (burst)
	for i := 0; i < 3; i++ {
		if !l.Allow("svc") {
			t.Fatalf("expected request %d to be allowed (burst=3)", i+1)
		}
	}

	// 4th should be rate limited
	if l.Allow("svc") {
		t.Fatal("expected 4th request to be rate limited")
	}
}

func TestLimiter_MultipleTargets(t *testing.T) {
	l := New()
	l.Set("a", 1, 1)
	l.Set("b", 1, 1)

	if !l.Allow("a") {
		t.Fatal("expected allow for target a")
	}
	if !l.Allow("b") {
		t.Fatal("expected allow for target b")
	}

	// Both exhausted
	if l.Allow("a") {
		t.Fatal("expected rate limit for target a")
	}
	if l.Allow("b") {
		t.Fatal("expected rate limit for target b")
	}
}

func TestLimiter_DefaultBurst(t *testing.T) {
	l := New()
	// When burst <= 0, it should default to int(rps)
	l.Set("svc", 5, 0) // burst=0, should default to 5

	// Should allow 5 requests (default burst = rps)
	allowed := 0
	for i := 0; i < 10; i++ {
		if l.Allow("svc") {
			allowed++
		}
	}

	if allowed != 5 {
		t.Errorf("expected 5 requests to be allowed with default burst, got %d", allowed)
	}
}

func TestLimiter_DefaultBurstSmallRPS(t *testing.T) {
	l := New()
	// When rps < 1 and burst <= 0, burst should default to 1
	l.Set("svc", 0.5, 0) // burst=0, rps=0.5, should default burst to 1

	// Should allow at least 1 request
	if !l.Allow("svc") {
		t.Fatal("expected first request to be allowed with default burst=1")
	}

	// Second should be rate limited
	if l.Allow("svc") {
		t.Fatal("expected second request to be rate limited")
	}
}

func TestLimiter_NegativeBurst(t *testing.T) {
	l := New()
	// When burst < 0, it should default to int(rps)
	l.Set("svc", 3, -1) // burst=-1, should default to 3

	// Should allow 3 requests (default burst = rps)
	allowed := 0
	for i := 0; i < 10; i++ {
		if l.Allow("svc") {
			allowed++
		}
	}

	if allowed != 3 {
		t.Errorf("expected 3 requests to be allowed with default burst, got %d", allowed)
	}
}

func TestLimiter_NegativeRPS(t *testing.T) {
	l := New()
	// Negative rps should behave like zero (no limit)
	l.Set("svc", -1, 10)

	for i := 0; i < 100; i++ {
		if !l.Allow("svc") {
			t.Fatal("expected allow for negative rps target")
		}
	}
}
