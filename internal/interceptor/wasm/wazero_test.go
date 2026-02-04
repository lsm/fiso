package wasm

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsm/fiso/internal/interceptor"
)

// buildWASMModule compiles a Go source file to a WASM binary using wasip1/wasm.
// Returns the path to the compiled .wasm file.
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

func TestWazeroRuntime_HappyPath(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	input := wasmInput{
		Payload:   json.RawMessage(`{"order_id":"abc"}`),
		Headers:   map[string]string{"Content-Type": "application/json"},
		Direction: "inbound",
	}
	inputBytes, _ := json.Marshal(input)

	result, err := rt.Call(ctx, inputBytes)
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	var output wasmOutput
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("unmarshal output: %v\nraw: %s", err, string(result))
	}

	// Verify the module enriched the payload
	var data map[string]interface{}
	if err := json.Unmarshal(output.Payload, &data); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if data["wasm_enriched"] != true {
		t.Errorf("expected wasm_enriched=true, got %v", data["wasm_enriched"])
	}
	if data["order_id"] != "abc" {
		t.Errorf("expected order_id=abc, got %v", data["order_id"])
	}

	// Verify header was added
	if output.Headers["X-WASM-Processed"] != "true" {
		t.Errorf("expected X-WASM-Processed header, got %v", output.Headers)
	}
}

func TestWazeroRuntime_MultipleCalls(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	for i := 0; i < 3; i++ {
		input := wasmInput{
			Payload:   json.RawMessage(`{"call":` + string(rune('0'+i)) + `}`),
			Headers:   map[string]string{},
			Direction: "inbound",
		}
		inputBytes, _ := json.Marshal(input)

		result, err := rt.Call(ctx, inputBytes)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}

		var output wasmOutput
		if err := json.Unmarshal(result, &output); err != nil {
			t.Fatalf("call %d unmarshal: %v\nraw: %s", i, err, string(result))
		}
	}
}

func TestWazeroRuntime_InvalidModule(t *testing.T) {
	ctx := context.Background()
	_, err := NewWazeroRuntime(ctx, []byte("not a wasm module"))
	if err == nil {
		t.Fatal("expected error for invalid WASM bytes")
	}
	if !strings.Contains(err.Error(), "compile wasm module") {
		t.Errorf("expected compile error, got %q", err.Error())
	}
}

func TestWazeroRuntime_Close(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestWazeroRuntime_EndToEnd_WithInterceptor(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	// Use the Interceptor wrapper (same as production code)
	ic := New(rt, "test-enrich-module")

	req := &interceptor.Request{
		Payload:   []byte(`{"user":"alice"}`),
		Headers:   map[string]string{"X-Request-ID": "req-1"},
		Direction: interceptor.Inbound,
	}

	result, err := ic.Process(ctx, req)
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result.Payload, &data); err != nil {
		t.Fatalf("unmarshal result payload: %v", err)
	}
	if data["wasm_enriched"] != true {
		t.Errorf("expected wasm_enriched=true, got %v", data)
	}
	if data["user"] != "alice" {
		t.Errorf("expected user=alice, got %v", data)
	}

	// Original header should be preserved, new one added
	if result.Headers["X-Request-ID"] != "req-1" {
		t.Errorf("expected X-Request-ID preserved, got %v", result.Headers)
	}
	if result.Headers["X-WASM-Processed"] != "true" {
		t.Errorf("expected X-WASM-Processed header, got %v", result.Headers)
	}

	if err := ic.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestCall_WASMExitError(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("testdata", "exit-error"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	input := wasmInput{
		Payload:   json.RawMessage(`{"test":"data"}`),
		Headers:   map[string]string{},
		Direction: "inbound",
	}
	inputBytes, _ := json.Marshal(input)

	_, err = rt.Call(ctx, inputBytes)
	if err == nil {
		t.Fatal("expected error when WASM module exits with error code")
	}
}

func TestCall_EmptyOutput(t *testing.T) {
	wasmPath := buildWASMModule(t, filepath.Join("testdata", "empty-output"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	ctx := context.Background()
	rt, err := NewWazeroRuntime(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	input := wasmInput{
		Payload:   json.RawMessage(`{"test":"data"}`),
		Headers:   map[string]string{},
		Direction: "inbound",
	}
	inputBytes, _ := json.Marshal(input)

	result, err := rt.Call(ctx, inputBytes)
	if err != nil {
		t.Fatalf("expected no error for empty output module, got: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d bytes: %s", len(result), result)
	}
}
