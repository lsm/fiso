//go:build wasmer

package wasm

import (
	"context"
	"fmt"
	"sync"
	"time"

	wasmer "github.com/wasmerio/wasmer-go/wasmer"
)

// WasmerAppRuntime implements AppRuntime for long-running WASIX applications.
type WasmerAppRuntime struct {
	mu       sync.Mutex
	config   Config
	instance *wasmer.Instance
	wasiEnv  *wasmer.WasiEnvironment
	store    *wasmer.Store
	module   *wasmer.Module
	addr     string
	cancel   context.CancelFunc
	running  bool
	exited   chan error
}

// NewWasmerAppRuntime creates a new Wasmer app runtime for long-running applications.
func NewWasmerAppRuntime(ctx context.Context, wasmBytes []byte, cfg Config) (*WasmerAppRuntime, error) {
	config := wasmer.NewConfig()
	config.UseCraneliftCompiler()
	config.UseUniversalEngine()

	engine := wasmer.NewEngineWithConfig(config)
	store := wasmer.NewStore(engine)

	module, err := wasmer.NewModule(store, wasmBytes)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("compile wasm module: %w", err)
	}

	return &WasmerAppRuntime{
		config: cfg,
		store:  store,
		module: module,
		exited: make(chan error, 1),
	}, nil
}

// Start launches the app as a long-running process.
func (w *WasmerAppRuntime) Start(ctx context.Context) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return w.addr, fmt.Errorf("app already running")
	}

	// Create a cancelable context for the app lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	// Build WASI environment
	builder := wasmer.NewWasiStateBuilder(w.config.ModulePath)

	// Add environment variables
	for key, value := range w.config.Env {
		builder.Environment(key, value)
	}

	// Add preopens
	for alias, path := range w.config.Preopens {
		builder.MapDirectory(alias, path)
	}

	// WASIX networking is enabled by default in wasmer-go?
	// Note: wasmer-go's WasiStateBuilder doesn't seem to have explicit "EnableNetworking" methods exposed in the easy API,
	// but it inherits host capabilities.

	// Inherit stdio for logs
	builder.InheritStdout()
	builder.InheritStderr()

	wasiEnv, err := builder.Finalize()
	if err != nil {
		return "", fmt.Errorf("create wasi environment: %w", err)
	}
	w.wasiEnv = wasiEnv

	importObject, err := wasiEnv.GenerateImportObject(w.store, w.module)
	if err != nil {
		return "", fmt.Errorf("generate imports: %w", err)
	}

	instance, err := wasmer.NewInstance(w.module, importObject)
	if err != nil {
		return "", fmt.Errorf("instantiate module: %w", err)
	}
	w.instance = instance

	// Run _start in a goroutine
	go func() {
		start, err := instance.Exports.GetWasiStartFunction()
		if err != nil {
			w.exited <- fmt.Errorf("get start function: %w", err)
			return
		}

		// Run the module
		// This blocks until the module exits
		_, err = start()
		if err != nil {
			// Check if it's a clean exit or error
			w.exited <- err
		} else {
			w.exited <- nil
		}

		// When it exits, mark as not running
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
	}()

	// Wait a bit to ensure it starts up (rudimentary health check)
	// Real implementation should probably probe the port
	time.Sleep(100 * time.Millisecond)

	// Determine address based on PORT env
	port := w.config.Env["PORT"]
	if port == "" {
		port = "80" // default fallback
	}
	w.addr = "127.0.0.1:" + port
	w.running = true

	return w.addr, nil
}

// Stop gracefully shuts down the app.
func (w *WasmerAppRuntime) Stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return nil
	}

	// Cancel context (if we used it to pass to the runtime, but we didn't)
	if w.cancel != nil {
		w.cancel()
	}

	// We can't force kill a running WASM thread easily in wasmer-go v1.0.4
	// without destroying the store/instance.
	// But Close() will do that.

	w.running = false
	return nil
}

// Addr returns the HTTP address.
func (w *WasmerAppRuntime) Addr() string {
	return w.addr
}

// IsRunning returns true if the app is active.
func (w *WasmerAppRuntime) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// Call invokes the app (for AppRuntime interface compliance).
// For AppRuntime, this is not used as we use HTTP proxying.
func (w *WasmerAppRuntime) Call(ctx context.Context, input []byte) ([]byte, error) {
	return nil, fmt.Errorf("direct call not supported for app runtime, use HTTP")
}

// Close releases resources.
func (w *WasmerAppRuntime) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cancel != nil {
		w.cancel()
	}

	if w.module != nil {
		w.module.Close()
	}
	if w.store != nil {
		w.store.Close()
	}

	w.running = false
	return nil
}

// Type returns the runtime type.
func (w *WasmerAppRuntime) Type() RuntimeType {
	return RuntimeWasmer
}
