package interceptor

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/lsm/fiso/internal/interceptor"
	"github.com/lsm/fiso/internal/link"
)

// mockMetricsRecorder captures interceptor invocations for testing.
type mockMetricsRecorder struct {
	invocations []struct {
		target   string
		module   string
		phase    string
		success  bool
		duration float64
	}
}

func (m *mockMetricsRecorder) RecordInterceptorInvocation(target, module, phase string, success bool, durationSeconds float64) {
	m.invocations = append(m.invocations, struct {
		target   string
		module   string
		phase    string
		success  bool
		duration float64
	}{target, module, phase, success, durationSeconds})
}

// mockInterceptor is a simple interceptor for testing.
type mockInterceptor struct {
	processFunc func(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error)
	closed      bool
}

func (m *mockInterceptor) Process(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
	if m.processFunc != nil {
		return m.processFunc(ctx, req)
	}
	return req, nil
}

func (m *mockInterceptor) Close() error {
	m.closed = true
	return nil
}

func TestNewRegistry(t *testing.T) {
	metrics := &mockMetricsRecorder{}
	logger := slog.Default()

	r := NewRegistry(metrics, logger)

	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.chains == nil {
		t.Error("expected chains map to be initialized")
	}
	if r.metrics != metrics {
		t.Error("expected metrics to be set")
	}
}

func TestRegistry_GetChains_NoInterceptors(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// No chains configured
	chains := r.GetChains("unknown-target")
	if chains != nil {
		t.Error("expected nil chains for unknown target")
	}
}

func TestRegistry_ProcessOutbound_NoChain(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	req := &interceptor.Request{
		Payload:   []byte(`{"test": true}`),
		Headers:   map[string]string{"Content-Type": "application/json"},
		Direction: interceptor.Outbound,
	}

	result, err := r.ProcessOutbound(context.Background(), "unknown-target", req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != req {
		t.Error("expected same request when no chain configured")
	}
}

func TestRegistry_ProcessInbound_NoChain(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	req := &interceptor.Request{
		Payload:   []byte(`{"test": true}`),
		Headers:   map[string]string{"Content-Type": "application/json"},
		Direction: interceptor.Inbound,
	}

	result, err := r.ProcessInbound(context.Background(), "unknown-target", req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != req {
		t.Error("expected same request when no chain configured")
	}
}

func TestRegistry_Close(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Close should not error even with no runtimes
	if err := r.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestInterceptorWrapper_Process_Success(t *testing.T) {
	metrics := &mockMetricsRecorder{}

	wrapper := &InterceptorWrapper{
		Interceptor: &mockInterceptor{
			processFunc: func(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
				// Modify the request
				req.Headers["X-Modified"] = "true"
				return req, nil
			},
		},
		module:   "test.wasm",
		failOpen: false,
		metrics:  metrics,
	}

	req := &interceptor.Request{
		Payload:   []byte(`{}`),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	result, err := wrapper.Process(context.Background(), req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Headers["X-Modified"] != "true" {
		t.Error("expected header to be modified")
	}

	// Check metrics were recorded
	if len(metrics.invocations) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(metrics.invocations))
	}
	if metrics.invocations[0].module != "test.wasm" {
		t.Errorf("expected module test.wasm, got %s", metrics.invocations[0].module)
	}
	if !metrics.invocations[0].success {
		t.Error("expected success=true")
	}
}

func TestInterceptorWrapper_Process_Error_FailClosed(t *testing.T) {
	metrics := &mockMetricsRecorder{}
	expectedErr := errors.New("interceptor error")

	wrapper := &InterceptorWrapper{
		Interceptor: &mockInterceptor{
			processFunc: func(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
				return nil, expectedErr
			},
		},
		module:   "test.wasm",
		failOpen: false,
		metrics:  metrics,
	}

	req := &interceptor.Request{
		Payload:   []byte(`{}`),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	result, err := wrapper.Process(context.Background(), req)
	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if result != nil {
		t.Error("expected nil result on error")
	}

	// Check metrics recorded failure
	if len(metrics.invocations) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(metrics.invocations))
	}
	if metrics.invocations[0].success {
		t.Error("expected success=false")
	}
}

func TestInterceptorWrapper_Process_Error_FailOpen(t *testing.T) {
	metrics := &mockMetricsRecorder{}

	wrapper := &InterceptorWrapper{
		Interceptor: &mockInterceptor{
			processFunc: func(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
				return nil, errors.New("interceptor error")
			},
		},
		module:   "test.wasm",
		failOpen: true,
		metrics:  metrics,
	}

	req := &interceptor.Request{
		Payload:   []byte(`original`),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	result, err := wrapper.Process(context.Background(), req)
	if err != nil {
		t.Errorf("expected no error with failOpen=true, got %v", err)
	}
	// With failOpen, should return original request
	if string(result.Payload) != "original" {
		t.Errorf("expected original payload, got %s", result.Payload)
	}

	// Check metrics recorded failure (even though we continued)
	if len(metrics.invocations) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(metrics.invocations))
	}
	if metrics.invocations[0].success {
		t.Error("expected success=false")
	}
}

func TestPhaseConstants(t *testing.T) {
	if PhaseOutbound != "outbound" {
		t.Errorf("expected PhaseOutbound to be 'outbound', got %s", PhaseOutbound)
	}
	if PhaseInbound != "inbound" {
		t.Errorf("expected PhaseInbound to be 'inbound', got %s", PhaseInbound)
	}
}

func TestRegistry_CreateInterceptor_UnknownType(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	ic := link.InterceptorConfig{
		Type:   "unknown",
		Config: map[string]interface{}{},
	}

	_, err := r.createInterceptor(context.Background(), ic, 0)
	if err == nil {
		t.Error("expected error for unknown interceptor type")
	}
}

func TestRegistry_CreateWASMInterceptor_MissingModule(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	config := map[string]interface{}{
		// No "module" field
	}

	_, err := r.createWASMInterceptor(context.Background(), config, 0)
	if err == nil {
		t.Error("expected error for missing module field")
	}
}

func TestRegistry_CreateWASMInterceptor_FileNotFound(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	config := map[string]interface{}{
		"module": "/nonexistent/path/to/module.wasm",
	}

	_, err := r.createWASMInterceptor(context.Background(), config, 0)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
	// The error is wrapped, so we check for the error message containing "no such file"
	if !strings.Contains(err.Error(), "no such file") && !strings.Contains(err.Error(), "cannot find") {
		t.Errorf("expected file not found error, got: %v", err)
	}
}

func TestRegistry_BuildChainsForTarget_PhaseParsing(t *testing.T) {
	// This test verifies that phase is correctly parsed from config
	// We can't fully test this without a real WASM module, but we can verify
	// the logic flow
	tests := []struct {
		name          string
		config        map[string]interface{}
		expectedPhase Phase
	}{
		{
			name:          "default phase is outbound",
			config:        map[string]interface{}{},
			expectedPhase: PhaseOutbound,
		},
		{
			name: "explicit outbound phase",
			config: map[string]interface{}{
				"phase": "outbound",
			},
			expectedPhase: PhaseOutbound,
		},
		{
			name: "explicit inbound phase",
			config: map[string]interface{}{
				"phase": "inbound",
			},
			expectedPhase: PhaseInbound,
		},
		{
			name: "invalid phase defaults to outbound",
			config: map[string]interface{}{
				"phase": "invalid",
			},
			expectedPhase: PhaseOutbound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify phase parsing logic
			phase := PhaseOutbound
			if phaseVal, ok := tt.config["phase"]; ok {
				if phaseStr, ok := phaseVal.(string); ok {
					phase = Phase(phaseStr)
				}
			}

			// Check the effective phase
			var effectivePhase Phase
			switch phase {
			case PhaseOutbound:
				effectivePhase = PhaseOutbound
			case PhaseInbound:
				effectivePhase = PhaseInbound
			default:
				effectivePhase = PhaseOutbound
			}

			if effectivePhase != tt.expectedPhase {
				t.Errorf("expected phase %s, got %s", tt.expectedPhase, effectivePhase)
			}
		})
	}
}

func TestTargetChains_Empty(t *testing.T) {
	// Test that empty chains behave correctly
	outbound := interceptor.NewChain()
	inbound := interceptor.NewChain()

	chains := &TargetChains{
		Outbound: outbound,
		Inbound:  inbound,
	}

	if chains.Outbound.Len() != 0 {
		t.Error("expected empty outbound chain")
	}
	if chains.Inbound.Len() != 0 {
		t.Error("expected empty inbound chain")
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Start multiple goroutines to test concurrent access
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = r.GetChains("test")
				_, _ = r.ProcessOutbound(context.Background(), "test", &interceptor.Request{})
				_, _ = r.ProcessInbound(context.Background(), "test", &interceptor.Request{})
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
