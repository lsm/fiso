package wasm

import (
	"bytes"
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// WazeroRuntime implements Runtime using the wazero WebAssembly runtime.
// It compiles the module once and instantiates a fresh copy per Call.
type WazeroRuntime struct {
	rt     wazero.Runtime
	module wazero.CompiledModule
}

// NewWazeroRuntime compiles a WASM module from raw bytes.
// The module must be a WASI binary (wasip1) that reads JSON from stdin and writes JSON to stdout.
func NewWazeroRuntime(ctx context.Context, wasmBytes []byte) (*WazeroRuntime, error) {
	return newWazeroRuntime(ctx, wasmBytes, HostHTTPConfig{}, false)
}

// NewWazeroRuntimeWithHTTP compiles a WASM module and instantiates the
// fiso.http_call host function with the supplied allowlist (ADR 0006).
// Without WithHTTP the module has no such import: a module importing it
// fails to instantiate, so the capability is absent rather than unchecked.
func NewWazeroRuntimeWithHTTP(ctx context.Context, wasmBytes []byte, cfg HostHTTPConfig) (*WazeroRuntime, error) {
	return newWazeroRuntime(ctx, wasmBytes, cfg, true)
}

func newWazeroRuntime(ctx context.Context, wasmBytes []byte, httpCfg HostHTTPConfig, withHTTP bool) (*WazeroRuntime, error) {
	rt := wazero.NewRuntime(ctx)

	// Instantiate WASI so the module can use stdin/stdout.
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	if withHTTP {
		client, err := newHostHTTPClient(httpCfg)
		if err != nil {
			_ = rt.Close(ctx)
			return nil, err
		}
		builder := rt.NewHostModuleBuilder("fiso")
		hostHTTPExport(builder, client)
		if _, err := builder.Instantiate(ctx); err != nil {
			_ = rt.Close(ctx)
			return nil, fmt.Errorf("instantiate fiso host module: %w", err)
		}
	}

	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("compile wasm module: %w", err)
	}

	return &WazeroRuntime{rt: rt, module: compiled}, nil
}

// Call invokes the WASM module with input on stdin and captures stdout as the result.
func (w *WazeroRuntime) Call(ctx context.Context, input []byte) ([]byte, error) {
	stdin := bytes.NewReader(input)
	var stdout bytes.Buffer

	cfg := wazero.NewModuleConfig().
		WithStdin(stdin).
		WithStdout(&stdout).
		WithStderr(&bytes.Buffer{}). // discard stderr
		WithName("")                 // anonymous module so multiple calls don't collide

	mod, err := w.rt.InstantiateModule(ctx, w.module, cfg)
	if err != nil {
		// If the module wrote output before failing, return it with the error
		// so the caller can inspect partial output if needed.
		if stdout.Len() > 0 {
			return stdout.Bytes(), fmt.Errorf("wasm execution: %w", err)
		}
		return nil, fmt.Errorf("wasm execution: %w", err)
	}
	_ = mod.Close(ctx)

	return stdout.Bytes(), nil
}

// Close releases all wazero resources.
func (w *WazeroRuntime) Close() error {
	return w.rt.Close(context.Background())
}
