package retry

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"time"
)

// Config holds retry configuration.
type Config struct {
	MaxAttempts     int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Jitter          float64 // ±jitter fraction (e.g., 0.2 = ±20%)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:     3,
		InitialInterval: 200 * time.Millisecond,
		MaxInterval:     30 * time.Second,
		Jitter:          0.2,
	}
}

// PermanentError wraps an error that should not be retried.
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent marks an error as permanent (non-retryable).
func Permanent(err error) error {
	return &PermanentError{Err: err}
}

// IsPermanent returns true if the error is a PermanentError.
func IsPermanent(err error) bool {
	var pe *PermanentError
	return errors.As(err, &pe)
}

// Do executes fn with retry logic. It stops retrying when:
// - fn returns nil (success)
// - fn returns a PermanentError
// - MaxAttempts is exhausted
// - ctx is cancelled
func Do(ctx context.Context, cfg Config, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if IsPermanent(lastErr) {
			return lastErr
		}
		if attempt < cfg.MaxAttempts-1 {
			backoff := calcBackoff(attempt, cfg)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return lastErr
}

func calcBackoff(attempt int, cfg Config) time.Duration {
	backoff := float64(cfg.InitialInterval) * math.Pow(2, float64(attempt))
	if backoff > float64(cfg.MaxInterval) {
		backoff = float64(cfg.MaxInterval)
	}
	if cfg.Jitter > 0 {
		jitter := backoff * cfg.Jitter
		backoff = backoff - jitter + rand.Float64()*2*jitter
	}
	return time.Duration(backoff)
}
