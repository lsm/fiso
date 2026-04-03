package delivery

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"
)

// KafkaTransactionalProducer is the minimal producer contract used for
// transactional publish via context propagation.
type KafkaTransactionalProducer interface {
	ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults
}

type kafkaProducerContextKey struct{}

// WithKafkaTransactionalProducer stores a transactional Kafka producer in ctx.
func WithKafkaTransactionalProducer(ctx context.Context, producer KafkaTransactionalProducer) context.Context {
	if producer == nil {
		return ctx
	}
	return context.WithValue(ctx, kafkaProducerContextKey{}, producer)
}

// KafkaTransactionalProducerFromContext extracts a transactional Kafka producer.
func KafkaTransactionalProducerFromContext(ctx context.Context) (KafkaTransactionalProducer, bool) {
	if ctx == nil {
		return nil, false
	}
	producer, ok := ctx.Value(kafkaProducerContextKey{}).(KafkaTransactionalProducer)
	return producer, ok
}
