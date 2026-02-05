package kafka

import (
	"context"
	"fmt"

	"github.com/lsm/fiso/internal/source/kafka"
)

// publisher abstracts the kafka publisher for testing.
type publisher interface {
	Publish(ctx context.Context, topic string, key, value []byte, headers map[string]string) error
	Close() error
}

// Config holds Kafka sink configuration.
type Config struct {
	Brokers []string
	Topic   string
}

// Sink delivers events to a Kafka topic.
type Sink struct {
	publisher publisher
	topic     string
}

// NewSink creates a new Kafka sink.
func NewSink(cfg Config) (*Sink, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("brokers are required")
	}
	if cfg.Topic == "" {
		return nil, fmt.Errorf("topic is required")
	}

	pub, err := kafka.NewPublisher(cfg.Brokers)
	if err != nil {
		return nil, fmt.Errorf("kafka publisher: %w", err)
	}

	return &Sink{
		publisher: pub,
		topic:     cfg.Topic,
	}, nil
}

// Deliver sends an event to the configured Kafka topic.
func (s *Sink) Deliver(ctx context.Context, event []byte, headers map[string]string) error {
	return s.publisher.Publish(ctx, s.topic, nil, event, headers)
}

// Close shuts down the Kafka publisher.
func (s *Sink) Close() error {
	return s.publisher.Close()
}
