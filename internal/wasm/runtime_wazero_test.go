//go:build !nowasmer

package wasm

import (
	"context"
	"encoding/json"
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
	defer rt.Close()

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
	defer rt.Close()

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
	defer rt.Close()

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
