package kafka

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lsm/fiso/internal/correlation"
	"github.com/lsm/fiso/internal/kafka"
	"github.com/lsm/fiso/internal/source"
	"github.com/lsm/fiso/internal/tracing"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/trace"
)

// Config holds Kafka source configuration.
type Config struct {
	Cluster       *kafka.ClusterConfig // Cluster config with auth/TLS (required)
	Topic         string
	ConsumerGroup string
	StartOffset   string // "earliest" or "latest" (default: "latest")
}

// consumer abstracts the kafka client methods used by Source for testing.
type consumer interface {
	PollFetches(ctx context.Context) kgo.Fetches
	MarkCommitRecords(rs ...*kgo.Record)
	CommitMarkedOffsets(ctx context.Context) error
	Close()
}

// Source consumes events from a Kafka topic.
type Source struct {
	client consumer
	topic  string
	logger *slog.Logger
	tracer trace.Tracer
}

// NewSource creates a new Kafka source.
func NewSource(cfg Config, logger *slog.Logger) (*Source, error) {
	if cfg.Cluster == nil {
		return nil, fmt.Errorf("cluster config is required")
	}
	if cfg.Topic == "" {
		return nil, fmt.Errorf("topic is required")
	}
	if cfg.ConsumerGroup == "" {
		return nil, fmt.Errorf("consumer group is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	offset := kgo.NewOffset().AtEnd()
	if cfg.StartOffset == "earliest" {
		offset = kgo.NewOffset().AtStart()
	}

	opts, err := kafka.ClientOptions(cfg.Cluster)
	if err != nil {
		return nil, fmt.Errorf("cluster options: %w", err)
	}

	// Add consumer-specific options
	opts = append(opts,
		kgo.ConsumerGroup(cfg.ConsumerGroup),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.ConsumeResetOffset(offset),
		kgo.DisableAutoCommit(),
	)

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("kafka client: %w", err)
	}

	return &Source{
		client: client,
		topic:  cfg.Topic,
		logger: logger,
		tracer: trace.NewNoopTracerProvider().Tracer("kafka-source"),
	}, nil
}

// SetTracer sets the tracer for the source.
func (s *Source) SetTracer(tracer trace.Tracer) {
	s.tracer = tracer
}

// Start begins consuming events from Kafka. Blocks until ctx is cancelled.
func (s *Source) Start(ctx context.Context, handler func(context.Context, source.Event) error) error {
	s.logger.Info("starting kafka consumer", "topic", s.topic)

	for {
		fetches := s.client.PollFetches(ctx)

		if errs := fetches.Errors(); len(errs) > 0 {
			for _, err := range errs {
				s.logger.Error("fetch error", "topic", err.Topic, "partition", err.Partition, "error", err.Err)
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}

		fetches.EachRecord(func(record *kgo.Record) {
			evt := source.Event{
				Key:     record.Key,
				Value:   record.Value,
				Headers: make(map[string]string, len(record.Headers)),
				Offset:  record.Offset,
				Topic:   record.Topic,
			}
			for _, h := range record.Headers {
				evt.Headers[h.Key] = string(h.Value)
			}

			// Extract or generate correlation ID
			corrID := correlation.ExtractOrGenerate(evt.Headers)
			evt.CorrelationID = corrID.Value

			// Extract trace context from Kafka headers
			recordCtx := correlation.ExtractTraceContext(ctx, evt.Headers)

			// Start consumer span
			spanCtx, span := tracing.StartSpan(recordCtx, s.tracer, tracing.SpanKafkaConsume,
				trace.WithAttributes(
					tracing.KafkaTopicAttr(record.Topic),
					tracing.KafkaPartitionAttr(record.Partition),
					tracing.KafkaOffsetAttr(record.Offset),
					tracing.CorrelationAttr(corrID.Value),
				),
			)

			s.logger.Info("event received",
				"correlation_id", corrID.Value,
				"correlation_source", corrID.Source,
				"topic", record.Topic,
				"offset", record.Offset,
				"partition", record.Partition,
			)

			if err := handler(spanCtx, evt); err != nil {
				tracing.SetSpanError(span, err)
				span.End()
				s.logger.Error("handler error", "topic", record.Topic, "offset", record.Offset, "error", err)
				return
			}

			// Commit offset after successful handler execution (at-least-once)
			s.client.MarkCommitRecords(record)
			if err := s.client.CommitMarkedOffsets(ctx); err != nil {
				tracing.SetSpanError(span, err)
				s.logger.Error("commit error", "topic", record.Topic, "offset", record.Offset, "error", err)
			} else {
				tracing.SetSpanOK(span)
			}
			span.End()
		})

		// Check for cancellation after processing the batch, ensuring
		// all records from the last fetch are fully drained before exit.
		if ctx.Err() != nil {
			s.logger.Info("kafka source draining complete", "topic", s.topic)
			return ctx.Err()
		}
	}
}

// Close performs graceful shutdown of the Kafka client.
func (s *Source) Close() error {
	s.client.Close()
	return nil
}
