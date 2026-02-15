//go:build !wasmer

package wasm

import (
	"context"
	"errors"
)

// WasmerRuntime stub for non-wasmer builds.
type WasmerRuntime struct{}

func NewWasmerRuntime(ctx context.Context, wasmBytes []byte, cfg Config) (*WasmerRuntime, error) {
	return nil, errors.New("wasmer runtime requires building with -tags wasmer")
}

func (w *WasmerRuntime) Call(ctx context.Context, input []byte) ([]byte, error) {
	return nil, errors.New("wasmer runtime not available")
}

func (w *WasmerRuntime) Close() error { return nil }

func (w *WasmerRuntime) Type() RuntimeType { return RuntimeWasmer }

// WasmerAppRuntime stub for non-wasmer builds.
type WasmerAppRuntime struct{}

func NewWasmerAppRuntime(ctx context.Context, wasmBytes []byte, cfg Config) (*WasmerAppRuntime, error) {
	return nil, errors.New("wasmer app runtime requires building with -tags wasmer")
}

func (w *WasmerAppRuntime) Start(ctx context.Context) (string, error) {
	return "", errors.New("wasmer runtime not available")
}

func (w *WasmerAppRuntime) Stop(ctx context.Context) error { return nil }

func (w *WasmerAppRuntime) Addr() string { return "" }

func (w *WasmerAppRuntime) IsRunning() bool { return false }

func (w *WasmerAppRuntime) Call(ctx context.Context, input []byte) ([]byte, error) {
	return nil, errors.New("wasmer runtime not available")
}

func (w *WasmerAppRuntime) Close() error { return nil }

func (w *WasmerAppRuntime) Type() RuntimeType { return RuntimeWasmer }
