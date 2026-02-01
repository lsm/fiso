package kafka

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lsm/fiso/internal/source"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Config holds Kafka source configuration.
type Config struct {
	Brokers       []string
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
}

// NewSource creates a new Kafka source.
func NewSource(cfg Config, logger *slog.Logger) (*Source, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("at least one broker is required")
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

	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ConsumerGroup(cfg.ConsumerGroup),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.ConsumeResetOffset(offset),
		kgo.DisableAutoCommit(),
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("kafka client: %w", err)
	}

	return &Source{
		client: client,
		topic:  cfg.Topic,
		logger: logger,
	}, nil
}

// Start begins consuming events from Kafka. Blocks until ctx is cancelled.
func (s *Source) Start(ctx context.Context, handler func(context.Context, source.Event) error) error {
	s.logger.Info("starting kafka consumer", "topic", s.topic)

	for {
		fetches := s.client.PollFetches(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if errs := fetches.Errors(); len(errs) > 0 {
			for _, err := range errs {
				s.logger.Error("fetch error", "topic", err.Topic, "partition", err.Partition, "error", err.Err)
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

			if err := handler(ctx, evt); err != nil {
				s.logger.Error("handler error", "topic", record.Topic, "offset", record.Offset, "error", err)
				return
			}

			// Commit offset after successful handler execution (at-least-once)
			s.client.MarkCommitRecords(record)
			if err := s.client.CommitMarkedOffsets(ctx); err != nil {
				s.logger.Error("commit error", "topic", record.Topic, "offset", record.Offset, "error", err)
			}
		})
	}
}

// Close performs graceful shutdown of the Kafka client.
func (s *Source) Close() error {
	s.client.Close()
	return nil
}
