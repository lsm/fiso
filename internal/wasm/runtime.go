package wasm

import (
	"context"
	"time"
)

// RuntimeType indicates which WASM runtime to use.
type RuntimeType string

const (
	RuntimeWazero RuntimeType = "wazero" // Pure Go, for transforms
	RuntimeWasmer RuntimeType = "wasmer" // CGO, for full apps with WASIX
)

// ModuleType indicates the WASM module's capability level.
type ModuleType string

const (
	ModuleTypeTransform ModuleType = "transform" // JSON-in/JSON-out
	ModuleTypeApp       ModuleType = "app"       // Full WASIX app
)

// ExecutionMode determines how the WASM module is invoked.
type ExecutionMode string

const (
	// ExecutionPerRequest creates a new instance for each invocation.
	ExecutionPerRequest ExecutionMode = "perRequest"
	// ExecutionLongRunning runs a single instance with HTTP endpoint.
	ExecutionLongRunning ExecutionMode = "longRunning"
	// ExecutionPooled maintains a pool of instances for concurrency.
	ExecutionPooled ExecutionMode = "pooled"
)

// Config configures a WASM runtime.
type Config struct {
	// Type specifies which runtime to use (wazero or wasmer).
	Type RuntimeType `yaml:"type"`

	// ModulePath is the path to the .wasm file.
	ModulePath string `yaml:"modulePath"`

	// ModuleType indicates transform vs app.
	ModuleType ModuleType `yaml:"moduleType"`

	// Execution mode for app-type WASM.
	Execution ExecutionMode `yaml:"execution"`

	// MemoryLimit in bytes (0 = unlimited).
	MemoryLimit int64 `yaml:"memoryLimit"`

	// Timeout per invocation.
	Timeout time.Duration `yaml:"timeout"`

	// Environment variables for the WASM module.
	Env map[string]string `yaml:"env"`

	// Preopened directories for filesystem access (Wasmer only).
	Preopens map[string]string `yaml:"preopens"`
}

// Runtime abstracts WASM execution across tazero and Wasmer.
type Runtime interface {
	// Call invokes the WASM module with binary input, returns binary output.
	Call(ctx context.Context, input []byte) ([]byte, error)

	// Close releases runtime resources.
	Close() error

	// Type returns the runtime type for logging/metrics.
	Type() RuntimeType
}

// AppRuntime extends Runtime for long-running WASIX applications.
type AppRuntime interface {
	Runtime

	// Start launches the app as a long-running process.
	// Returns the address (host:port) for HTTP communication.
	Start(ctx context.Context) (addr string, err error)

	// Stop gracefully shuts down the app.
	Stop(ctx context.Context) error

	// Addr returns the HTTP address if the app is running.
	Addr() string

	// IsRunning returns true if the app is active.
	IsRunning() bool
}

// Factory creates runtimes based on configuration.
type Factory interface {
	Create(ctx context.Context, cfg Config) (Runtime, error)
	CreateApp(ctx context.Context, cfg Config) (AppRuntime, error)
}
