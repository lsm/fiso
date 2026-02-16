//go:build wasmer

package wasmer

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// Execution mode constants
const (
	ExecutionPerRequest  = "perRequest"
	ExecutionLongRunning = "longRunning"
	ExecutionPooled      = "pooled"
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

// Validate checks that the app config is valid.
func (c *AppConfig) Validate() error {
	if c.Name == "" {
		return errors.New("app name is required")
	}
	if c.Module == "" {
		return errors.New("module path is required")
	}
	if _, err := os.Stat(c.Module); err != nil {
		return fmt.Errorf("module path not accessible: %w", err)
	}
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}
	if c.MemoryMB < 0 {
		return fmt.Errorf("memory limit must be non-negative: %d", c.MemoryMB)
	}
	if c.Timeout < 0 {
		return fmt.Errorf("timeout must be non-negative: %v", c.Timeout)
	}
	if c.HealthCheckInterval < 0 {
		return fmt.Errorf("health check interval must be non-negative: %v", c.HealthCheckInterval)
	}
	switch c.Execution {
	case ExecutionPerRequest, ExecutionLongRunning, ExecutionPooled, "":
		// valid
	default:
		return fmt.Errorf("invalid execution mode: %s", c.Execution)
	}
	return nil
}

// Validate checks that the manager config is valid.
func (c *ManagerConfig) Validate() error {
	for i := range c.Apps {
		if err := c.Apps[i].Validate(); err != nil {
			return fmt.Errorf("app[%d]: %w", i, err)
		}
	}
	if c.DefaultPortRange.Min < 0 || c.DefaultPortRange.Min > 65535 {
		return fmt.Errorf("invalid port range min: %d", c.DefaultPortRange.Min)
	}
	if c.DefaultPortRange.Max < 0 || c.DefaultPortRange.Max > 65535 {
		return fmt.Errorf("invalid port range max: %d", c.DefaultPortRange.Max)
	}
	if c.DefaultPortRange.Max > 0 && c.DefaultPortRange.Min > c.DefaultPortRange.Max {
		return errors.New("port range min cannot be greater than max")
	}
	return nil
}
