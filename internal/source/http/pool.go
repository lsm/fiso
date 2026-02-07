package http

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/lsm/fiso/internal/source"
)

// ServerPool manages shared HTTP servers by listen address.
// Multiple flows can register different paths on the same server.
type ServerPool struct {
	mu      sync.RWMutex
	servers map[string]*sharedServer
	logger  *slog.Logger
}

type sharedServer struct {
	addr     string
	mux      *http.ServeMux
	server   *http.Server
	listener net.Listener
	handlers map[string]func(context.Context, source.Event) error
	started  bool
	ready    chan struct{} // closed when listener is ready
	mu       sync.Mutex
	logger   *slog.Logger
}

// RouteHandle represents a registered route that can be closed.
type RouteHandle struct {
	pool *ServerPool
	addr string
	path string
}

// PooledSource is an HTTP source that uses a shared server pool.
// It implements the Source interface but delegates actual serving to the pool.
type PooledSource struct {
	pool   *ServerPool
	addr   string
	path   string
	handle *RouteHandle
}

// NewPooledSource creates an HTTP source that pre-registers with a shared pool.
// The path is reserved immediately so pool.Start() knows about all routes.
// The handler is set later when Start() is called.
func NewPooledSource(pool *ServerPool, cfg Config) (*PooledSource, error) {
	if cfg.ListenAddr == "" {
		return nil, fmt.Errorf("HTTP listen address is required")
	}
	path := cfg.Path
	if path == "" {
		path = "/"
	}

	// Pre-register to reserve the path (handler set in Start)
	handle, err := pool.PreRegister(cfg.ListenAddr, path)
	if err != nil {
		return nil, fmt.Errorf("pre-register: %w", err)
	}

	return &PooledSource{
		pool:   pool,
		addr:   cfg.ListenAddr,
		path:   path,
		handle: handle,
	}, nil
}

// Start sets the handler for this source and blocks until ctx is cancelled.
// The actual HTTP server is managed by the pool.
func (s *PooledSource) Start(ctx context.Context, handler func(context.Context, source.Event) error) error {
	if err := s.pool.SetHandler(s.handle, handler); err != nil {
		return fmt.Errorf("set handler: %w", err)
	}

	// Block until context is cancelled
	// The actual serving is done by the pool's Start method
	<-ctx.Done()
	return ctx.Err()
}

// Close is a no-op for pooled sources; the pool manages server lifecycle.
func (s *PooledSource) Close() error {
	return nil
}

// NewServerPool creates a new HTTP server pool.
func NewServerPool(logger *slog.Logger) *ServerPool {
	if logger == nil {
		logger = slog.Default()
	}
	return &ServerPool{
		servers: make(map[string]*sharedServer),
		logger:  logger,
	}
}

// PreRegister reserves a path on a shared server without setting a handler.
// The handler is set later via SetHandler. This allows routes to be registered
// before pool.Start() while handlers are set during source.Start().
func (p *ServerPool) PreRegister(listenAddr, path string) (*RouteHandle, error) {
	if listenAddr == "" {
		return nil, fmt.Errorf("listenAddr is required")
	}
	if path == "" {
		path = "/"
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	srv, exists := p.servers[listenAddr]
	if !exists {
		srv = &sharedServer{
			addr:     listenAddr,
			mux:      http.NewServeMux(),
			handlers: make(map[string]func(context.Context, source.Event) error),
			ready:    make(chan struct{}),
			logger:   p.logger,
		}
		p.servers[listenAddr] = srv
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if _, pathExists := srv.handlers[path]; pathExists {
		return nil, fmt.Errorf("path %q already registered on %s", path, listenAddr)
	}

	// Reserve the path with nil handler (set later via SetHandler)
	srv.handlers[path] = nil

	// Register HTTP handler that looks up the actual handler at request time
	srv.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		srv.mu.Lock()
		handler := srv.handlers[path]
		srv.mu.Unlock()

		if handler == nil {
			http.Error(w, "handler not ready", http.StatusServiceUnavailable)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		headers := make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}

		evt := source.Event{
			Value:   body,
			Headers: headers,
			Topic:   "http",
		}

		if err := handler(r.Context(), evt); err != nil {
			srv.logger.Error("handler error", "path", path, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	return &RouteHandle{
		pool: p,
		addr: listenAddr,
		path: path,
	}, nil
}

// SetHandler sets the handler for a pre-registered route.
func (p *ServerPool) SetHandler(handle *RouteHandle, handler func(context.Context, source.Event) error) error {
	if handle == nil {
		return fmt.Errorf("handle is nil")
	}

	p.mu.RLock()
	srv, exists := p.servers[handle.addr]
	p.mu.RUnlock()

	if !exists {
		return fmt.Errorf("server %s not found", handle.addr)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if _, pathExists := srv.handlers[handle.path]; !pathExists {
		return fmt.Errorf("path %q not registered on %s", handle.path, handle.addr)
	}

	srv.handlers[handle.path] = handler
	return nil
}

// Register adds a path handler to a shared server.
// If no server exists for the listenAddr, one is created.
// Returns a handle that can be used to track the registration.
func (p *ServerPool) Register(listenAddr, path string, handler func(context.Context, source.Event) error) (*RouteHandle, error) {
	if listenAddr == "" {
		return nil, fmt.Errorf("listenAddr is required")
	}
	if path == "" {
		path = "/"
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	srv, exists := p.servers[listenAddr]
	if !exists {
		srv = &sharedServer{
			addr:     listenAddr,
			mux:      http.NewServeMux(),
			handlers: make(map[string]func(context.Context, source.Event) error),
			ready:    make(chan struct{}),
			logger:   p.logger,
		}
		p.servers[listenAddr] = srv
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if _, pathExists := srv.handlers[path]; pathExists {
		return nil, fmt.Errorf("path %q already registered on %s", path, listenAddr)
	}

	// Register the HTTP handler
	srv.handlers[path] = handler
	srv.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		headers := make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}

		evt := source.Event{
			Value:   body,
			Headers: headers,
			Topic:   "http",
		}

		if err := handler(r.Context(), evt); err != nil {
			srv.logger.Error("handler error", "path", path, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	return &RouteHandle{
		pool: p,
		addr: listenAddr,
		path: path,
	}, nil
}

// Start starts all registered servers. Call this after all routes are registered.
// Blocks until ctx is cancelled.
func (p *ServerPool) Start(ctx context.Context) error {
	p.mu.RLock()
	servers := make([]*sharedServer, 0, len(p.servers))
	for _, srv := range p.servers {
		servers = append(servers, srv)
	}
	p.mu.RUnlock()

	if len(servers) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	errCh := make(chan error, len(servers))

	for _, srv := range servers {
		go func(s *sharedServer) {
			if err := s.start(ctx); err != nil && err != context.Canceled {
				errCh <- err
			}
		}(srv)
	}

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		// Graceful shutdown
		_ = p.Close()
		return ctx.Err()
	case err := <-errCh:
		_ = p.Close()
		return err
	}
}

// Close gracefully shuts down all servers.
func (p *ServerPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var firstErr error
	for addr, srv := range p.servers {
		if err := srv.close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close server %s: %w", addr, err)
		}
	}
	return firstErr
}

// ServerCount returns the number of shared servers in the pool.
func (p *ServerPool) ServerCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.servers)
}

// RouteCount returns the total number of registered routes across all servers.
func (p *ServerPool) RouteCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, srv := range p.servers {
		srv.mu.Lock()
		count += len(srv.handlers)
		srv.mu.Unlock()
	}
	return count
}

// WaitReady blocks until all servers are ready to accept connections.
func (p *ServerPool) WaitReady() {
	p.mu.RLock()
	servers := make([]*sharedServer, 0, len(p.servers))
	for _, srv := range p.servers {
		servers = append(servers, srv)
	}
	p.mu.RUnlock()

	for _, srv := range servers {
		<-srv.ready
	}
}

// ListenAddr returns the actual listen address for a server after it starts.
// Returns empty string if server not found or not started.
func (p *ServerPool) ListenAddr(configAddr string) string {
	p.mu.RLock()
	srv, exists := p.servers[configAddr]
	p.mu.RUnlock()

	if !exists {
		return ""
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.listener == nil {
		return ""
	}
	return srv.listener.Addr().String()
}

func (s *sharedServer) start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}

	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	s.listener = lis
	s.server = &http.Server{Handler: s.mux}
	s.started = true
	close(s.ready) // signal that listener is ready
	s.mu.Unlock()

	s.logger.Info("http pool server starting", "addr", lis.Addr().String(), "routes", len(s.handlers))

	errCh := make(chan error, 1)
	go func() {
		if err := s.server.Serve(lis); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (s *sharedServer) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		return s.server.Shutdown(context.Background())
	}
	return nil
}
