//go:build wasmer

package wasmer

import (
	"testing"
	"time"
)

func TestAppConfig_Defaults(t *testing.T) {
	cfg := AppConfig{}

	if cfg.Name != "" {
		t.Errorf("expected empty Name, got %q", cfg.Name)
	}
	if cfg.Module != "" {
		t.Errorf("expected empty Module, got %q", cfg.Module)
	}
	if cfg.Execution != "" {
		t.Errorf("expected empty Execution, got %q", cfg.Execution)
	}
	if cfg.Port != 0 {
		t.Errorf("expected Port 0 (auto-allocate), got %d", cfg.Port)
	}
	if cfg.MemoryMB != 0 {
		t.Errorf("expected MemoryMB 0, got %d", cfg.MemoryMB)
	}
	if cfg.Timeout != 0 {
		t.Errorf("expected Timeout 0, got %v", cfg.Timeout)
	}
	if cfg.Env != nil {
		t.Errorf("expected nil Env, got %v", cfg.Env)
	}
	if cfg.Preopens != nil {
		t.Errorf("expected nil Preopens, got %v", cfg.Preopens)
	}
	if cfg.HealthCheck != "" {
		t.Errorf("expected empty HealthCheck, got %q", cfg.HealthCheck)
	}
	if cfg.HealthCheckInterval != 0 {
		t.Errorf("expected HealthCheckInterval 0, got %v", cfg.HealthCheckInterval)
	}
}

func TestAppConfig_WithValues(t *testing.T) {
	cfg := AppConfig{
		Name:                "test-app",
		Module:              "/apps/test.wasm",
		Execution:           "longRunning",
		Port:                8080,
		MemoryMB:            128,
		Timeout:             30 * time.Second,
		Env:                 map[string]string{"NODE_ENV": "production"},
		Preopens:            map[string]string{"/data": "/host/data"},
		HealthCheck:         "/health",
		HealthCheckInterval: 10 * time.Second,
	}

	if cfg.Name != "test-app" {
		t.Errorf("Name = %q, want test-app", cfg.Name)
	}
	if cfg.Module != "/apps/test.wasm" {
		t.Errorf("Module = %q, want /apps/test.wasm", cfg.Module)
	}
	if cfg.Execution != "longRunning" {
		t.Errorf("Execution = %q, want longRunning", cfg.Execution)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.MemoryMB != 128 {
		t.Errorf("MemoryMB = %d, want 128", cfg.MemoryMB)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}
	if cfg.Env["NODE_ENV"] != "production" {
		t.Errorf("Env[NODE_ENV] = %q, want production", cfg.Env["NODE_ENV"])
	}
	if cfg.Preopens["/data"] != "/host/data" {
		t.Errorf("Preopens[/data] = %q, want /host/data", cfg.Preopens["/data"])
	}
	if cfg.HealthCheck != "/health" {
		t.Errorf("HealthCheck = %q, want /health", cfg.HealthCheck)
	}
	if cfg.HealthCheckInterval != 10*time.Second {
		t.Errorf("HealthCheckInterval = %v, want 10s", cfg.HealthCheckInterval)
	}
}

func TestManagerConfig_Defaults(t *testing.T) {
	cfg := ManagerConfig{}

	if cfg.Apps != nil {
		t.Errorf("expected nil Apps, got %v", cfg.Apps)
	}
	if cfg.DefaultPortRange.Min != 0 {
		t.Errorf("expected DefaultPortRange.Min 0, got %d", cfg.DefaultPortRange.Min)
	}
	if cfg.DefaultPortRange.Max != 0 {
		t.Errorf("expected DefaultPortRange.Max 0, got %d", cfg.DefaultPortRange.Max)
	}
}

func TestManagerConfig_WithApps(t *testing.T) {
	cfg := ManagerConfig{
		Apps: []AppConfig{
			{Name: "app1", Module: "/app1.wasm"},
			{Name: "app2", Module: "/app2.wasm"},
		},
		DefaultPortRange: PortRange{Min: 9000, Max: 9999},
	}

	if len(cfg.Apps) != 2 {
		t.Errorf("len(Apps) = %d, want 2", len(cfg.Apps))
	}
	if cfg.Apps[0].Name != "app1" {
		t.Errorf("Apps[0].Name = %q, want app1", cfg.Apps[0].Name)
	}
	if cfg.DefaultPortRange.Min != 9000 {
		t.Errorf("DefaultPortRange.Min = %d, want 9000", cfg.DefaultPortRange.Min)
	}
	if cfg.DefaultPortRange.Max != 9999 {
		t.Errorf("DefaultPortRange.Max = %d, want 9999", cfg.DefaultPortRange.Max)
	}
}

func TestPortRange_Values(t *testing.T) {
	tests := []struct {
		name    string
		pr      PortRange
		wantMin int
		wantMax int
	}{
		{"zero values", PortRange{}, 0, 0},
		{"standard range", PortRange{Min: 9000, Max: 9999}, 9000, 9999},
		{"single port", PortRange{Min: 8080, Max: 8080}, 8080, 8080},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.pr.Min != tt.wantMin {
				t.Errorf("Min = %d, want %d", tt.pr.Min, tt.wantMin)
			}
			if tt.pr.Max != tt.wantMax {
				t.Errorf("Max = %d, want %d", tt.pr.Max, tt.wantMax)
			}
		})
	}
}
