//go:build !nowasmer

package wasm

import (
	"context"
	"fmt"
	"os"
)

// DefaultFactory is the default runtime factory.
type DefaultFactory struct{}

// NewFactory creates a new runtime factory.
func NewFactory() *DefaultFactory {
	return &DefaultFactory{}
}

// Create creates a Runtime based on the configuration.
func (f *DefaultFactory) Create(ctx context.Context, cfg Config) (Runtime, error) {
	// Read WASM bytes from file
	wasmBytes, err := os.ReadFile(cfg.ModulePath)
	if err != nil {
		return nil, fmt.Errorf("read wasm module: %w", err)
	}

	switch cfg.Type {
	case RuntimeWazero, "": // default to wazero
		return NewWazeroRuntime(ctx, wasmBytes)
	case RuntimeWasmer:
		return NewWasmerRuntime(ctx, wasmBytes, cfg)
	default:
		return nil, fmt.Errorf("unknown runtime type: %s", cfg.Type)
	}
}

// CreateApp creates an AppRuntime for long-running applications.
func (f *DefaultFactory) CreateApp(ctx context.Context, cfg Config) (AppRuntime, error) {
	if cfg.Type != RuntimeWasmer {
		return nil, fmt.Errorf("app runtime requires wasmer, got: %s", cfg.Type)
	}

	wasmBytes, err := os.ReadFile(cfg.ModulePath)
	if err != nil {
		return nil, fmt.Errorf("read wasm module: %w", err)
	}

	return NewWasmerAppRuntime(ctx, wasmBytes, cfg)
}
