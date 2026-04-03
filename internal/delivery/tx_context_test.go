package delivery

import (
	"context"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
)

type mockTxProducer struct{}

func (m *mockTxProducer) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	results := make(kgo.ProduceResults, len(rs))
	for i, r := range rs {
		results[i] = kgo.ProduceResult{Record: r}
	}
	return results
}

func TestWithKafkaTransactionalProducer_NilProducer(t *testing.T) {
	base := context.Background()
	ctx := WithKafkaTransactionalProducer(base, nil)
	if ctx != base {
		t.Fatal("expected original context when producer is nil")
	}
}

func TestKafkaTransactionalProducerFromContext(t *testing.T) {
	producer := &mockTxProducer{}
	ctx := WithKafkaTransactionalProducer(context.Background(), producer)

	got, ok := KafkaTransactionalProducerFromContext(ctx)
	if !ok {
		t.Fatal("expected producer in context")
	}
	if got != producer {
		t.Fatal("expected same producer instance")
	}
}

func TestKafkaTransactionalProducerFromContext_NilAndMissing(t *testing.T) {
	if _, ok := KafkaTransactionalProducerFromContext(nil); ok { //nolint:staticcheck // explicitly testing nil-context guard in KafkaTransactionalProducerFromContext
		t.Fatal("expected missing producer for nil context")
	}
	if _, ok := KafkaTransactionalProducerFromContext(context.Background()); ok {
		t.Fatal("expected missing producer for context without value")
	}
}
