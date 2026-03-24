package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/lsm/fiso/internal/correlation"
	"github.com/lsm/fiso/internal/kafka"
	"github.com/lsm/fiso/internal/source"
	"github.com/lsm/fiso/internal/tracing"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Config holds Kafka source configuration.
type Config struct {
	Cluster       *kafka.ClusterConfig // Cluster config with auth/TLS (required)
	Topic         string
	ConsumerGroup string
	StartOffset   any // "earliest", "latest", or non-negative numeric offset (default: "latest")
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

	offset, err := resolveStartOffset(cfg.StartOffset)
	if err != nil {
		return nil, fmt.Errorf("invalid startOffset: %w", err)
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
		tracer: noop.NewTracerProvider().Tracer("kafka-source"),
	}, nil
}

func resolveStartOffset(raw any) (kgo.Offset, error) {
	if raw == nil {
		return kgo.NewOffset().AtEnd(), nil
	}

	if n, ok, err := parseNumericStartOffset(raw); ok || err != nil {
		if err != nil {
			return kgo.Offset{}, err
		}
		return kgo.NewOffset().At(n), nil
	}

	s, ok := raw.(string)
	if !ok {
		return kgo.Offset{}, fmt.Errorf("must be \"earliest\", \"latest\", or a non-negative integer (got %T)", raw)
	}

	normalized := strings.ToLower(strings.TrimSpace(s))
	switch normalized {
	case "", "latest":
		return kgo.NewOffset().AtEnd(), nil
	case "earliest":
		return kgo.NewOffset().AtStart(), nil
	default:
		n, err := strconv.ParseInt(normalized, 10, 64)
		if err != nil {
			return kgo.Offset{}, fmt.Errorf("must be \"earliest\", \"latest\", or a non-negative integer (got %q)", s)
		}
		if n < 0 {
			return kgo.Offset{}, fmt.Errorf("must be >= 0 when numeric (got %d)", n)
		}
		return kgo.NewOffset().At(n), nil
	}
}

func parseNumericStartOffset(raw any) (offset int64, ok bool, err error) {
	switch v := raw.(type) {
	case int:
		if v < 0 {
			return 0, true, fmt.Errorf("must be >= 0 when numeric (got %d)", v)
		}
		return int64(v), true, nil
	case int8:
		if v < 0 {
			return 0, true, fmt.Errorf("must be >= 0 when numeric (got %d)", v)
		}
		return int64(v), true, nil
	case int16:
		if v < 0 {
			return 0, true, fmt.Errorf("must be >= 0 when numeric (got %d)", v)
		}
		return int64(v), true, nil
	case int32:
		if v < 0 {
			return 0, true, fmt.Errorf("must be >= 0 when numeric (got %d)", v)
		}
		return int64(v), true, nil
	case int64:
		if v < 0 {
			return 0, true, fmt.Errorf("must be >= 0 when numeric (got %d)", v)
		}
		return v, true, nil
	case uint:
		if uint64(v) > uint64(math.MaxInt64) {
			return 0, true, fmt.Errorf("numeric startOffset overflows int64: %d", v)
		}
		return int64(v), true, nil
	case uint8:
		return int64(v), true, nil
	case uint16:
		return int64(v), true, nil
	case uint32:
		return int64(v), true, nil
	case uint64:
		if v > uint64(math.MaxInt64) {
			return 0, true, fmt.Errorf("numeric startOffset overflows int64: %d", v)
		}
		return int64(v), true, nil
	case float32:
		if v < 0 {
			return 0, true, fmt.Errorf("must be >= 0 when numeric (got %v)", v)
		}
		if math.Trunc(float64(v)) != float64(v) {
			return 0, true, fmt.Errorf("must be an integer when numeric (got %v)", v)
		}
		if float64(v) > math.MaxInt64 {
			return 0, true, fmt.Errorf("numeric startOffset overflows int64: %v", v)
		}
		return int64(v), true, nil
	case float64:
		if v < 0 {
			return 0, true, fmt.Errorf("must be >= 0 when numeric (got %v)", v)
		}
		if math.Trunc(v) != v {
			return 0, true, fmt.Errorf("must be an integer when numeric (got %v)", v)
		}
		if v > math.MaxInt64 {
			return 0, true, fmt.Errorf("numeric startOffset overflows int64: %v", v)
		}
		return int64(v), true, nil
	default:
		return 0, false, nil
	}
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
