package wasm

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		rt       RuntimeType
		expected string
	}{
		{"wazero", RuntimeWazero, "wazero"},
		{"wasmer", RuntimeWasmer, "wasmer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.rt) != tt.expected {
				t.Errorf("RuntimeType %q = %q, want %q", tt.name, tt.rt, tt.expected)
			}
		})
	}
}

func TestModuleType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		mt       ModuleType
		expected string
	}{
		{"transform", ModuleTypeTransform, "transform"},
		{"app", ModuleTypeApp, "app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.mt) != tt.expected {
				t.Errorf("ModuleType %q = %q, want %q", tt.name, tt.mt, tt.expected)
			}
		})
	}
}

func TestExecutionMode_Constants(t *testing.T) {
	tests := []struct {
		name     string
		em       ExecutionMode
		expected string
	}{
		{"perRequest", ExecutionPerRequest, "perRequest"},
		{"longRunning", ExecutionLongRunning, "longRunning"},
		{"pooled", ExecutionPooled, "pooled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.em) != tt.expected {
				t.Errorf("ExecutionMode %q = %q, want %q", tt.name, tt.em, tt.expected)
			}
		})
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := Config{}

	// Verify zero values are sensible defaults
	if cfg.Type != "" {
		t.Errorf("expected empty Type as default, got %q", cfg.Type)
	}
	if cfg.MemoryLimit != 0 {
		t.Errorf("expected MemoryLimit 0 (unlimited), got %d", cfg.MemoryLimit)
	}
	if cfg.Timeout != 0 {
		t.Errorf("expected Timeout 0 (no timeout), got %v", cfg.Timeout)
	}
	if cfg.Env != nil {
		t.Errorf("expected nil Env, got %v", cfg.Env)
	}
	if cfg.Preopens != nil {
		t.Errorf("expected nil Preopens, got %v", cfg.Preopens)
	}
}

func TestConfig_WithValues(t *testing.T) {
	cfg := Config{
		Type:        RuntimeWazero,
		ModulePath:  "/path/to/module.wasm",
		ModuleType:  ModuleTypeTransform,
		Execution:   ExecutionPerRequest,
		MemoryLimit: 64 * 1024 * 1024, // 64MB
		Timeout:     30 * time.Second,
		Env:         map[string]string{"FOO": "bar"},
		Preopens:    map[string]string{"/data": "/host/data"},
	}

	if cfg.Type != RuntimeWazero {
		t.Errorf("Type = %q, want %q", cfg.Type, RuntimeWazero)
	}
	if cfg.ModulePath != "/path/to/module.wasm" {
		t.Errorf("ModulePath = %q, want /path/to/module.wasm", cfg.ModulePath)
	}
	if cfg.ModuleType != ModuleTypeTransform {
		t.Errorf("ModuleType = %q, want %q", cfg.ModuleType, ModuleTypeTransform)
	}
	if cfg.Execution != ExecutionPerRequest {
		t.Errorf("Execution = %q, want %q", cfg.Execution, ExecutionPerRequest)
	}
	if cfg.MemoryLimit != 64*1024*1024 {
		t.Errorf("MemoryLimit = %d, want 67108864", cfg.MemoryLimit)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}
	if cfg.Env["FOO"] != "bar" {
		t.Errorf("Env[FOO] = %q, want bar", cfg.Env["FOO"])
	}
	if cfg.Preopens["/data"] != "/host/data" {
		t.Errorf("Preopens[/data] = %q, want /host/data", cfg.Preopens["/data"])
	}
}

// MockRuntime implements Runtime for testing purposes.
type MockRuntime struct {
	callErr   error
	callResp  []byte
	closeErr  error
	rtype     RuntimeType
	closed    bool
	callCount int
}

func (m *MockRuntime) Call(ctx context.Context, input []byte) ([]byte, error) {
	m.callCount++
	return m.callResp, m.callErr
}

func (m *MockRuntime) Close() error {
	m.closed = true
	return m.closeErr
}

func (m *MockRuntime) Type() RuntimeType {
	return m.rtype
}

// MockAppRuntime implements AppRuntime for testing purposes.
type MockAppRuntime struct {
	*MockRuntime
	addr     string
	running  bool
	startErr error
	stopErr  error
}

func (m *MockAppRuntime) Start(ctx context.Context) (string, error) {
	m.running = true
	return m.addr, m.startErr
}

func (m *MockAppRuntime) Stop(ctx context.Context) error {
	m.running = false
	return m.stopErr
}

func (m *MockAppRuntime) Addr() string {
	return m.addr
}

func (m *MockAppRuntime) IsRunning() bool {
	return m.running
}

func TestMockRuntime_ImplementsInterface(t *testing.T) {
	// This test verifies that MockRuntime implements the Runtime interface
	var _ Runtime = &MockRuntime{rtype: RuntimeWazero}
}

func TestMockAppRuntime_ImplementsInterface(t *testing.T) {
	// This test verifies that MockAppRuntime implements the AppRuntime interface
	var _ AppRuntime = &MockAppRuntime{MockRuntime: &MockRuntime{rtype: RuntimeWasmer}}
}
