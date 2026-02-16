//go:build wasmer

package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
)

// WasmerAppRuntime implements AppRuntime for long-running HTTP applications.
//
// NOTE: wasmer-go v1.0.4 doesn't expose WASIX socket imports (e.g. sock_accept),
// so Go WASI modules that call net/http directly cannot be instantiated in-process.
// To support app-mode semantics reliably, we run a host HTTP server and invoke the
// WASM module per request via the stdin/stdout JSON ABI.
type WasmerAppRuntime struct {
	mu      sync.Mutex
	config  Config
	runner  *WasmerRuntime
	server  *http.Server
	ln      net.Listener
	addr    string
	running bool
}

type appRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   string            `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

type appResponse struct {
	Status   int               `json:"status,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Body     json.RawMessage   `json:"body,omitempty"`
	BodyText string            `json:"bodyText,omitempty"`
}

// NewWasmerAppRuntime creates a new Wasmer app runtime.
func NewWasmerAppRuntime(ctx context.Context, wasmBytes []byte, cfg Config) (*WasmerAppRuntime, error) {
	runner, err := NewWasmerRuntime(ctx, wasmBytes, cfg)
	if err != nil {
		return nil, fmt.Errorf("create wasmer runner: %w", err)
	}

	return &WasmerAppRuntime{
		config: cfg,
		runner: runner,
	}, nil
}

// Start launches the app as a long-running HTTP endpoint.
func (w *WasmerAppRuntime) Start(ctx context.Context) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return w.addr, fmt.Errorf("app already running")
	}

	port := w.config.Env["PORT"]
	if port == "" {
		port = "80"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", fmt.Errorf("invalid PORT %q: %w", port, err)
	}

	listenAddr := "0.0.0.0:" + port
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return "", fmt.Errorf("listen on %s: %w", listenAddr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", w.handleHTTP)

	w.server = &http.Server{Handler: mux}
	w.ln = ln
	w.addr = "127.0.0.1:" + port
	w.running = true

	go func() {
		_ = w.server.Serve(ln)
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
	}()

	return w.addr, nil
}

func (w *WasmerAppRuntime) handleHTTP(rw http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	var body []byte
	if req.Body != nil {
		defer req.Body.Close()
		b, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(rw, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
			return
		}
		body = b
	}

	headers := make(map[string]string, len(req.Header))
	for k, v := range req.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	payload := appRequest{
		Method:  req.Method,
		Path:    req.URL.Path,
		Query:   req.URL.RawQuery,
		Headers: headers,
	}
	if len(body) > 0 {
		if json.Valid(body) {
			payload.Body = body
		} else {
			wrapped, _ := json.Marshal(string(body))
			payload.Body = wrapped
		}
	}

	in, err := json.Marshal(payload)
	if err != nil {
		http.Error(rw, fmt.Sprintf("marshal request: %v", err), http.StatusInternalServerError)
		return
	}

	out, err := w.runner.Call(ctx, in)
	if err != nil {
		http.Error(rw, fmt.Sprintf("wasm execution: %v", err), http.StatusBadGateway)
		return
	}

	var resp appResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		http.Error(rw, fmt.Sprintf("decode wasm response: %v", err), http.StatusBadGateway)
		return
	}

	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}

	for k, v := range resp.Headers {
		rw.Header().Set(k, v)
	}
	if rw.Header().Get("Content-Type") == "" && len(resp.BodyText) == 0 {
		rw.Header().Set("Content-Type", "application/json")
	}

	rw.WriteHeader(status)

	if len(resp.BodyText) > 0 {
		_, _ = rw.Write([]byte(resp.BodyText))
		return
	}

	if len(resp.Body) > 0 {
		_, _ = rw.Write(resp.Body)
		return
	}

	_, _ = rw.Write([]byte("{}"))
}

// Stop gracefully shuts down the app.
func (w *WasmerAppRuntime) Stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return nil
	}

	if w.server != nil {
		if err := w.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown app server: %w", err)
		}
	}
	w.running = false
	return nil
}

// Addr returns the HTTP address.
func (w *WasmerAppRuntime) Addr() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.addr
}

// IsRunning returns true if the app is active.
func (w *WasmerAppRuntime) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// Call invokes the app (for interface compliance).
func (w *WasmerAppRuntime) Call(ctx context.Context, input []byte) ([]byte, error) {
	return w.runner.Call(ctx, input)
}

// Close releases resources.
func (w *WasmerAppRuntime) Close() error {
	_ = w.Stop(context.Background())
	if w.runner != nil {
		return w.runner.Close()
	}
	return nil
}

// Type returns the runtime type.
func (w *WasmerAppRuntime) Type() RuntimeType {
	return RuntimeWasmer
}
