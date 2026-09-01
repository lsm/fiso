//go:build wasmer

package wasmer

import (
	"context"
	"testing"
	"time"

	"github.com/lsm/fiso/internal/wasm"
)

// fakeAppRuntime is a test double for the app runtime lifecycle.
type fakeAppRuntime struct {
	startAddr string
	stopErr   error
	stopped   bool
	closed    bool
}

func (f *fakeAppRuntime) Call(ctx context.Context, input []byte) ([]byte, error) {
	return input, nil
}

func (f *fakeAppRuntime) Close() error { f.closed = true; return nil }

func (f *fakeAppRuntime) Type() wasm.RuntimeType { return wasm.RuntimeWasmer }

func (f *fakeAppRuntime) Start(ctx context.Context) (string, error) {
	return f.startAddr, nil
}

func (f *fakeAppRuntime) Stop(ctx context.Context) error {
	f.stopped = true
	return f.stopErr
}

func (f *fakeAppRuntime) Addr() string { return f.startAddr }

func (f *fakeAppRuntime) IsRunning() bool { return !f.stopped }

// populatedManager builds a manager with one hand-registered app, following
// the existing test idiom.
func populatedManager(rt wasm.AppRuntime, stopHealth chan struct{}) *Manager {
	m := NewManager()
	m.apps["app"] = &AppInstance{
		Name:       "app",
		Config:     AppConfig{Name: "app", Module: "test.wasm"},
		Runtime:    rt,
		Addr:       "127.0.0.1:19090",
		Health:     HealthHealthy,
		Started:    time.Now(),
		StopHealth: stopHealth,
	}
	return m
}

// TestStopAll_ClosesHealthChannels pins that StopAll terminates every app's
// health-check goroutine: the StopHealth channel must be closed, otherwise
// the goroutine leaks and keeps probing the shut-down server for the rest of
// the process lifetime.
func TestStopAll_ClosesHealthChannels(t *testing.T) {
	stopHealth := make(chan struct{})
	m := populatedManager(&fakeAppRuntime{startAddr: "127.0.0.1:19090"}, stopHealth)

	if err := m.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}

	select {
	case <-stopHealth:
		// closed: health goroutine was told to stop
	default:
		t.Fatal("StopAll must close the app's StopHealth channel; the health-check goroutine leaks otherwise")
	}
}

// TestStopApp_IsIdempotentAfterStopFailure pins that a failed Stop leaves
// the manager in a state where StopApp can be called again without panicking
// on a double close of StopHealth.
func TestStopApp_IsIdempotentAfterStopFailure(t *testing.T) {
	stopHealth := make(chan struct{})
	m := populatedManager(&fakeAppRuntime{
		startAddr: "127.0.0.1:19090",
		stopErr:   errFakeStop,
	}, stopHealth)

	if err := m.StopApp(context.Background(), "app"); err == nil {
		t.Fatal("expected the first StopApp to fail with the runtime error")
	}

	// The retry must not panic (close of an already-closed channel).
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("second StopApp panicked: %v", r)
		}
	}()
	_ = m.StopApp(context.Background(), "app")
}

// errFakeStop is a distinct sentinel so the fake's failure is unambiguous.
var errFakeStop = &fakeStopError{}

type fakeStopError struct{}

func (*fakeStopError) Error() string { return "fake stop failure" }

// TestPortPool_ExplicitPortReserved pins that an explicitly configured port
// is accounted in the pool: after the app stops, the pool must not report
// the port as free if it was never marked used — and conversely a reserved
// explicit port must not be handed to a second Allocate.
func TestPortPool_ExplicitPortReserved(t *testing.T) {
	p := NewPortPool(49100, 49200)

	if !p.Reserve(49150) {
		t.Fatal("reserving an explicit in-range port must succeed")
	}
	// The reserved port must not be handed out by Allocate.
	for i := 0; i < 10; i++ {
		got, err := p.Allocate()
		if err != nil {
			t.Fatalf("allocate: %v", err)
		}
		if got == 49150 {
			t.Fatal("Allocate handed out a port reserved for an explicit-port app")
		}
	}
	// Release is symmetric with Reserve: the port can be reserved again.
	p.Release(49150)
	if !p.Reserve(49150) {
		t.Fatal("after Release, the port must be reserveable again")
	}
}

// TestPortPool_OutOfRangeExplicitPortAllowed pins that an explicit port
// outside the dynamic range is permitted (the app binds it directly); only
// in-range conflicts are rejected.
func TestPortPool_OutOfRangeExplicitPortAllowed(t *testing.T) {
	m := NewManagerWithPortRange(49200, 49210)

	// Far outside the range: Reserve reports untracked, StartApp proceeds.
	if m.portPool.Reserve(49555) {
		t.Fatal("an out-of-range port should not be pool-tracked")
	}
	// In-range reservation works and a second reservation of the same port
	// fails (conflict).
	if !m.portPool.Reserve(49205) {
		t.Fatal("first in-range reservation must succeed")
	}
	if m.portPool.Reserve(49205) {
		t.Fatal("double reservation of an in-range port must fail")
	}
}

// TestStopApp_HealthChannelNotNiled pins that after a failed stop the app
// remains registered with its StopHealth channel still set (closed but not
// nil-ed), so the health goroutine's held reference stays valid.
func TestStopApp_HealthChannelNotNiled(t *testing.T) {
	stopHealth := make(chan struct{})
	m := populatedManager(&fakeAppRuntime{
		startAddr: "127.0.0.1:19090",
		stopErr:   errFakeStop,
	}, stopHealth)

	if err := m.StopApp(context.Background(), "app"); err == nil {
		t.Fatal("expected the stop to fail")
	}
	app, ok := m.GetApp("app")
	if !ok {
		t.Fatal("a failed stop must leave the app registered for retry")
	}
	if app.StopHealth != nil {
		select {
		case <-app.StopHealth:
			// closed exactly once — correct
		default:
			t.Fatal("StopHealth should be closed after the stop attempt")
		}
	} else {
		t.Fatal("StopHealth must not be nil-ed; the health goroutine holds the reference")
	}
}
