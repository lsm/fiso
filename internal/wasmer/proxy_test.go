//go:build wasmer

package wasmer

import (
	"testing"

	"github.com/lsm/fiso/internal/interceptor"
)

func TestNewProxy(t *testing.T) {
	manager := NewManager()
	proxy := NewProxy(manager)

	if proxy == nil {
		t.Fatal("NewProxy returned nil")
	}
	if proxy.manager != manager {
		t.Error("proxy.manager not set correctly")
	}
}

func TestProxy_Forward_AppNotFound(t *testing.T) {
	manager := NewManager()
	proxy := NewProxy(manager)

	req := &interceptor.Request{
		Payload:   []byte(`{"test": true}`),
		Headers:   map[string]string{"Content-Type": "application/json"},
		Direction: interceptor.Inbound,
	}

	_, err := proxy.Forward(nil, "nonexistent-app", req)
	if err == nil {
		t.Fatal("expected error when forwarding to nonexistent app")
	}
}

func TestProxy_Forward_AppNotHealthy(t *testing.T) {
	manager := NewManager()
	proxy := NewProxy(manager)

	// Add an app instance manually with unhealthy status
	manager.apps["unhealthy-app"] = &AppInstance{
		Name:   "unhealthy-app",
		Health: HealthUnhealthy,
		Addr:   "127.0.0.1:9999",
	}

	req := &interceptor.Request{
		Payload:   []byte(`{"test": true}`),
		Headers:   map[string]string{"Content-Type": "application/json"},
		Direction: interceptor.Inbound,
	}

	_, err := proxy.Forward(nil, "unhealthy-app", req)
	if err == nil {
		t.Fatal("expected error when forwarding to unhealthy app")
	}
}

func TestProxy_Forward_AppStarting(t *testing.T) {
	manager := NewManager()
	proxy := NewProxy(manager)

	// Add an app instance with starting status
	manager.apps["starting-app"] = &AppInstance{
		Name:   "starting-app",
		Health: HealthStarting,
		Addr:   "127.0.0.1:9998",
	}

	req := &interceptor.Request{
		Payload:   []byte(`{"test": true}`),
		Headers:   map[string]string{"Content-Type": "application/json"},
		Direction: interceptor.Inbound,
	}

	_, err := proxy.Forward(nil, "starting-app", req)
	if err == nil {
		t.Fatal("expected error when forwarding to starting app")
	}
}

func TestProxy_Forward_AppStopped(t *testing.T) {
	manager := NewManager()
	proxy := NewProxy(manager)

	// Add an app instance with stopped status
	manager.apps["stopped-app"] = &AppInstance{
		Name:   "stopped-app",
		Health: HealthStopped,
		Addr:   "127.0.0.1:9997",
	}

	req := &interceptor.Request{
		Payload:   []byte(`{"test": true}`),
		Headers:   map[string]string{"Content-Type": "application/json"},
		Direction: interceptor.Inbound,
	}

	_, err := proxy.Forward(nil, "stopped-app", req)
	if err == nil {
		t.Fatal("expected error when forwarding to stopped app")
	}
}
