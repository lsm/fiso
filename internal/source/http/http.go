package http

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"

	"github.com/lsm/fiso/internal/source"
)

// Config holds HTTP source configuration.
type Config struct {
	ListenAddr string
	Path       string
}

// Source receives events via HTTP POST and dispatches them to the handler.
type Source struct {
	server     *http.Server
	logger     *slog.Logger
	addr       string
	path       string
	ListenAddr string
	ready      chan struct{}
}

// NewSource creates a new HTTP source.
func NewSource(cfg Config, logger *slog.Logger) (*Source, error) {
	if cfg.ListenAddr == "" {
		return nil, fmt.Errorf("HTTP listen address is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	path := cfg.Path
	if path == "" {
		path = "/"
	}
	return &Source{
		addr:   cfg.ListenAddr,
		path:   path,
		logger: logger,
		ready:  make(chan struct{}),
	}, nil
}

// Start begins accepting HTTP requests and dispatching events to the handler.
// Blocks until ctx is cancelled.
func (s *Source) Start(ctx context.Context, handler func(context.Context, source.Event) error) error {
	mux := http.NewServeMux()
	mux.HandleFunc(s.path, func(w http.ResponseWriter, r *http.Request) {
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
			s.logger.Error("handler error", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	s.ListenAddr = lis.Addr().String()

	s.server = &http.Server{Handler: mux}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http source starting", "addr", s.ListenAddr, "path", s.path)
		close(s.ready)
		if err := s.server.Serve(lis); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		if err := s.server.Shutdown(context.Background()); err != nil {
			s.logger.Error("http server shutdown error", "error", err)
		}
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// Close stops the HTTP server.
func (s *Source) Close() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}
