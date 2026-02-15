//go:build wasmer

package wasmer

import (
	"time"
)

// AppConfig configures a Wasmer application.
type AppConfig struct {
	// Name is the unique identifier for this app.
	Name string `yaml:"name"`

	// Module is the path to the .wasm file.
	Module string `yaml:"module"`

	// Execution mode: perRequest, longRunning, or pooled.
	Execution string `yaml:"execution"`

	// Port for the HTTP server (0 = auto-allocate).
	Port int `yaml:"port"`

	// MemoryMB is the memory limit in megabytes.
	MemoryMB int64 `yaml:"memoryMB"`

	// Timeout for HTTP requests.
	Timeout time.Duration `yaml:"timeout"`

	// Environment variables for the WASM module.
	Env map[string]string `yaml:"env"`

	// Preopened directories for filesystem access.
	Preopens map[string]string `yaml:"preopens"`

	// HealthCheck path (e.g., "/health").
	HealthCheck string `yaml:"healthCheck"`

	// HealthCheckInterval for periodic health checks.
	HealthCheckInterval time.Duration `yaml:"healthCheckInterval"`
}

// ManagerConfig configures the Wasmer manager.
type ManagerConfig struct {
	// Apps is the list of applications to manage.
	Apps []AppConfig `yaml:"apps"`

	// DefaultPortRange for auto-allocation.
	DefaultPortRange PortRange `yaml:"defaultPortRange"`
}

// PortRange defines the range for dynamic port allocation.
type PortRange struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}
