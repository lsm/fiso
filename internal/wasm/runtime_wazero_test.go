//go:build !nowasmer

package wasm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildWASMModule compiles a Go source file to a WASM binary using wasip1/wasm.
func buildWASMModule(t *testing.T, srcDir string) string {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "module.wasm")
	cmd := exec.Command("go", "build", "-o", outPath, ".")
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compile wasm module: %v\n%s", err, out)
	}
	return outPath
}

func TestWazeroRuntime_New(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime failed: %v", err)
	}
	defer func() { _ = rt.Close() }()

	if rt.Type() != RuntimeWazero {
		t.Errorf("Type() = %q, want %q", rt.Type(), RuntimeWazero)
	}
}

func TestWazeroRuntime_Call(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime failed: %v", err)
	}
	defer func() { _ = rt.Close() }()

	// The WASM module expects input in the format: {"payload": ..., "headers": ..., "direction": ...}
	input := map[string]interface{}{
		"payload":   json.RawMessage(`{"test":"data"}`),
		"headers":   map[string]string{"X-Test": "value"},
		"direction": "inbound",
	}
	inputBytes, _ := json.Marshal(input)

	output, err := rt.Call(ctx, inputBytes)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if len(output) == 0 {
		t.Error("expected non-empty output")
	}

	// Verify the output contains wasm_enriched
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}
	payload, ok := result["payload"].(map[string]interface{})
	if !ok {
		t.Fatal("expected payload in output")
	}
	if payload["wasm_enriched"] != true {
		t.Error("expected wasm_enriched to be true")
	}
}

func TestWazeroRuntime_Close(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime failed: %v", err)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestWazeroRuntime_InvalidWASM(t *testing.T) {
	ctx := context.Background()
	_, err := NewWazeroRuntime(ctx, []byte("not valid wasm"))
	if err == nil {
		t.Fatal("expected error for invalid WASM bytes")
	}
	if !strings.Contains(err.Error(), "compile wasm module") {
		t.Errorf("expected compile error, got: %v", err)
	}
}

func TestWazeroRuntime_MultipleCalls(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime failed: %v", err)
	}
	defer func() { _ = rt.Close() }()

	for i := 0; i < 3; i++ {
		input := map[string]interface{}{
			"payload":   json.RawMessage(`{"call":` + string(rune('0'+i)) + `}`),
			"headers":   map[string]string{},
			"direction": "inbound",
		}
		inputBytes, _ := json.Marshal(input)

		_, err := rt.Call(ctx, inputBytes)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i, err)
		}
	}
}

func TestWazeroRuntime_EmptyOutput(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "empty-output"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime failed: %v", err)
	}
	defer func() { _ = rt.Close() }()

	input := map[string]interface{}{
		"payload":   json.RawMessage(`{"test":"data"}`),
		"headers":   map[string]string{},
		"direction": "inbound",
	}
	inputBytes, _ := json.Marshal(input)

	output, err := rt.Call(ctx, inputBytes)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// Empty output is valid
	if len(output) != 0 {
		t.Errorf("expected empty output, got %q", output)
	}
}

func TestWazeroRuntime_ExitError(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "exit-error"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime failed: %v", err)
	}
	defer func() { _ = rt.Close() }()

	_, err = rt.Call(ctx, []byte("{}"))
	if err == nil {
		t.Fatal("expected error for module that exits with non-zero")
	}
}

func TestWazeroRuntime_PartialOutput(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "partial-output"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime failed: %v", err)
	}
	defer func() { _ = rt.Close() }()

	output, err := rt.Call(ctx, []byte("{}"))
	// Partial output should be returned even on error
	if err == nil {
		t.Fatal("expected error for module that exits with non-zero")
	}
	if string(output) != `{"partial":"data"}` {
		t.Errorf("expected partial output, got %q", output)
	}
}

func TestWazeroRuntime_CallWithCancelledContext(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime failed: %v", err)
	}
	defer func() { _ = rt.Close() }()

	// Create a cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	input := map[string]interface{}{
		"payload":   json.RawMessage(`{"test":"data"}`),
		"headers":   map[string]string{},
		"direction": "inbound",
	}
	inputBytes, _ := json.Marshal(input)

	// Note: wazero may still execute the module if cancellation is detected late
	// The key test is that Call() doesn't panic or hang with a cancelled context
	_, _ = rt.Call(cancelledCtx, inputBytes)
	// We just verify no panic occurred
}

func TestWazeroRuntime_DoubleClose(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime failed: %v", err)
	}

	// First close should succeed
	if err := rt.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// Second close should also succeed (wazero handles this gracefully)
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

func TestWazeroRuntime_EmptyInput(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime failed: %v", err)
	}
	defer func() { _ = rt.Close() }()

	// Call with empty input - the enrich module expects valid JSON, so it may fail
	// This tests that Call handles empty input gracefully (no panic)
	_, _ = rt.Call(ctx, []byte{})
	// We just verify no panic occurred
}

func TestWazeroRuntime_LargeInput(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime failed: %v", err)
	}
	defer func() { _ = rt.Close() }()

	// Create a large payload
	largePayload := make(map[string]string)
	for i := 0; i < 100; i++ {
		largePayload[fmt.Sprintf("key_%d", i)] = fmt.Sprintf("value_%d", i)
	}
	input := map[string]interface{}{
		"payload":   largePayload,
		"headers":   map[string]string{"X-Large": "request"},
		"direction": "inbound",
	}
	inputBytes, _ := json.Marshal(input)

	output, err := rt.Call(ctx, inputBytes)
	if err != nil {
		t.Fatalf("Call with large input failed: %v", err)
	}
	if len(output) == 0 {
		t.Error("expected non-empty output for large input")
	}
}

// TestWazeroRuntime_GuestClockFollowsHost pins the guest-clock contract:
// wazero's sandbox default is a frozen fake wall clock, which would make a
// time-dependent guest (JWT exp/nbf verification) silently accept expired
// credentials. The guest must see the real host time.
func TestWazeroRuntime_GuestClockFollowsHost(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("testdata", "clock"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	rt, err := NewWazeroRuntime(context.Background(), wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	before := time.Now().Unix()
	out, err := rt.Call(context.Background(), []byte(`{}`))
	after := time.Now().Unix()
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var report struct {
		Now int64 `json:"now"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("parse guest report %s: %v", out, err)
	}
	// The guest ran between the two host readings; anything outside that
	// window (within tolerance) is a frozen or skewed clock.
	if report.Now < before-300 || report.Now > after+300 {
		t.Fatalf("guest clock %d is outside the host window [%d, %d] — sandbox default clock?", report.Now, before-300, after+300)
	}
}

// TestWazeroRuntime_StderrSurfacedOnError pins the guest-stderr contract:
// a failing guest's diagnostic must reach the operator. The auth guest's
// misconfiguration message ("auth: configuration: ...") is its only
// diagnostic; discarding stderr leaves a generic exit error that cannot
// be acted on.
func TestWazeroRuntime_StderrSurfacedOnError(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("testdata", "stderr-fail"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	rt, err := NewWazeroRuntime(context.Background(), wasmBytes)
	if err != nil {
		t.Fatalf("NewWazeroRuntime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	_, err = rt.Call(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("expected execution error")
	}
	if !strings.Contains(err.Error(), "auth-config-boom") {
		t.Fatalf("error must surface the guest's stderr diagnostic, got: %v", err)
	}
}

// TestLimitWriter_BoundsCapture pins the stderr bound: writes beyond the
// cap are dropped while being captured, not truncated after the fact, so
// a guest streaming unbounded diagnostics cannot grow host memory.
func TestLimitWriter_BoundsCapture(t *testing.T) {
	w := &limitWriter{limit: 16}
	big := make([]byte, 1<<20) // 1 MiB
	for i := range big {
		big[i] = 'x'
	}
	for i := 0; i < 8; i++ {
		if n, err := w.Write(big); err != nil || n != len(big) {
			t.Fatalf("write %d: n=%d err=%v (the guest must not be blocked)", i, n, err)
		}
	}
	if w.buf.Len() > 16 {
		t.Fatalf("retained %d bytes, want at most 16", w.buf.Len())
	}
	if !w.truncated {
		t.Fatal("truncation must be reported")
	}

	small := &limitWriter{limit: 16}
	if _, err := small.Write([]byte("short message")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if small.truncated {
		t.Fatal("under-limit write must not be marked truncated")
	}
	if small.buf.String() != "short message" {
		t.Fatalf("content = %q", small.buf.String())
	}
}

// TestWazeroRuntime_StderrTruncatedInError pins that an over-limit guest
// stderr is capped in the surfaced error.
func TestWazeroRuntime_StderrTruncatedInError(t *testing.T) {
	w := &limitWriter{limit: 8}
	_, _ = w.Write([]byte("0123456789ABCDEF"))
	err := withGuestStderr(errors.New("boom"), w)
	if !strings.Contains(err.Error(), "guest stderr: 01234567…") {
		t.Fatalf("capped stderr missing; got: %v", err)
	}
}
