//go:build wasmer

package unified

import (
	"time"

	"github.com/lsm/fiso/internal/link"
)

// Config is the unified configuration for fiso-wasmer-aio.
type Config struct {
	// Flow configuration directory.
	Flow FlowConfig `yaml:"flow"`

	// Link proxy configuration.
	Link LinkConfig `yaml:"link"`

	// Wasmer app configurations.
	Wasmer WasmerConfig `yaml:"wasmer"`
}

// FlowConfig configures the flow pipeline component.
type FlowConfig struct {
	// ConfigDir is the path to flow definitions directory.
	ConfigDir string `yaml:"configDir"`

	// MetricsAddr for flow metrics.
	MetricsAddr string `yaml:"metricsAddr"`
}

// LinkConfig configures the link proxy component.
type LinkConfig struct {
	// ConfigPath is the path to link config file.
	ConfigPath string `yaml:"configPath"`

	// Or inline configuration.
	*link.Config `yaml:",inline"`
}

// WasmerConfig configures Wasmer applications.
type WasmerConfig struct {
	// Apps is the list of Wasmer apps to run.
	Apps []AppConfig `yaml:"apps"`

	// DefaultPortRange for auto-allocation.
	DefaultPortRange PortRange `yaml:"defaultPortRange"`
}

// AppConfig configures a single Wasmer application.
type AppConfig struct {
	// Name is the unique identifier.
	Name string `yaml:"name"`

	// Module is the path to the .wasm file.
	Module string `yaml:"module"`

	// Execution mode: perRequest, longRunning, or pooled.
	Execution string `yaml:"execution"`

	// Port for HTTP server (0 = auto-allocate).
	Port int `yaml:"port"`

	// MemoryMB is the memory limit.
	MemoryMB int64 `yaml:"memoryMB"`

	// Timeout for requests.
	Timeout time.Duration `yaml:"timeout"`

	// Environment variables.
	Env map[string]string `yaml:"env"`

	// Preopened directories.
	Preopens map[string]string `yaml:"preopens"`

	// HealthCheck endpoint path.
	HealthCheck string `yaml:"healthCheck"`

	// HealthCheckInterval for periodic checks.
	HealthCheckInterval time.Duration `yaml:"healthCheckInterval"`
}

// PortRange defines the range for dynamic port allocation.
type PortRange struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Flow: FlowConfig{
			ConfigDir:   "/etc/fiso/flows",
			MetricsAddr: ":9090",
		},
		Link: LinkConfig{
			ConfigPath: "/etc/fiso/link/config.yaml",
		},
		Wasmer: WasmerConfig{
			DefaultPortRange: PortRange{Min: 9000, Max: 9999},
		},
	}
}
