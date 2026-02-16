//go:build wasmer

package wasmer

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/lsm/fiso/internal/wasm"
)

// Manager handles lifecycle of long-running Wasmer apps.
type Manager struct {
	mu       sync.RWMutex
	apps     map[string]*AppInstance
	portPool *PortPool
	logger   interface{} // TODO: use proper logger type
}

// AppInstance represents a running Wasmer application.
type AppInstance struct {
	Name    string
	Config  AppConfig
	Runtime wasm.AppRuntime
	Client  *http.Client
	Addr    string
	Health  HealthStatus
	Started time.Time
}

// HealthStatus represents the health of an app.
type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthUnhealthy HealthStatus = "unhealthy"
	HealthStarting  HealthStatus = "starting"
	HealthStopped   HealthStatus = "stopped"
)

// PortPool manages dynamic port allocation.
type PortPool struct {
	mu      sync.Mutex
	used    map[int]bool
	minPort int
	maxPort int
}

// NewPortPool creates a new port pool.
func NewPortPool(minPort, maxPort int) *PortPool {
	return &PortPool{
		used:    make(map[int]bool),
		minPort: minPort,
		maxPort: maxPort,
	}
}

// Allocate returns an available port.
func (p *PortPool) Allocate() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for port := p.minPort; port <= p.maxPort; port++ {
		if !p.used[port] {
			// Verify port is actually available
			ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
			if err != nil {
				continue
			}
			ln.Close()

			p.used[port] = true
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", p.minPort, p.maxPort)
}

// Release returns a port to the pool.
func (p *PortPool) Release(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.used, port)
}

// NewManager creates a new Wasmer app manager.
func NewManager() *Manager {
	return &Manager{
		apps:     make(map[string]*AppInstance),
		portPool: NewPortPool(9000, 9999),
	}
}

// StartApp launches a Wasmer app.
func (m *Manager) StartApp(ctx context.Context, cfg AppConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.apps[cfg.Name]; exists {
		return fmt.Errorf("app %q already running", cfg.Name)
	}

	// Allocate port if not specified
	port := cfg.Port
	if port == 0 {
		var err error
		port, err = m.portPool.Allocate()
		if err != nil {
			return fmt.Errorf("allocate port: %w", err)
		}
	}

	// Read WASM module
	wasmBytes, err := os.ReadFile(cfg.Module)
	if err != nil {
		m.portPool.Release(port)
		return fmt.Errorf("read wasm module: %w", err)
	}

	// Create runtime config
	runtimeCfg := wasm.Config{
		Type:       wasm.RuntimeWasmer,
		ModulePath: cfg.Module,
		Env:        cfg.Env,
		Preopens:   cfg.Preopens,
	}
	if runtimeCfg.Env == nil {
		runtimeCfg.Env = make(map[string]string)
	}
	runtimeCfg.Env["PORT"] = fmt.Sprintf("%d", port)
	if cfg.MemoryMB > 0 {
		runtimeCfg.MemoryLimit = cfg.MemoryMB * 1024 * 1024
	}
	if cfg.Timeout > 0 {
		runtimeCfg.Timeout = cfg.Timeout
	}

	// Create app runtime
	runtime, err := wasm.NewWasmerAppRuntime(ctx, wasmBytes, runtimeCfg)
	if err != nil {
		m.portPool.Release(port)
		return fmt.Errorf("create runtime: %w", err)
	}

	// Start the app
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if _, err := runtime.Start(ctx); err != nil {
		runtime.Close()
		m.portPool.Release(port)
		return fmt.Errorf("start app: %w", err)
	}

	// Create HTTP client for the app
	client := &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: false,
		},
	}

	m.apps[cfg.Name] = &AppInstance{
		Name:    cfg.Name,
		Config:  cfg,
		Runtime: runtime,
		Client:  client,
		Addr:    addr,
		Health:  HealthStarting,
		Started: time.Now(),
	}

	return nil
}

// StopApp gracefully stops a Wasmer app.
func (m *Manager) StopApp(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	app, exists := m.apps[name]
	if !exists {
		return fmt.Errorf("app %q not found", name)
	}

	if err := app.Runtime.Stop(ctx); err != nil {
		return fmt.Errorf("stop app: %w", err)
	}

	app.Runtime.Close()
	m.portPool.Release(extractPort(app.Addr))
	delete(m.apps, name)

	return nil
}

// GetApp returns an app instance by name.
func (m *Manager) GetApp(name string) (*AppInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	app, ok := m.apps[name]
	return app, ok
}

// ListApps returns all running apps.
func (m *Manager) ListApps() []*AppInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	apps := make([]*AppInstance, 0, len(m.apps))
	for _, app := range m.apps {
		apps = append(apps, app)
	}
	return apps
}

// StopAll stops all running apps.
func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for name, app := range m.apps {
		if err := app.Runtime.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop %s: %w", name, err))
		}
		app.Runtime.Close()
		m.portPool.Release(extractPort(app.Addr))
	}
	m.apps = make(map[string]*AppInstance)

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping apps: %v", errs)
	}
	return nil
}

// extractPort extracts the port from an address string.
func extractPort(addr string) int {
	var port int
	fmt.Sscanf(addr, "127.0.0.1:%d", &port)
	return port
}
