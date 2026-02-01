package ratelimit

import (
	"sync"

	"golang.org/x/time/rate"
)

// Limiter provides per-target rate limiting using token bucket algorithm.
type Limiter struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
}

// New creates a new Limiter with no targets configured.
func New() *Limiter {
	return &Limiter{
		limiters: make(map[string]*rate.Limiter),
	}
}

// Set configures rate limiting for a target.
// A zero rps means no rate limit for that target.
func (l *Limiter) Set(target string, rps float64, burst int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if rps <= 0 {
		delete(l.limiters, target)
		return
	}
	if burst <= 0 {
		burst = int(rps)
		if burst < 1 {
			burst = 1
		}
	}
	l.limiters[target] = rate.NewLimiter(rate.Limit(rps), burst)
}

// Allow reports whether a request for the given target is allowed.
// Returns true if no rate limit is configured for the target.
func (l *Limiter) Allow(target string) bool {
	l.mu.RLock()
	lim, ok := l.limiters[target]
	l.mu.RUnlock()

	if !ok {
		return true
	}
	return lim.Allow()
}
