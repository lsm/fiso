//go:build wasmer

package wasm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWasmerRuntime_NewWasmerRuntime_ValidModule(t *testing.T) {
	ctx := context.Background()
	wasmPath := buildTestWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	cfg := Config{
		Type:       RuntimeWasmer,
		ModulePath: wasmPath,
	}

	rt, err := NewWasmerRuntime(ctx, wasmBytes, cfg)
	if err != nil {
		t.Fatalf("NewWasmerRuntime failed: %v", err)
	}
	defer rt.Close()

	if rt.Type() != RuntimeWasmer {
		t.Errorf("Type() = %q, want %q", rt.Type(), RuntimeWasmer)
	}
}

func TestWasmerRuntime_NewWasmerRuntime_InvalidWASM(t *testing.T) {
	ctx := context.Background()

	cfg := Config{
		Type:       RuntimeWasmer,
		ModulePath: "test.wasm",
	}

	_, err := NewWasmerRuntime(ctx, []byte("invalid wasm bytes"), cfg)
	if err == nil {
		t.Fatal("expected error for invalid WASM")
	}
}

func TestWasmerRuntime_Call_ValidInput(t *testing.T) {
	ctx := context.Background()
	wasmPath := buildTestWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	cfg := Config{
		Type:       RuntimeWasmer,
		ModulePath: wasmPath,
	}

	rt, err := NewWasmerRuntime(ctx, wasmBytes, cfg)
	if err != nil {
		t.Fatalf("NewWasmerRuntime failed: %v", err)
	}
	defer rt.Close()

	input := []byte(`{"payload":{"test":"data"},"headers":{},"direction":"inbound"}`)
	output, err := rt.Call(ctx, input)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if len(output) == 0 {
		t.Error("expected non-empty output")
	}
}

func TestWasmerRuntime_Call_CancelledContext(t *testing.T) {
	ctx := context.Background()
	wasmPath := buildTestWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	cfg := Config{
		Type:       RuntimeWasmer,
		ModulePath: wasmPath,
	}

	rt, err := NewWasmerRuntime(ctx, wasmBytes, cfg)
	if err != nil {
		t.Fatalf("NewWasmerRuntime failed: %v", err)
	}
	defer rt.Close()

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	input := []byte(`{"payload":{"test":"data"},"headers":{},"direction":"inbound"}`)
	_, err = rt.Call(ctx, input)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestWasmerRuntime_Call_WithTimeout(t *testing.T) {
	ctx := context.Background()
	wasmPath := buildTestWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	cfg := Config{
		Type:       RuntimeWasmer,
		ModulePath: wasmPath,
		Timeout:    5 * time.Second,
	}

	rt, err := NewWasmerRuntime(ctx, wasmBytes, cfg)
	if err != nil {
		t.Fatalf("NewWasmerRuntime failed: %v", err)
	}
	defer rt.Close()

	input := []byte(`{"payload":{"test":"data"},"headers":{},"direction":"inbound"}`)
	output, err := rt.Call(ctx, input)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if len(output) == 0 {
		t.Error("expected non-empty output")
	}
}

func TestWasmerRuntime_Close(t *testing.T) {
	ctx := context.Background()
	wasmPath := buildTestWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	cfg := Config{
		Type:       RuntimeWasmer,
		ModulePath: wasmPath,
	}

	rt, err := NewWasmerRuntime(ctx, wasmBytes, cfg)
	if err != nil {
		t.Fatalf("NewWasmerRuntime failed: %v", err)
	}

	// Close should succeed
	if err := rt.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Double close should not error
	if err := rt.Close(); err != nil {
		t.Errorf("Double Close failed: %v", err)
	}

	// Call after close should error
	input := []byte(`{"payload":{}}`)
	_, err = rt.Call(ctx, input)
	if err == nil {
		t.Error("expected error calling after close")
	}
}

func TestWasmerRuntime_Call_AfterClose(t *testing.T) {
	ctx := context.Background()
	wasmPath := buildTestWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	cfg := Config{
		Type:       RuntimeWasmer,
		ModulePath: wasmPath,
	}

	rt, err := NewWasmerRuntime(ctx, wasmBytes, cfg)
	if err != nil {
		t.Fatalf("NewWasmerRuntime failed: %v", err)
	}

	rt.Close()

	input := []byte(`{"payload":{}}`)
	_, err = rt.Call(ctx, input)
	if err == nil {
		t.Fatal("expected error calling after close")
	}
}
