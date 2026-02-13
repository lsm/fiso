package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/lsm/fiso/internal/correlation"
	"github.com/lsm/fiso/internal/tracing"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Config holds gRPC sink configuration.
type Config struct {
	Address string
	TLS     bool
	Timeout time.Duration
}

// Sink delivers events via gRPC unary call.
// It uses a simple convention: the event is sent as raw bytes in the
// "fiso-event" metadata field, and the body is passed as the request payload.
// The server implements a simple Deliver(context, payload) -> response pattern.
type Sink struct {
	conn    *grpc.ClientConn
	timeout time.Duration
	logger  *slog.Logger
	tracer  trace.Tracer
}

// NewSink creates a new gRPC sink.
func NewSink(cfg Config) (*Sink, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("gRPC address is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	var opts []grpc.DialOption
	if !cfg.TLS {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	// Add OTel gRPC instrumentation
	opts = append(opts, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))

	conn, err := grpc.NewClient(cfg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpc dial: %w", err)
	}

	return &Sink{
		conn:    conn,
		timeout: cfg.Timeout,
		logger:  slog.Default(),
		tracer:  noop.NewTracerProvider().Tracer("grpc-sink"),
	}, nil
}

// SetTracer sets the tracer for the sink.
func (s *Sink) SetTracer(tracer trace.Tracer) {
	s.tracer = tracer
}

// Deliver sends the event via gRPC. It uses a generic unary invoker
// with the method path "/fiso.v1.EventService/Deliver".
func (s *Sink) Deliver(ctx context.Context, event []byte, headers map[string]string) error {
	start := time.Now()

	// Extract correlation ID from headers
	corrID := correlation.ExtractOrGenerate(headers)

	// Start span for delivery
	ctx, span := tracing.StartSpan(ctx, s.tracer, tracing.SpanGRPCDeliver,
		trace.WithAttributes(
			tracing.GRPCMethodAttr("/fiso.v1.EventService/Deliver"),
			tracing.CorrelationAttr(corrID.Value),
		),
	)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Inject trace context into headers for propagation
	headers = correlation.InjectTraceContext(ctx, headers)

	// Convert headers to gRPC metadata
	md := metadata.New(headers)
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Use raw codec for the unary call
	var resp []byte
	err := s.conn.Invoke(ctx, "/fiso.v1.EventService/Deliver", event, &resp, grpc.ForceCodec(rawCodec{}))
	if err != nil {
		tracing.SetSpanError(span, err)
		s.logger.Error("delivery failed",
			"correlation_id", corrID.Value,
			"target", s.conn.Target(),
			"error", err,
		)
		return fmt.Errorf("grpc deliver: %w", err)
	}

	tracing.SetSpanOK(span)
	s.logger.Info("event delivered",
		"correlation_id", corrID.Value,
		"target", s.conn.Target(),
		"latency_ms", time.Since(start).Milliseconds(),
	)
	return nil
}

// Close closes the gRPC connection.
func (s *Sink) Close() error {
	return s.conn.Close()
}

// rawCodec is a gRPC codec that sends/receives raw bytes without protobuf.
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
