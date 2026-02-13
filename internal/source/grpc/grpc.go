package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/lsm/fiso/internal/correlation"
	"github.com/lsm/fiso/internal/source"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Config holds gRPC source configuration.
type Config struct {
	ListenAddr string
}

// Source receives events via gRPC and dispatches them to the handler.
type Source struct {
	server *grpc.Server
	logger *slog.Logger
	addr   string
	// ListenAddr is set after Start creates the listener, safe to read
	// once the source is running.
	ListenAddr string
	ready      chan struct{}
}

// NewSource creates a new gRPC source.
func NewSource(cfg Config, logger *slog.Logger) (*Source, error) {
	if cfg.ListenAddr == "" {
		return nil, fmt.Errorf("gRPC listen address is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Source{
		addr:   cfg.ListenAddr,
		logger: logger,
		ready:  make(chan struct{}),
	}, nil
}

// Start begins accepting gRPC connections and dispatching events to the handler.
// Blocks until ctx is cancelled.
func (s *Source) Start(ctx context.Context, handler func(context.Context, source.Event) error) error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	s.ListenAddr = lis.Addr().String()

	svc := &eventHandler{handler: handler, logger: s.logger}
	s.server = grpc.NewServer(
		grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(svc.handle),
	)

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("grpc source starting", "addr", s.ListenAddr)
		close(s.ready)
		if err := s.server.Serve(lis); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		s.server.GracefulStop()
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// Close stops the gRPC server.
func (s *Source) Close() error {
	if s.server != nil {
		s.server.Stop()
	}
	return nil
}

type eventHandler struct {
	handler func(context.Context, source.Event) error
	logger  *slog.Logger
}

func (eh *eventHandler) handle(_ interface{}, stream grpc.ServerStream) error {
	var data []byte
	if err := stream.RecvMsg(&data); err != nil {
		return fmt.Errorf("receive: %w", err)
	}

	// Extract headers from gRPC metadata
	headers := make(map[string]string)
	if md, ok := metadata.FromIncomingContext(stream.Context()); ok {
		for k, v := range md {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
	}

	// Get context from stream and extract trace context and correlation ID
	ctx := stream.Context()
	ctx = correlation.ExtractTraceContext(ctx, headers)
	corrID := correlation.ExtractOrGenerate(headers)

	evt := source.Event{
		Value:         data,
		Headers:       headers,
		Topic:         "grpc",
		CorrelationID: corrID.Value,
	}

	eh.logger.Info("event received",
		"correlation_id", corrID.Value,
		"correlation_source", corrID.Source,
		"topic", "grpc",
	)

	if err := eh.handler(ctx, evt); err != nil {
		eh.logger.Error("handler error", "error", err)
		return err
	}

	return stream.SendMsg([]byte("ok"))
}

// rawCodec sends/receives raw bytes without protobuf.
type rawCodec struct{}

func (rawCodec) Marshal(v interface{}) ([]byte, error) {
	b, ok := v.([]byte)
	if !ok {
		return nil, fmt.Errorf("rawCodec: expected []byte, got %T", v)
	}
	return b, nil
}

func (rawCodec) Unmarshal(data []byte, v interface{}) error {
	bp, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("rawCodec: expected *[]byte, got %T", v)
	}
	*bp = data
	return nil
}

func (rawCodec) Name() string { return "raw" }
