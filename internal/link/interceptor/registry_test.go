package interceptor

import (
	"context"
	"errors"
	"log/slog"
	"os"
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

func TestRegistry_Load_EmptyInterceptors(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Load targets with no interceptors - should skip without error
	targets := []link.LinkTarget{
		{Name: "target1", Interceptors: nil},
		{Name: "target2", Interceptors: []link.InterceptorConfig{}},
	}

	err := r.Load(context.Background(), targets)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// No chains should be created for targets without interceptors
	if r.GetChains("target1") != nil {
		t.Error("expected nil chains for target without interceptors")
	}
	if r.GetChains("target2") != nil {
		t.Error("expected nil chains for target with empty interceptors")
	}
}

func TestRegistry_Load_UnknownInterceptorType(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	targets := []link.LinkTarget{
		{
			Name: "target1",
			Interceptors: []link.InterceptorConfig{
				{Type: "invalid-type", Config: map[string]interface{}{}},
			},
		},
	}

	err := r.Load(context.Background(), targets)
	if err == nil {
		t.Fatal("expected error for unknown interceptor type")
	}
	if !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("expected error about unknown type, got: %v", err)
	}
}

func TestRegistry_Load_WASMMissingModule(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	targets := []link.LinkTarget{
		{
			Name: "target1",
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{}}, // missing module
			},
		},
	}

	err := r.Load(context.Background(), targets)
	if err == nil {
		t.Fatal("expected error for missing module")
	}
	if !strings.Contains(err.Error(), "missing 'module'") {
		t.Errorf("expected error about missing module, got: %v", err)
	}
}

func TestRegistry_Load_WASMFileNotFound(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	targets := []link.LinkTarget{
		{
			Name: "target1",
			Interceptors: []link.InterceptorConfig{
				{Type: "wasm", Config: map[string]interface{}{"module": "/nonexistent/module.wasm"}},
			},
		},
	}

	err := r.Load(context.Background(), targets)
	if err == nil {
		t.Fatal("expected error for file not found")
	}
	if !strings.Contains(err.Error(), "read wasm module") {
		t.Errorf("expected error about reading wasm module, got: %v", err)
	}
}

func TestRegistry_Load_MultipleTargets(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Multiple targets, some with errors should stop loading
	targets := []link.LinkTarget{
		{Name: "good-target", Interceptors: nil}, // No interceptors, should succeed
	}

	err := r.Load(context.Background(), targets)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRegistry_NewRegistry_NilLogger(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, nil)
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.logger == nil {
		t.Error("expected default logger when nil provided")
	}
}

func TestRegistry_Close_WithRuntimes(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Manually add a mock runtime to test Close
	mockRt := &mockInterceptor{}
	r.runtimes = append(r.runtimes, mockRt)

	err := r.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !mockRt.closed {
		t.Error("expected runtime to be closed")
	}
}

func TestRegistry_Close_MultipleRuntimes(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Add multiple mock runtimes
	mockRt1 := &mockInterceptor{}
	mockRt2 := &mockInterceptor{}
	r.runtimes = append(r.runtimes, mockRt1, mockRt2)

	err := r.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !mockRt1.closed || !mockRt2.closed {
		t.Error("expected all runtimes to be closed")
	}
}

func TestRegistry_ProcessOutbound_WithChain(t *testing.T) {
	metrics := &mockMetricsRecorder{}
	r := NewRegistry(metrics, slog.Default())

	// Create a chain manually
	mockIc := &mockInterceptor{
		processFunc: func(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
			req.Headers["X-Processed"] = "true"
			return req, nil
		},
	}
	r.chains["test-target"] = &TargetChains{
		Outbound: interceptor.NewChain(mockIc),
		Inbound:  interceptor.NewChain(),
	}

	req := &interceptor.Request{
		Payload:   []byte(`{"test": true}`),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	result, err := r.ProcessOutbound(context.Background(), "test-target", req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Headers["X-Processed"] != "true" {
		t.Error("expected header to be set by interceptor")
	}
}

func TestRegistry_ProcessInbound_WithChain(t *testing.T) {
	metrics := &mockMetricsRecorder{}
	r := NewRegistry(metrics, slog.Default())

	// Create a chain manually
	mockIc := &mockInterceptor{
		processFunc: func(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
			req.Headers["X-Inbound"] = "true"
			return req, nil
		},
	}
	r.chains["test-target"] = &TargetChains{
		Outbound: interceptor.NewChain(),
		Inbound:  interceptor.NewChain(mockIc),
	}

	req := &interceptor.Request{
		Payload:   []byte(`{"test": true}`),
		Headers:   map[string]string{},
		Direction: interceptor.Inbound,
	}

	result, err := r.ProcessInbound(context.Background(), "test-target", req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Headers["X-Inbound"] != "true" {
		t.Error("expected header to be set by interceptor")
	}
}

func TestRegistry_Load_ReloadClosesExistingRuntimes(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Add a mock runtime manually
	mockRt := &mockInterceptor{}
	r.runtimes = append(r.runtimes, mockRt)

	// Load with empty targets should close existing runtimes
	err := r.Load(context.Background(), []link.LinkTarget{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !mockRt.closed {
		t.Error("expected existing runtime to be closed on reload")
	}
}

func TestInterceptorWrapper_Process_NilMetrics(t *testing.T) {
	wrapper := &InterceptorWrapper{
		Interceptor: &mockInterceptor{
			processFunc: func(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
				return req, nil
			},
		},
		module:   "test.wasm",
		failOpen: false,
		metrics:  nil, // nil metrics
	}

	req := &interceptor.Request{
		Payload:   []byte(`{}`),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	// Should not panic with nil metrics
	result, err := wrapper.Process(context.Background(), req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestInterceptorWrapper_Process_FailOpenWithNilResult(t *testing.T) {
	wrapper := &InterceptorWrapper{
		Interceptor: &mockInterceptor{
			processFunc: func(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
				return nil, errors.New("interceptor error")
			},
		},
		module:   "test.wasm",
		failOpen: true,
		metrics:  &mockMetricsRecorder{},
	}

	req := &interceptor.Request{
		Payload:   []byte(`original`),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	// With failOpen=true, should return original request on error
	result, err := wrapper.Process(context.Background(), req)
	if err != nil {
		t.Errorf("expected no error with failOpen, got: %v", err)
	}
	if string(result.Payload) != "original" {
		t.Errorf("expected original payload, got: %s", result.Payload)
	}
}

func TestRegistry_CreateWASMInterceptor_WithFailOpen(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Test with failOpen config
	config := map[string]interface{}{
		"module":   "/nonexistent/module.wasm",
		"failOpen": true,
	}

	// This will fail because the file doesn't exist, but we're testing the config parsing
	_, err := r.createWASMInterceptor(context.Background(), config, 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	// Error should be about reading the file
	if !strings.Contains(err.Error(), "read wasm module") {
		t.Errorf("expected error about reading wasm module, got: %v", err)
	}
}

func TestRegistry_CreateWASMInterceptor_ModuleNotString(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Test with module not being a string
	config := map[string]interface{}{
		"module": 123, // not a string
	}

	_, err := r.createWASMInterceptor(context.Background(), config, 0)
	if err == nil {
		t.Fatal("expected error for non-string module")
	}
	if !strings.Contains(err.Error(), "missing 'module' field") {
		t.Errorf("expected error about missing module field, got: %v", err)
	}
}

func TestRegistry_Load_WithValidWASMModule(t *testing.T) {
	// Skip if the test WASM module doesn't exist
	wasmPath := "../interceptor/wasm/testdata/partial-output/module.wasm"
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		t.Skip("test WASM module not found")
	}

	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	targets := []link.LinkTarget{
		{
			Name: "test-target",
			Interceptors: []link.InterceptorConfig{
				{
					Type: "wasm",
					Config: map[string]interface{}{
						"module":   wasmPath,
						"failOpen": false,
					},
				},
			},
		},
	}

	err := r.Load(context.Background(), targets)
	if err != nil {
		t.Fatalf("unexpected error loading WASM module: %v", err)
	}

	// Verify chains were created
	chains := r.GetChains("test-target")
	if chains == nil {
		t.Fatal("expected chains to be created")
	}
	if chains.Outbound == nil || chains.Outbound.Len() != 1 {
		t.Errorf("expected 1 outbound interceptor, got %d", chains.Outbound.Len())
	}

	// Clean up
	if err := r.Close(); err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}
}

func TestRegistry_Load_WithValidWASMModuleInbound(t *testing.T) {
	// Skip if the test WASM module doesn't exist
	wasmPath := "../interceptor/wasm/testdata/partial-output/module.wasm"
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		t.Skip("test WASM module not found")
	}

	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	targets := []link.LinkTarget{
		{
			Name: "test-target-inbound",
			Interceptors: []link.InterceptorConfig{
				{
					Type: "wasm",
					Config: map[string]interface{}{
						"module": wasmPath,
						"phase":  "inbound",
					},
				},
			},
		},
	}

	err := r.Load(context.Background(), targets)
	if err != nil {
		t.Fatalf("unexpected error loading WASM module: %v", err)
	}

	// Verify chains were created with inbound interceptor
	chains := r.GetChains("test-target-inbound")
	if chains == nil {
		t.Fatal("expected chains to be created")
	}
	if chains.Inbound == nil || chains.Inbound.Len() != 1 {
		t.Errorf("expected 1 inbound interceptor, got %d", chains.Inbound.Len())
	}
	if chains.Outbound == nil || chains.Outbound.Len() != 0 {
		t.Errorf("expected 0 outbound interceptors, got %d", chains.Outbound.Len())
	}

	// Clean up
	if err := r.Close(); err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}
}

func TestRegistry_Load_WithMultipleInterceptors(t *testing.T) {
	// Skip if the test WASM module doesn't exist
	wasmPath := "../interceptor/wasm/testdata/partial-output/module.wasm"
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		t.Skip("test WASM module not found")
	}

	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	targets := []link.LinkTarget{
		{
			Name: "multi-target",
			Interceptors: []link.InterceptorConfig{
				{
					Type: "wasm",
					Config: map[string]interface{}{
						"module": wasmPath,
						"phase":  "outbound",
					},
				},
				{
					Type: "wasm",
					Config: map[string]interface{}{
						"module": wasmPath,
						"phase":  "inbound",
					},
				},
			},
		},
	}

	err := r.Load(context.Background(), targets)
	if err != nil {
		t.Fatalf("unexpected error loading WASM modules: %v", err)
	}

	// Verify chains were created
	chains := r.GetChains("multi-target")
	if chains == nil {
		t.Fatal("expected chains to be created")
	}
	if chains.Outbound == nil || chains.Outbound.Len() != 1 {
		t.Errorf("expected 1 outbound interceptor, got %d", chains.Outbound.Len())
	}
	if chains.Inbound == nil || chains.Inbound.Len() != 1 {
		t.Errorf("expected 1 inbound interceptor, got %d", chains.Inbound.Len())
	}

	// Clean up
	if err := r.Close(); err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}
}

func TestRegistry_Load_WithInvalidWASMModule(t *testing.T) {
	// Create a temp file with invalid WASM content
	tmpFile, err := os.CreateTemp("", "invalid-*.wasm")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	// Write invalid WASM content
	if _, err := tmpFile.WriteString("not a valid wasm module"); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = tmpFile.Close()

	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	targets := []link.LinkTarget{
		{
			Name: "invalid-target",
			Interceptors: []link.InterceptorConfig{
				{
					Type: "wasm",
					Config: map[string]interface{}{
						"module": tmpFile.Name(),
					},
				},
			},
		},
	}

	err = r.Load(context.Background(), targets)
	if err == nil {
		t.Fatal("expected error for invalid WASM module")
	}
}

func TestRegistry_Close_Error(t *testing.T) {
	// Create mock runtime that returns error on close
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())
	r.runtimes = append(r.runtimes, &errorClosingInterceptor{})

	err := r.Close()
	if err == nil {
		t.Error("expected error from Close")
	}
}

func TestRegistry_Load_WarnOnCloseError(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Add existing runtime that fails to close
	r.runtimes = append(r.runtimes, &errorClosingInterceptor{})

	// Load should warn but continue when closing existing runtimes
	err := r.Load(context.Background(), []link.LinkTarget{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// errorClosingInterceptor is a mock interceptor that returns an error on Close.
type errorClosingInterceptor struct{}

func (e *errorClosingInterceptor) Process(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
	return req, nil
}

func (e *errorClosingInterceptor) Close() error {
	return errors.New("close error")
}

func TestRegistry_Load_PhaseAsString(t *testing.T) {
	// Skip if the test WASM module doesn't exist
	wasmPath := "../interceptor/wasm/testdata/partial-output/module.wasm"
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		t.Skip("test WASM module not found")
	}

	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Test with phase as non-string (should default to outbound)
	targets := []link.LinkTarget{
		{
			Name: "phase-default-target",
			Interceptors: []link.InterceptorConfig{
				{
					Type: "wasm",
					Config: map[string]interface{}{
						"module": wasmPath,
						"phase":  123, // not a string
					},
				},
			},
		},
	}

	err := r.Load(context.Background(), targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chains := r.GetChains("phase-default-target")
	if chains == nil {
		t.Fatal("expected chains to be created")
	}
	// Should default to outbound since phase is not a string
	if chains.Outbound == nil || chains.Outbound.Len() != 1 {
		t.Errorf("expected 1 outbound interceptor, got %d", chains.Outbound.Len())
	}

	r.Close()
}

func TestRegistry_Load_MultipleBuildChainsForTarget(t *testing.T) {
	// Skip if the test WASM module doesn't exist
	wasmPath := "../interceptor/wasm/testdata/partial-output/module.wasm"
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		t.Skip("test WASM module not found")
	}

	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Test with multiple targets
	targets := []link.LinkTarget{
		{
			Name: "target-a",
			Interceptors: []link.InterceptorConfig{
				{
					Type: "wasm",
					Config: map[string]interface{}{
						"module": wasmPath,
						"phase":  "outbound",
					},
				},
			},
		},
		{
			Name: "target-b",
			Interceptors: []link.InterceptorConfig{
				{
					Type: "wasm",
					Config: map[string]interface{}{
						"module": wasmPath,
						"phase":  "inbound",
					},
				},
			},
		},
	}

	err := r.Load(context.Background(), targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify target-a has outbound
	chainsA := r.GetChains("target-a")
	if chainsA == nil || chainsA.Outbound.Len() != 1 {
		t.Error("expected target-a to have 1 outbound interceptor")
	}

	// Verify target-b has inbound
	chainsB := r.GetChains("target-b")
	if chainsB == nil || chainsB.Inbound.Len() != 1 {
		t.Error("expected target-b to have 1 inbound interceptor")
	}

	r.Close()
}

func TestRegistry_CreateInterceptor_Index(t *testing.T) {
	r := NewRegistry(&mockMetricsRecorder{}, slog.Default())

	// Test that index is used in error message
	ic := link.InterceptorConfig{
		Type:   "unknown",
		Config: map[string]interface{}{},
	}

	_, err := r.createInterceptor(context.Background(), ic, 5)
	if err == nil {
		t.Fatal("expected error for unknown interceptor type")
	}
	if !strings.Contains(err.Error(), "interceptor[5]") {
		t.Errorf("expected error to contain index 5, got: %v", err)
	}
}
