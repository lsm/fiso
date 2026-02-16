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

func TestMockRuntime_Call(t *testing.T) {
	mock := &MockRuntime{
		callResp: []byte(`{"result":"success"}`),
		rtype:    RuntimeWazero,
	}

	ctx := context.Background()
	resp, err := mock.Call(ctx, []byte(`{}`))
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if string(resp) != `{"result":"success"}` {
		t.Errorf("Call() = %q, want %q", resp, `{"result":"success"}`)
	}
	if mock.callCount != 1 {
		t.Errorf("callCount = %d, want 1", mock.callCount)
	}
}

func TestMockRuntime_CallError(t *testing.T) {
	mock := &MockRuntime{
		callErr: context.DeadlineExceeded,
		rtype:   RuntimeWazero,
	}

	ctx := context.Background()
	_, err := mock.Call(ctx, []byte(`{}`))
	if err != context.DeadlineExceeded {
		t.Errorf("Call() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestMockRuntime_Close(t *testing.T) {
	mock := &MockRuntime{rtype: RuntimeWazero}

	if err := mock.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
	if !mock.closed {
		t.Error("expected closed to be true after Close()")
	}
}

func TestMockRuntime_CloseError(t *testing.T) {
	mock := &MockRuntime{
		closeErr: context.Canceled,
		rtype:    RuntimeWazero,
	}

	if err := mock.Close(); err != context.Canceled {
		t.Errorf("Close() error = %v, want %v", err, context.Canceled)
	}
}

func TestMockRuntime_Type(t *testing.T) {
	tests := []struct {
		name     string
		rtype    RuntimeType
		expected RuntimeType
	}{
		{"wazero", RuntimeWazero, RuntimeWazero},
		{"wasmer", RuntimeWasmer, RuntimeWasmer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockRuntime{rtype: tt.rtype}
			if mock.Type() != tt.expected {
				t.Errorf("Type() = %q, want %q", mock.Type(), tt.expected)
			}
		})
	}
}

func TestMockAppRuntime_Start(t *testing.T) {
	mock := &MockAppRuntime{
		MockRuntime: &MockRuntime{rtype: RuntimeWasmer},
		addr:        "127.0.0.1:8080",
	}

	ctx := context.Background()
	addr, err := mock.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if addr != "127.0.0.1:8080" {
		t.Errorf("Start() addr = %q, want %q", addr, "127.0.0.1:8080")
	}
	if !mock.running {
		t.Error("expected running to be true after Start()")
	}
}

func TestMockAppRuntime_StartError(t *testing.T) {
	mock := &MockAppRuntime{
		MockRuntime: &MockRuntime{rtype: RuntimeWasmer},
		startErr:    context.DeadlineExceeded,
	}

	ctx := context.Background()
	_, err := mock.Start(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Start() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestMockAppRuntime_Stop(t *testing.T) {
	mock := &MockAppRuntime{
		MockRuntime: &MockRuntime{rtype: RuntimeWasmer},
		running:     true,
	}

	ctx := context.Background()
	if err := mock.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if mock.running {
		t.Error("expected running to be false after Stop()")
	}
}

func TestMockAppRuntime_StopError(t *testing.T) {
	mock := &MockAppRuntime{
		MockRuntime: &MockRuntime{rtype: RuntimeWasmer},
		running:     true,
		stopErr:     context.Canceled,
	}

	ctx := context.Background()
	if err := mock.Stop(ctx); err != context.Canceled {
		t.Errorf("Stop() error = %v, want %v", err, context.Canceled)
	}
}

func TestMockAppRuntime_Addr(t *testing.T) {
	mock := &MockAppRuntime{
		MockRuntime: &MockRuntime{rtype: RuntimeWasmer},
		addr:        "127.0.0.1:9090",
	}

	if mock.Addr() != "127.0.0.1:9090" {
		t.Errorf("Addr() = %q, want %q", mock.Addr(), "127.0.0.1:9090")
	}
}

func TestMockAppRuntime_IsRunning(t *testing.T) {
	mock := &MockAppRuntime{
		MockRuntime: &MockRuntime{rtype: RuntimeWasmer},
		running:     true,
	}

	if !mock.IsRunning() {
		t.Error("IsRunning() = false, want true")
	}

	mock.running = false
	if mock.IsRunning() {
		t.Error("IsRunning() = true, want false")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid wazero config",
			config: Config{
				Type:       RuntimeWazero,
				ModulePath: "/path/to/module.wasm",
			},
			wantErr: false,
		},
		{
			name: "valid wasmer config",
			config: Config{
				Type:       RuntimeWasmer,
				ModulePath: "/path/to/module.wasm",
			},
			wantErr: false,
		},
		{
			name: "invalid runtime type",
			config: Config{
				Type:       RuntimeType("invalid"),
				ModulePath: "/path/to/module.wasm",
			},
			wantErr: true,
		},
		{
			name: "negative memory limit",
			config: Config{
				MemoryLimit: -1,
			},
			wantErr: true,
		},
		{
			name: "negative timeout",
			config: Config{
				Timeout: -1 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid execution mode",
			config: Config{
				Execution: ExecutionMode("invalid"),
			},
			wantErr: true,
		},
		{
			name: "invalid module type",
			config: Config{
				ModuleType: ModuleType("invalid"),
			},
			wantErr: true,
		},
		{
			name: "valid execution modes",
			config: Config{
				ModulePath: "/path/to/module.wasm",
				Execution:  ExecutionPerRequest,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
