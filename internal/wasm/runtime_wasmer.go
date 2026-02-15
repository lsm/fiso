//go:build wasmer

package wasm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	wasmer "github.com/wasmerio/wasmer-go/wasmer"
)

// WasmerRuntime implements Runtime using the Wasmer WebAssembly runtime with WASIX support.
// Requires CGO and the wasmer build tag.
type WasmerRuntime struct {
	mu     sync.Mutex
	store  *wasmer.Store
	module *wasmer.Module
	config Config
	closed bool
}

// NewWasmerRuntime creates a new Wasmer runtime for per-request WASM execution.
// The module must be a WASI binary (wasip1 or WASIX) that reads JSON from stdin and writes JSON to stdout.
func NewWasmerRuntime(ctx context.Context, wasmBytes []byte, cfg Config) (*WasmerRuntime, error) {
	// Create engine with Cranelift compiler (good balance of speed and performance)
	config := wasmer.NewConfig()
	config.UseCraneliftCompiler()
	config.UseUniversalEngine()

	engine := wasmer.NewEngineWithConfig(config)
	store := wasmer.NewStore(engine)

	// Compile the module
	module, err := wasmer.NewModule(store, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("compile wasm module: %w", err)
	}

	return &WasmerRuntime{
		store:  store,
		module: module,
		config: cfg,
	}, nil
}

// Call invokes the WASM module with input and returns the output.
// Uses WASI stdin/stdout for communication.
func (w *WasmerRuntime) Call(ctx context.Context, input []byte) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil, fmt.Errorf("runtime is closed")
	}

	// Create a temporary file for stdin input (wasmer-go doesn't have direct stdin injection)
	tmpDir := os.TempDir()
	stdinFile := filepath.Join(tmpDir, fmt.Sprintf("wasm-stdin-%d", time.Now().UnixNano()))
	if err := os.WriteFile(stdinFile, input, 0600); err != nil {
		return nil, fmt.Errorf("write stdin file: %w", err)
	}
	defer os.Remove(stdinFile)

	// Build WASI environment
	builder := wasmer.NewWasiStateBuilder(w.config.ModulePath)

	// Add environment variables from config
	for key, value := range w.config.Env {
		builder.Environment(key, value)
	}

	// Map stdin file as /stdin
	builder.MapDirectory("stdin", tmpDir)
	builder.Argument("--stdin-file")
	builder.Argument("/stdin/" + filepath.Base(stdinFile))

	// Add preopens from config
	for alias, path := range w.config.Preopens {
		builder.MapDirectory(alias, path)
	}

	// Capture stdout/stderr
	builder.CaptureStdout()
	builder.CaptureStderr()

	wasiEnv, err := builder.Finalize()
	if err != nil {
		return nil, fmt.Errorf("create wasi environment: %w", err)
	}

	// Generate import object from WASI environment
	importObject, err := wasiEnv.GenerateImportObject(w.store, w.module)
	if err != nil {
		return nil, fmt.Errorf("generate imports: %w", err)
	}

	// Instantiate module
	instance, err := wasmer.NewInstance(w.module, importObject)
	if err != nil {
		return nil, fmt.Errorf("instantiate module: %w", err)
	}
	defer instance.Close()

	// Apply timeout if configured
	if w.config.Timeout > 0 {
		ctx, cancel := context.WithTimeout(ctx, w.config.Timeout)
		defer cancel()

		done := make(chan struct{})
		var execErr error
		var result []byte

		go func() {
			defer close(done)
			result, execErr = w.executeWasi(instance, wasiEnv)
		}()

		select {
		case <-done:
			return result, execErr
		case <-ctx.Done():
			return nil, fmt.Errorf("wasm execution timeout after %v", w.config.Timeout)
		}
	}

	return w.executeWasi(instance, wasiEnv)
}

// executeWasi runs the WASI start function and returns stdout.
func (w *WasmerRuntime) executeWasi(instance *wasmer.Instance, wasiEnv *wasmer.WasiEnvironment) ([]byte, error) {
	// Get the WASI _start function
	start, err := instance.Exports.GetWasiStartFunction()
	if err != nil {
		return nil, fmt.Errorf("get wasi start function: %w", err)
	}

	// Execute the module
	_, err = start()

	// Read captured output
	stdout := wasiEnv.ReadStdout()
	stderr := wasiEnv.ReadStderr()

	if err != nil {
		if len(stderr) > 0 {
			return stdout, fmt.Errorf("wasm execution: %w, stderr: %s", err, string(stderr))
		}
		return stdout, fmt.Errorf("wasm execution: %w", err)
	}

	return stdout, nil
}

// Close releases Wasmer resources.
func (w *WasmerRuntime) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	w.closed = true

	if w.module != nil {
		w.module.Close()
	}
	if w.store != nil {
		w.store.Close()
	}

	return nil
}

// Type returns the runtime type.
func (w *WasmerRuntime) Type() RuntimeType {
	return RuntimeWasmer
}
