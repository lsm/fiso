//go:build !wasmer

package wasm

import (
	"context"
	"testing"
)

func TestWasmerRuntime_New(t *testing.T) {
	ctx := context.Background()
	_, err := NewWasmerRuntime(ctx, []byte{}, Config{})
	if err == nil {
		t.Fatal("expected error when creating WasmerRuntime without wasmer build tag")
	}
	if err.Error() != "wasmer runtime requires building with -tags wasmer" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestWasmerRuntime_Call(t *testing.T) {
	rt := &WasmerRuntime{}
	_, err := rt.Call(context.Background(), []byte("input"))
	if err == nil {
		t.Fatal("expected error when calling WasmerRuntime without wasmer build tag")
	}
	if err.Error() != "wasmer runtime not available" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestWasmerRuntime_Close(t *testing.T) {
	rt := &WasmerRuntime{}
	err := rt.Close()
	if err != nil {
		t.Errorf("unexpected error on Close: %v", err)
	}
}

func TestWasmerRuntime_Type(t *testing.T) {
	rt := &WasmerRuntime{}
	if rt.Type() != RuntimeWasmer {
		t.Errorf("Type() = %q, want %q", rt.Type(), RuntimeWasmer)
	}
}

func TestWasmerAppRuntime_New(t *testing.T) {
	ctx := context.Background()
	_, err := NewWasmerAppRuntime(ctx, []byte{}, Config{})
	if err == nil {
		t.Fatal("expected error when creating WasmerAppRuntime without wasmer build tag")
	}
	if err.Error() != "wasmer app runtime requires building with -tags wasmer" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestWasmerAppRuntime_Start(t *testing.T) {
	rt := &WasmerAppRuntime{}
	_, err := rt.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when starting WasmerAppRuntime without wasmer build tag")
	}
	if err.Error() != "wasmer runtime not available" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestWasmerAppRuntime_Stop(t *testing.T) {
	rt := &WasmerAppRuntime{}
	err := rt.Stop(context.Background())
	if err != nil {
		t.Errorf("unexpected error on Stop: %v", err)
	}
}

func TestWasmerAppRuntime_Addr(t *testing.T) {
	rt := &WasmerAppRuntime{}
	if rt.Addr() != "" {
		t.Errorf("Addr() = %q, want empty string", rt.Addr())
	}
}

func TestWasmerAppRuntime_IsRunning(t *testing.T) {
	rt := &WasmerAppRuntime{}
	if rt.IsRunning() {
		t.Error("IsRunning() = true, want false")
	}
}

func TestWasmerAppRuntime_Call(t *testing.T) {
	rt := &WasmerAppRuntime{}
	_, err := rt.Call(context.Background(), []byte("input"))
	if err == nil {
		t.Fatal("expected error when calling WasmerAppRuntime without wasmer build tag")
	}
	if err.Error() != "wasmer runtime not available" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestWasmerAppRuntime_Close(t *testing.T) {
	rt := &WasmerAppRuntime{}
	err := rt.Close()
	if err != nil {
		t.Errorf("unexpected error on Close: %v", err)
	}
}

func TestWasmerAppRuntime_Type(t *testing.T) {
	rt := &WasmerAppRuntime{}
	if rt.Type() != RuntimeWasmer {
		t.Errorf("Type() = %q, want %q", rt.Type(), RuntimeWasmer)
	}
}
