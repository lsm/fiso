//go:build wasmer

package wasmer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/lsm/fiso/internal/interceptor"
)

// Proxy handles HTTP proxying to Wasmer apps.
type Proxy struct {
	manager *Manager
}

// NewProxy creates a new Wasmer proxy.
func NewProxy(manager *Manager) *Proxy {
	return &Proxy{manager: manager}
}

// Forward sends a request to a Wasmer app and returns the response.
func (p *Proxy) Forward(ctx context.Context, appName string, req *interceptor.Request) (*interceptor.Request, error) {
	app, ok := p.manager.GetApp(appName)
	if !ok {
		return nil, fmt.Errorf("app %q not found", appName)
	}

	if app.Health != HealthHealthy {
		return nil, fmt.Errorf("app %q is not healthy (status: %s)", appName, app.Health)
	}

	// Build HTTP request to the app
	url := fmt.Sprintf("http://%s/process", app.Addr)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(req.Payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set headers
	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := app.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("app returned error: %s (%s)", resp.Status, string(body))
	}

	// Build response headers
	respHeaders := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			respHeaders[key] = values[0]
		}
	}

	return &interceptor.Request{
		Payload:   body,
		Headers:   respHeaders,
		Direction: req.Direction,
	}, nil
}
