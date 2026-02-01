package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

// State represents the circuit breaker state.
type State int

const (
	Closed   State = 0
	HalfOpen State = 1
	Open     State = 2
)

func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case HalfOpen:
		return "half-open"
	case Open:
		return "open"
	default:
		return "unknown"
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// Config holds circuit breaker configuration.
type Config struct {
	FailureThreshold int
	SuccessThreshold int
	ResetTimeout     time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		ResetTimeout:     30 * time.Second,
	}
}

// Breaker implements the circuit breaker pattern with three states.
type Breaker struct {
	mu               sync.Mutex
	state            State
	failures         int
	successes        int
	failureThreshold int
	successThreshold int
	resetTimeout     time.Duration
	lastFailure      time.Time
	clock            func() time.Time
}

// Option configures a Breaker.
type Option func(*Breaker)

// WithClock sets a custom clock for testing.
func WithClock(clock func() time.Time) Option {
	return func(b *Breaker) {
		b.clock = clock
	}
}

// New creates a new circuit breaker.
func New(cfg Config, opts ...Option) *Breaker {
	b := &Breaker{
		state:            Closed,
		failureThreshold: cfg.FailureThreshold,
		successThreshold: cfg.SuccessThreshold,
		resetTimeout:     cfg.ResetTimeout,
		clock:            time.Now,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Allow checks whether the request is allowed. Returns ErrCircuitOpen if
// the circuit is open and the reset timeout has not elapsed.
func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case Closed:
		return nil
	case Open:
		if b.clock().Sub(b.lastFailure) >= b.resetTimeout {
			b.state = HalfOpen
			b.successes = 0
			return nil
		}
		return ErrCircuitOpen
	case HalfOpen:
		return nil
	default:
		return nil
	}
}

// RecordSuccess records a successful request.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case HalfOpen:
		b.successes++
		if b.successes >= b.successThreshold {
			b.state = Closed
			b.failures = 0
			b.successes = 0
		}
	case Closed:
		b.failures = 0
	}
}

// RecordFailure records a failed request.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case Closed:
		b.failures++
		if b.failures >= b.failureThreshold {
			b.state = Open
			b.lastFailure = b.clock()
		}
	case HalfOpen:
		b.state = Open
		b.lastFailure = b.clock()
		b.successes = 0
	}
}

// State returns the current state.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Counts returns the current failure and success counts.
func (b *Breaker) Counts() (failures, successes int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures, b.successes
}
