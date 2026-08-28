// Package flowruntime provides shared runtime lifecycle primitives for
// Flow-capable binaries.
package flowruntime

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/lsm/fiso/internal/observability"
)

// Gate enforces the aggregate readiness policy for a Flow-capable process:
// every configured startup pipeline is a required runner. Readiness becomes
// true once all runners are launched (SetRunning) and drops to false for the
// rest of the process lifetime as soon as any required runner returns
// terminally — an error other than context cancellation, or an unexpected
// nil return. Expected cancellation during shutdown is not terminal; the
// shutdown path owns that readiness transition. The process stays alive and
// surviving runners are left untouched. See
// docs/adr/0005-drop-readiness-on-required-pipeline-termination.md.
type Gate struct {
	health *observability.HealthServer

	mu        sync.Mutex
	terminals int
}

// NewGate creates a Gate that drives the supplied HealthServer's readiness.
func NewGate(health *observability.HealthServer) *Gate {
	return &Gate{health: health}
}

// Go launches run as a required runner with the process context.
// The returned channel closes after the runner returns and the gate has
// classified the return, so callers can observe readiness transitions
// deterministically.
func (g *Gate) Go(name string, run func(ctx context.Context) error) <-chan struct{} {
	return g.GoContext(context.Background(), name, run)
}

// GoContext launches run as a required runner with the supplied context.
func (g *Gate) GoContext(ctx context.Context, name string, run func(ctx context.Context) error) <-chan struct{} {
	settled := make(chan struct{})
	go func() {
		defer close(settled)
		slog.Info("flow started", "name", name)
		err := run(ctx)
		if isExpectedCancellation(err) {
			slog.Info("flow stopped (shutdown)", "name", name)
			return
		}
		// Terminal: the runner stopped serving and nothing restarts it.
		// Any error that is not context cancellation counts, and so does
		// an unexpected nil return.
		g.mu.Lock()
		g.terminals++
		g.mu.Unlock()
		g.health.SetReady(false)
		if err != nil {
			slog.Error("flow stopped with error; readiness dropped", "name", name, "error", err)
		} else {
			slog.Error("flow stopped without error; readiness dropped", "name", name)
		}
	}()
	return settled
}

// SetRunning marks the process ready once every required runner has been
// launched. If a runner already returned terminally, readiness stays false —
// a failed startup must not be overwritten by a late SetRunning.
func (g *Gate) SetRunning() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.terminals > 0 {
		return
	}
	g.health.SetReady(true)
}

// TerminalCount reports how many required runners returned terminally.
func (g *Gate) TerminalCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.terminals
}

// isExpectedCancellation reports whether err is the error a well-behaved
// runner returns during graceful shutdown. Real sources return a non-nil
// ctx.Err() on cancellation, so terminality cannot be classified by
// err != nil alone.
func isExpectedCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
