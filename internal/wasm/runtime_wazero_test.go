//go:build !nowasmer

package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
