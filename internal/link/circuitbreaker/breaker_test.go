package circuitbreaker

import (
	"testing"
	"time"
)

func TestNew_DefaultState(t *testing.T) {
	b := New(DefaultConfig())
	if b.State() != Closed {
		t.Fatalf("expected Closed, got %s", b.State())
	}
}

func TestAllow_ClosedAlwaysAllows(t *testing.T) {
	b := New(DefaultConfig())
	for i := 0; i < 10; i++ {
		if err := b.Allow(); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	}
}

func TestTransition_ClosedToOpen(t *testing.T) {
	cfg := Config{FailureThreshold: 3, SuccessThreshold: 1, ResetTimeout: 10 * time.Second}
	b := New(cfg)

	// Record failures up to threshold
	for i := 0; i < 3; i++ {
		_ = b.Allow()
		b.RecordFailure()
	}

	if b.State() != Open {
		t.Fatalf("expected Open after %d failures, got %s", cfg.FailureThreshold, b.State())
	}
}

func TestAllow_OpenReturnsError(t *testing.T) {
	cfg := Config{FailureThreshold: 1, SuccessThreshold: 1, ResetTimeout: 1 * time.Hour}
	b := New(cfg)

	_ = b.Allow()
	b.RecordFailure()

	err := b.Allow()
	if err != ErrCircuitOpen {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestTransition_OpenToHalfOpen(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	cfg := Config{FailureThreshold: 1, SuccessThreshold: 1, ResetTimeout: 5 * time.Second}
	b := New(cfg, WithClock(clock))

	_ = b.Allow()
	b.RecordFailure()

	if b.State() != Open {
		t.Fatalf("expected Open, got %s", b.State())
	}

	// Advance clock past reset timeout
	now = now.Add(6 * time.Second)

	err := b.Allow()
	if err != nil {
		t.Fatalf("expected nil after reset timeout, got %v", err)
	}
	if b.State() != HalfOpen {
		t.Fatalf("expected HalfOpen, got %s", b.State())
	}
}

func TestTransition_HalfOpenToClosed(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	cfg := Config{FailureThreshold: 1, SuccessThreshold: 2, ResetTimeout: 5 * time.Second}
	b := New(cfg, WithClock(clock))

	// Trip to Open
	_ = b.Allow()
	b.RecordFailure()

	// Advance past reset
	now = now.Add(6 * time.Second)
	_ = b.Allow() // transitions to HalfOpen

	// Record successes up to threshold
	b.RecordSuccess()
	if b.State() != HalfOpen {
		t.Fatalf("expected still HalfOpen after 1 success, got %s", b.State())
	}
	b.RecordSuccess()
	if b.State() != Closed {
		t.Fatalf("expected Closed after %d successes, got %s", cfg.SuccessThreshold, b.State())
	}
}

func TestTransition_HalfOpenToOpen(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	cfg := Config{FailureThreshold: 1, SuccessThreshold: 3, ResetTimeout: 5 * time.Second}
	b := New(cfg, WithClock(clock))

	// Trip to Open
	_ = b.Allow()
	b.RecordFailure()

	// Advance past reset
	now = now.Add(6 * time.Second)
	_ = b.Allow() // transitions to HalfOpen

	// Single failure in HalfOpen should trip back to Open
	b.RecordFailure()
	if b.State() != Open {
		t.Fatalf("expected Open after failure in HalfOpen, got %s", b.State())
	}
}

func TestRecordSuccess_ClosedResetsFailures(t *testing.T) {
	cfg := Config{FailureThreshold: 3, SuccessThreshold: 1, ResetTimeout: 10 * time.Second}
	b := New(cfg)

	// Accumulate some failures (below threshold)
	_ = b.Allow()
	b.RecordFailure()
	_ = b.Allow()
	b.RecordFailure()

	failures, _ := b.Counts()
	if failures != 2 {
		t.Fatalf("expected 2 failures, got %d", failures)
	}

	// Success should reset failure count
	b.RecordSuccess()
	failures, _ = b.Counts()
	if failures != 0 {
		t.Fatalf("expected 0 failures after success, got %d", failures)
	}
}

func TestOpenDoesNotTransition_BeforeTimeout(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	cfg := Config{FailureThreshold: 1, SuccessThreshold: 1, ResetTimeout: 10 * time.Second}
	b := New(cfg, WithClock(clock))

	_ = b.Allow()
	b.RecordFailure()

	// Advance only 5 seconds (less than 10s timeout)
	now = now.Add(5 * time.Second)

	err := b.Allow()
	if err != ErrCircuitOpen {
		t.Fatalf("expected ErrCircuitOpen before timeout, got %v", err)
	}
	if b.State() != Open {
		t.Fatalf("expected still Open, got %s", b.State())
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{Closed, "closed"},
		{HalfOpen, "half-open"},
		{Open, "open"},
		{State(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %s, want %s", tt.state, got, tt.want)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	cfg := Config{FailureThreshold: 100, SuccessThreshold: 100, ResetTimeout: 10 * time.Second}
	b := New(cfg)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				_ = b.Allow()
				b.RecordFailure()
				b.RecordSuccess()
				_ = b.State()
				_, _ = b.Counts()
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
