//go:build wasmer

package wasmer

import (
	"testing"
)

func TestNewPortPool(t *testing.T) {
	pool := NewPortPool(9000, 9999)
	if pool == nil {
		t.Fatal("NewPortPool returned nil")
	}
	if pool.minPort != 9000 {
		t.Errorf("minPort = %d, want 9000", pool.minPort)
	}
	if pool.maxPort != 9999 {
		t.Errorf("maxPort = %d, want 9999", pool.maxPort)
	}
	if pool.used == nil {
		t.Error("used map is nil")
	}
}

func TestPortPool_Allocate(t *testing.T) {
	// Use a narrow range that's likely to have available ports
	pool := NewPortPool(45000, 45100)

	port, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate failed: %v", err)
	}
	if port < 45000 || port > 45100 {
		t.Errorf("allocated port %d out of range [45000, 45100]", port)
	}
	if !pool.used[port] {
		t.Errorf("port %d not marked as used", port)
	}
}

func TestPortPool_AllocateMultiple(t *testing.T) {
	pool := NewPortPool(45200, 45210)

	ports := make(map[int]bool)
	for i := 0; i < 5; i++ {
		port, err := pool.Allocate()
		if err != nil {
			t.Fatalf("Allocate %d failed: %v", i, err)
		}
		if ports[port] {
			t.Errorf("port %d allocated twice", port)
		}
		ports[port] = true
	}

	if len(ports) != 5 {
		t.Errorf("expected 5 unique ports, got %d", len(ports))
	}
}

func TestPortPool_Release(t *testing.T) {
	pool := NewPortPool(45300, 45400)

	port, err := pool.Allocate()
	if err != nil {
		t.Fatalf("Allocate failed: %v", err)
	}

	pool.Release(port)

	if pool.used[port] {
		t.Errorf("port %d still marked as used after release", port)
	}
}

func TestPortPool_ReleaseThenReallocate(t *testing.T) {
	pool := NewPortPool(45500, 45600)

	port1, err := pool.Allocate()
	if err != nil {
		t.Fatalf("first Allocate failed: %v", err)
	}

	pool.Release(port1)

	port2, err := pool.Allocate()
	if err != nil {
		t.Fatalf("second Allocate failed: %v", err)
	}

	// Should be able to reallocate the same port
	if port2 != port1 {
		t.Logf("Note: got different port after release (first: %d, second: %d), this is OK", port1, port2)
	}
}

func TestPortPool_Exhaustion(t *testing.T) {
	// Use a very small range to test exhaustion
	pool := NewPortPool(45700, 45702)

	// Allocate all available ports
	for i := 0; i < 3; i++ {
		_, err := pool.Allocate()
		if err != nil {
			t.Fatalf("Allocate %d failed: %v", i, err)
		}
	}

	// Next allocation should fail
	_, err := pool.Allocate()
	if err == nil {
		t.Error("expected error when pool is exhausted")
	}
}

func TestPortPool_ConcurrentAllocation(t *testing.T) {
	pool := NewPortPool(45800, 45900)

	done := make(chan int)
	allocate := func() {
		port, err := pool.Allocate()
		if err != nil {
			t.Errorf("concurrent Allocate failed: %v", err)
			done <- -1
			return
		}
		done <- port
	}

	// Start multiple concurrent allocations
	go allocate()
	go allocate()
	go allocate()

	ports := make(map[int]bool)
	for i := 0; i < 3; i++ {
		port := <-done
		if port < 0 {
			continue // Error already reported
		}
		if ports[port] {
			t.Errorf("port %d allocated to multiple goroutines", port)
		}
		ports[port] = true
	}

	if len(ports) != 3 {
		t.Errorf("expected 3 unique ports, got %d", len(ports))
	}
}

func TestNewManager(t *testing.T) {
	manager := NewManager()
	if manager == nil {
		t.Fatal("NewManager returned nil")
	}
	if manager.apps == nil {
		t.Error("apps map is nil")
	}
	if manager.portPool == nil {
		t.Error("portPool is nil")
	}
}

func TestManager_GetApp_NotFound(t *testing.T) {
	manager := NewManager()

	_, ok := manager.GetApp("nonexistent")
	if ok {
		t.Error("expected GetApp to return false for nonexistent app")
	}
}

func TestManager_ListApps_Empty(t *testing.T) {
	manager := NewManager()

	apps := manager.ListApps()
	if len(apps) != 0 {
		t.Errorf("expected empty list, got %d apps", len(apps))
	}
}

func TestManager_ListApps_WithApps(t *testing.T) {
	manager := NewManager()

	// Add apps directly to the map for testing
	manager.apps["app1"] = &AppInstance{Name: "app1", Addr: "127.0.0.1:9001"}
	manager.apps["app2"] = &AppInstance{Name: "app2", Addr: "127.0.0.1:9002"}

	apps := manager.ListApps()
	if len(apps) != 2 {
		t.Errorf("expected 2 apps, got %d", len(apps))
	}

	// Verify both apps are present
	found := make(map[string]bool)
	for _, app := range apps {
		found[app.Name] = true
	}
	if !found["app1"] || !found["app2"] {
		t.Error("expected both app1 and app2 in list")
	}
}

func TestManager_StopApp_NotFound(t *testing.T) {
	manager := NewManager()

	err := manager.StopApp(nil, "nonexistent")
	if err == nil {
		t.Error("expected error when stopping nonexistent app")
	}
}

func TestManager_StartApp_AlreadyExists(t *testing.T) {
	manager := NewManager()

	// Add an existing app to the map
	manager.apps["existing-app"] = &AppInstance{Name: "existing-app", Addr: "127.0.0.1:9001"}

	cfg := AppConfig{
		Name:   "existing-app",
		Module: "/path/to/module.wasm",
	}

	err := manager.StartApp(nil, cfg)
	if err == nil {
		t.Error("expected error when starting app that already exists")
	}
}

func TestManager_StartApp_MissingModule(t *testing.T) {
	manager := NewManager()

	cfg := AppConfig{
		Name:   "new-app",
		Module: "/nonexistent/path/to/module.wasm",
	}

	err := manager.StartApp(nil, cfg)
	if err == nil {
		t.Error("expected error when starting app with missing module")
	}
}

func TestManager_GetApp_Found(t *testing.T) {
	manager := NewManager()

	// Add an app directly to the map for testing
	manager.apps["test-app"] = &AppInstance{Name: "test-app", Addr: "127.0.0.1:9001"}

	app, ok := manager.GetApp("test-app")
	if !ok {
		t.Fatal("expected to find test-app")
	}
	if app.Name != "test-app" {
		t.Errorf("app.Name = %q, want test-app", app.Name)
	}
}

func TestExtractPort(t *testing.T) {
	tests := []struct {
		addr     string
		expected int
	}{
		{"127.0.0.1:8080", 8080},
		{"127.0.0.1:9000", 9000},
		{"127.0.0.1:65535", 65535},
		{"", 0},
		{"invalid", 0},
		{"127.0.0.1:", 0},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			port := extractPort(tt.addr)
			if port != tt.expected {
				t.Errorf("extractPort(%q) = %d, want %d", tt.addr, port, tt.expected)
			}
		})
	}
}

func TestHealthStatus_Constants(t *testing.T) {
	tests := []struct {
		name     string
		status   HealthStatus
		expected string
	}{
		{"healthy", HealthHealthy, "healthy"},
		{"unhealthy", HealthUnhealthy, "unhealthy"},
		{"starting", HealthStarting, "starting"},
		{"stopped", HealthStopped, "stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("HealthStatus %s = %q, want %q", tt.name, tt.status, tt.expected)
			}
		})
	}
}

func TestAppInstance_Fields(t *testing.T) {
	instance := &AppInstance{
		Name:   "test-app",
		Config: AppConfig{Name: "test-app", Module: "/test.wasm"},
		Addr:   "127.0.0.1:8080",
		Health: HealthHealthy,
	}

	if instance.Name != "test-app" {
		t.Errorf("Name = %q, want test-app", instance.Name)
	}
	if instance.Config.Module != "/test.wasm" {
		t.Errorf("Config.Module = %q, want /test.wasm", instance.Config.Module)
	}
	if instance.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr = %q, want 127.0.0.1:8080", instance.Addr)
	}
	if instance.Health != HealthHealthy {
		t.Errorf("Health = %q, want healthy", instance.Health)
	}
}
