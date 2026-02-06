package kafka

import (
	"context"
	"fmt"

	"github.com/lsm/fiso/internal/kafka"
	"github.com/twmb/franz-go/pkg/kgo"
)

// producer abstracts the kafka client methods used by Publisher for testing.
type producer interface {
	ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults
	Close()
}

// Publisher publishes messages to Kafka topics. Implements dlq.Publisher.
type Publisher struct {
	client producer
}

// NewPublisher creates a new Kafka publisher with cluster configuration.
// This supports SASL authentication and TLS.
func NewPublisher(cluster *kafka.ClusterConfig) (*Publisher, error) {
	if cluster == nil {
		return nil, fmt.Errorf("cluster config is required")
	}

	opts, err := kafka.ClientOptions(cluster)
	if err != nil {
		return nil, fmt.Errorf("cluster options: %w", err)
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("kafka publisher client: %w", err)
	}

	return &Publisher{client: client}, nil
}

// Publish sends a message to the specified Kafka topic.
func (p *Publisher) Publish(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
	record := &kgo.Record{
		Topic: topic,
		Key:   key,
		Value: value,
	}
	for k, v := range headers {
		record.Headers = append(record.Headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}

	results := p.client.ProduceSync(ctx, record)
	if err := results.FirstErr(); err != nil {
		return fmt.Errorf("kafka publish: %w", err)
	}
	return nil
}

// Close shuts down the publisher.
func (p *Publisher) Close() error {
	p.client.Close()
	return nil
}
