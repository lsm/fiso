package retry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDo_Success(t *testing.T) {
	calls := 0
	err := Do(context.Background(), DefaultConfig(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestDo_RetriesTransientError(t *testing.T) {
	cfg := Config{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 10 * time.Millisecond, Jitter: 0}
	calls := 0
	err := Do(context.Background(), cfg, func() error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil after retry, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestDo_StopsOnPermanentError(t *testing.T) {
	cfg := Config{MaxAttempts: 5, InitialInterval: time.Millisecond, MaxInterval: 10 * time.Millisecond, Jitter: 0}
	calls := 0
	err := Do(context.Background(), cfg, func() error {
		calls++
		return Permanent(errors.New("permanent"))
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry on permanent), got %d", calls)
	}
	if !IsPermanent(err) {
		t.Fatalf("expected PermanentError, got %T", err)
	}
}

func TestDo_ExhaustsMaxAttempts(t *testing.T) {
	cfg := Config{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 10 * time.Millisecond, Jitter: 0}
	calls := 0
	err := Do(context.Background(), cfg, func() error {
		calls++
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error after max attempts")
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestDo_RespectsContextCancellation(t *testing.T) {
	cfg := Config{MaxAttempts: 100, InitialInterval: time.Second, MaxInterval: time.Second, Jitter: 0}
	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := Do(ctx, cfg, func() error {
		calls++
		return errors.New("fail")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestPermanentError_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	pe := Permanent(inner)
	if !errors.Is(pe, inner) {
		t.Fatal("expected PermanentError to unwrap to inner")
	}
}

func TestIsPermanent_NilError(t *testing.T) {
	if IsPermanent(nil) {
		t.Fatal("expected false for nil")
	}
}

func TestIsPermanent_NonPermanent(t *testing.T) {
	if IsPermanent(errors.New("regular")) {
		t.Fatal("expected false for regular error")
	}
}

func TestCalcBackoff_ExponentialGrowth(t *testing.T) {
	cfg := Config{InitialInterval: 100 * time.Millisecond, MaxInterval: 10 * time.Second, Jitter: 0}
	b0 := calcBackoff(0, cfg)
	b1 := calcBackoff(1, cfg)
	b2 := calcBackoff(2, cfg)

	if b0 != 100*time.Millisecond {
		t.Errorf("attempt 0: expected 100ms, got %v", b0)
	}
	if b1 != 200*time.Millisecond {
		t.Errorf("attempt 1: expected 200ms, got %v", b1)
	}
	if b2 != 400*time.Millisecond {
		t.Errorf("attempt 2: expected 400ms, got %v", b2)
	}
}

func TestCalcBackoff_CapsAtMaxInterval(t *testing.T) {
	cfg := Config{InitialInterval: 100 * time.Millisecond, MaxInterval: 500 * time.Millisecond, Jitter: 0}
	b := calcBackoff(10, cfg) // 100ms * 2^10 = 102.4s, should be capped
	if b != 500*time.Millisecond {
		t.Errorf("expected cap at 500ms, got %v", b)
	}
}

func TestCalcBackoff_JitterBounds(t *testing.T) {
	cfg := Config{InitialInterval: 100 * time.Millisecond, MaxInterval: 10 * time.Second, Jitter: 0.2}
	for i := 0; i < 100; i++ {
		b := calcBackoff(0, cfg)
		// 100ms ± 20% = 80ms to 120ms
		if b < 80*time.Millisecond || b > 120*time.Millisecond {
			t.Errorf("backoff %v out of jitter bounds [80ms, 120ms]", b)
		}
	}
}

func TestPermanentError_Error(t *testing.T) {
	pe := &PermanentError{Err: fmt.Errorf("bad request")}
	if pe.Error() != "bad request" {
		t.Errorf("expected 'bad request', got %q", pe.Error())
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts 3, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialInterval != 200*time.Millisecond {
		t.Errorf("expected InitialInterval 200ms, got %v", cfg.InitialInterval)
	}
	if cfg.MaxInterval != 30*time.Second {
		t.Errorf("expected MaxInterval 30s, got %v", cfg.MaxInterval)
	}
	if cfg.Jitter != 0.2 {
		t.Errorf("expected Jitter 0.2, got %f", cfg.Jitter)
	}
}
