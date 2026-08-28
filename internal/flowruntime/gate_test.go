package flowruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lsm/fiso/internal/observability"
)

// The Gate implements the aggregate readiness policy for Flow-capable
// binaries: every configured startup pipeline is required; any terminal
// return (an error that is not context cancellation, or an unexpected nil)
// drops process readiness while other runners and /healthz are unaffected.
// See docs/adr/0005-drop-readiness-on-required-pipeline-termination.md.

// fakeRunner is a deterministic required-runner double. Run signals that it
// entered, then blocks until the test forces a terminal return (stop) or the
// context is cancelled. finished closes just before Run returns.
type fakeRunner struct {
	started  chan struct{}
	stop     chan struct{}
	err      error
	finished chan struct{}
}

func newFakeRunner(err error) *fakeRunner {
	return &fakeRunner{
		started:  make(chan struct{}),
		stop:     make(chan struct{}),
		err:      err,
		finished: make(chan struct{}),
	}
}

func (f *fakeRunner) Run(ctx context.Context) error {
	close(f.started)
	defer close(f.finished)
	select {
	case <-f.stop:
		return f.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func readyzStatus(t *testing.T, h *observability.HealthServer) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	return rec.Code
}

func mustBe200(t *testing.T, h *observability.HealthServer, stage string) {
	t.Helper()
	if code := readyzStatus(t, h); code != http.StatusOK {
		t.Fatalf("%s: expected /readyz 200, got %d", stage, code)
	}
}

func mustBe503(t *testing.T, h *observability.HealthServer, stage string) {
	t.Helper()
	if code := readyzStatus(t, h); code != http.StatusServiceUnavailable {
		t.Fatalf("%s: expected /readyz 503, got %d", stage, code)
	}
}

func TestGate_RequiredRunnerErrorDropsReadiness(t *testing.T) {
	health := observability.NewHealthServer()
	gate := NewGate(health)
	runner := newFakeRunner(errors.New("listen 127.0.0.1:19444: bind: address already in use"))

	settled := gate.Go("dead-flow", runner.Run)
	gate.SetRunning()
	mustBe200(t, health, "after startup")

	close(runner.stop) // terminal failure: the source listener cannot bind
	<-settled          // gate has classified the return
	mustBe503(t, health, "after terminal runner error")
}

func TestGate_UnexpectedNilReturnDropsReadiness(t *testing.T) {
	health := observability.NewHealthServer()
	gate := NewGate(health)
	// A required pipeline returning nil without shutdown is terminal: it
	// stopped serving and nothing will restart it.
	runner := newFakeRunner(nil)

	settled := gate.Go("silent-flow", runner.Run)
	gate.SetRunning()
	mustBe200(t, health, "after startup")

	close(runner.stop)
	<-settled
	mustBe503(t, health, "after unexpected nil return")
}

func TestGate_TerminalRunnerLeavesOtherRunnersRunning(t *testing.T) {
	health := observability.NewHealthServer()
	gate := NewGate(health)
	dead := newFakeRunner(errors.New("kafka: broker unreachable"))
	alive := newFakeRunner(nil)

	deadSettled := gate.Go("dead-flow", dead.Run)
	aliveSettled := gate.Go("alive-flow", alive.Run)
	gate.SetRunning()
	mustBe200(t, health, "after startup")

	close(dead.stop)
	<-deadSettled
	mustBe503(t, health, "aggregate readiness after one terminal runner")

	// The surviving runner must still be running: the gate must not cancel
	// the shared context or otherwise disturb it.
	<-alive.started
	select {
	case <-alive.finished:
		t.Fatal("surviving runner was terminated by the gate")
	default:
	}
	select {
	case <-aliveSettled:
		t.Fatal("surviving runner settled without being stopped")
	default:
	}
}

func TestGate_ExpectedCancellationKeepsReadiness(t *testing.T) {
	health := observability.NewHealthServer()
	gate := NewGate(health)
	ctx, cancel := context.WithCancel(context.Background())
	runner := newFakeRunner(nil)

	settled := gate.GoContext(ctx, "flow", runner.Run)
	gate.SetRunning()
	mustBe200(t, health, "after startup")

	cancel() // graceful shutdown: Run returns context.Canceled
	<-runner.finished
	<-settled
	// Readiness on shutdown is owned by the binary's shutdown path, which
	// has not run yet; the gate itself must not flip it here — but it must
	// not flag the expected cancellation as terminal either.
	if gate.TerminalCount() != 0 {
		t.Fatalf("expected cancellation must not count as terminal, got %d", gate.TerminalCount())
	}
}

func TestGate_ZeroRequiredRunnersIsReady(t *testing.T) {
	// Pins the fiso-wasmer-aio shape: flow config loading may legitimately
	// yield zero flows; with no required runner failed, readiness holds.
	health := observability.NewHealthServer()
	gate := NewGate(health)
	gate.SetRunning()
	mustBe200(t, health, "zero required runners")
}

func TestGate_SetRunningAfterTerminalStaysNotReady(t *testing.T) {
	// A runner that dies during startup (e.g. its listener cannot bind)
	// must not be overwritten by a later SetRunning.
	health := observability.NewHealthServer()
	gate := NewGate(health)
	settled := gate.Go("dead-at-startup", func(context.Context) error {
		return errors.New("listen 127.0.0.1:19444: bind: address already in use")
	})
	<-settled
	gate.SetRunning()
	mustBe503(t, health, "after SetRunning following a terminal runner")
}
