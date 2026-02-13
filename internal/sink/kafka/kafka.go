package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/lsm/fiso/internal/correlation"
	"github.com/lsm/fiso/internal/kafka"
	kafkasource "github.com/lsm/fiso/internal/source/kafka"
	"github.com/lsm/fiso/internal/tracing"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// publisher abstracts the kafka publisher for testing.
type publisher interface {
	Publish(ctx context.Context, topic string, key, value []byte, headers map[string]string) error
	Close() error
}

// Config holds Kafka sink configuration.
type Config struct {
	Cluster *kafka.ClusterConfig // Cluster config with auth/TLS (required)
	Topic   string
}

// Sink delivers events to a Kafka topic.
type Sink struct {
	publisher publisher
	topic     string
	logger    *slog.Logger
	tracer    trace.Tracer
}

// NewSink creates a new Kafka sink.
func NewSink(cfg Config) (*Sink, error) {
	if cfg.Cluster == nil {
		return nil, fmt.Errorf("cluster config is required")
	}
	if cfg.Topic == "" {
		return nil, fmt.Errorf("topic is required")
	}

	pub, err := kafkasource.NewPublisher(cfg.Cluster)
	if err != nil {
		return nil, fmt.Errorf("kafka publisher: %w", err)
	}

	return &Sink{
		publisher: pub,
		topic:     cfg.Topic,
		logger:    slog.Default(),
		tracer:    noop.NewTracerProvider().Tracer("kafka-sink"),
	}, nil
}

// SetTracer sets the tracer for the sink.
func (s *Sink) SetTracer(tracer trace.Tracer) {
	s.tracer = tracer
}

// Deliver sends an event to the configured Kafka topic.
func (s *Sink) Deliver(ctx context.Context, event []byte, headers map[string]string) error {
	start := time.Now()

	// Extract correlation ID from headers
	corrID := correlation.ExtractOrGenerate(headers)

	// Start span for delivery
	ctx, span := tracing.StartSpan(ctx, s.tracer, tracing.SpanKafkaPublish,
		trace.WithAttributes(
			tracing.KafkaTopicAttr(s.topic),
			tracing.CorrelationAttr(corrID.Value),
		),
	)
	defer span.End()

	// Inject trace context into headers for propagation
	headers = correlation.InjectTraceContext(ctx, headers)

	err := s.publisher.Publish(ctx, s.topic, nil, event, headers)
	if err != nil {
		tracing.SetSpanError(span, err)
		s.logger.Error("delivery failed",
			"correlation_id", corrID.Value,
			"target", s.topic,
			"error", err,
		)
		return err
	}

	tracing.SetSpanOK(span)
	s.logger.Info("event delivered",
		"correlation_id", corrID.Value,
		"target", s.topic,
		"latency_ms", time.Since(start).Milliseconds(),
	)
	return nil
}

// Close shuts down the Kafka publisher.
func (s *Sink) Close() error {
	return s.publisher.Close()
}
