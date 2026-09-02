//go:build !nowasmer

package wasm

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// WazeroRuntime implements Runtime using the wazero WebAssembly runtime.
// It compiles the module once and instantiates a fresh copy per Call.
type WazeroRuntime struct {
	rt     wazero.Runtime
	module wazero.CompiledModule
	env    map[string]string
}

// NewWazeroRuntime compiles a WASM module from raw bytes.
// The module must be a WASI binary (wasip1) that reads JSON from stdin and writes JSON to stdout.
func NewWazeroRuntime(ctx context.Context, wasmBytes []byte) (*WazeroRuntime, error) {
	return newWazeroRuntime(ctx, wasmBytes, nil, nil)
}

// NewWazeroRuntimeWithHTTP additionally instantiates the fiso.http_call
// host function with the supplied allowlist (ADR 0006).
func NewWazeroRuntimeWithHTTP(ctx context.Context, wasmBytes []byte, cfg HostHTTPConfig) (*WazeroRuntime, error) {
	return newWazeroRuntime(ctx, wasmBytes, &cfg, nil)
}

// WazeroOptions configures the optional capabilities of a WazeroRuntime.
type WazeroOptions struct {
	// Env is delivered to the guest as environment variables on every
	// invocation (ADR 0008) — the channel for key material such as JWT
	// verification keys.
	Env map[string]string

	// HostHTTP enables the fiso.http_call host function with the supplied
	// allowlist (ADR 0006). Nil disables the capability entirely.
	HostHTTP *HostHTTPConfig
}

// NewWazeroRuntimeWithOptions compiles a WASM module with env delivery and
// optionally the host HTTP capability combined.
func NewWazeroRuntimeWithOptions(ctx context.Context, wasmBytes []byte, opts WazeroOptions) (*WazeroRuntime, error) {
	return newWazeroRuntime(ctx, wasmBytes, opts.HostHTTP, opts.Env)
}

func newWazeroRuntime(ctx context.Context, wasmBytes []byte, httpCfg *HostHTTPConfig, env map[string]string) (*WazeroRuntime, error) {
	rt := wazero.NewRuntime(ctx)

	// Instantiate WASI so the module can use stdin/stdout.
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	if httpCfg != nil {
		client, err := newHostHTTPClient(*httpCfg)
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

	return &WazeroRuntime{rt: rt, module: compiled, env: env}, nil
}

// Call invokes the WASM module with input on stdin and captures stdout as the result.
// The guest is instantiated with the env configured at construction.
func (w *WazeroRuntime) Call(ctx context.Context, input []byte) ([]byte, error) {
	return w.CallWithEnv(ctx, input, w.env)
}

// CallWithEnv is Call with environment variables set for the invocation
// (used by tests to select guest code paths).
func (w *WazeroRuntime) CallWithEnv(ctx context.Context, input []byte, env map[string]string) ([]byte, error) {
	stdin := bytes.NewReader(input)
	var stdout bytes.Buffer

	cfg := wazero.NewModuleConfig().
		WithStdin(stdin).
		WithStdout(&stdout).
		WithStderr(&bytes.Buffer{}). // discard stderr
		WithName("").                // anonymous module so multiple calls don't collide
		// wazero's sandbox defaults are a fake clock and a deterministic
		// random source. Time-dependent guests — an authentication module
		// checking JWT exp/nbf — would silently accept expired credentials
		// against a frozen clock, so the guest sees the real system
		// facilities (ADR 0008).
		WithSysWalltime().
		WithSysNanotime().
		WithSysNanosleep().
		WithRandSource(rand.Reader)
	for k, v := range env {
		cfg = cfg.WithEnv(k, v)
	}

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

// Type returns the runtime type for logging/metrics.
func (w *WazeroRuntime) Type() RuntimeType {
	return RuntimeWazero
}
