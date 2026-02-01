package kafka

import (
	"context"
	"errors"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
)

// mockProducer implements the producer interface for testing.
type mockProducer struct {
	results kgo.ProduceResults
	records []*kgo.Record
	closed  bool
}

func (m *mockProducer) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	m.records = append(m.records, rs...)
	return m.results
}

func (m *mockProducer) Close() {
	m.closed = true
}

func TestNewPublisher_EmptyBrokers(t *testing.T) {
	_, err := NewPublisher(nil)
	if err == nil {
		t.Fatal("expected error for empty brokers")
	}
}

func TestNewPublisher_ValidConfig(t *testing.T) {
	pub, err := NewPublisher([]string{"localhost:9092"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = pub.Close() }()
}

func TestPublisher_Close(t *testing.T) {
	pub, err := NewPublisher([]string{"localhost:9092"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pub.Close(); err != nil {
		t.Fatalf("close error: %v", err)
	}
}

func TestPublisher_Publish_Success(t *testing.T) {
	mp := &mockProducer{
		results: kgo.ProduceResults{{Record: &kgo.Record{}, Err: nil}},
	}
	pub := &Publisher{client: mp}

	err := pub.Publish(context.Background(), "test-topic", []byte("key"), []byte("value"), map[string]string{
		"ce-type":   "test.event",
		"ce-source": "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mp.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(mp.records))
	}
	if mp.records[0].Topic != "test-topic" {
		t.Errorf("expected topic 'test-topic', got %q", mp.records[0].Topic)
	}
	if string(mp.records[0].Key) != "key" {
		t.Errorf("expected key 'key', got %q", string(mp.records[0].Key))
	}
	if string(mp.records[0].Value) != "value" {
		t.Errorf("expected value 'value', got %q", string(mp.records[0].Value))
	}
	if len(mp.records[0].Headers) != 2 {
		t.Errorf("expected 2 headers, got %d", len(mp.records[0].Headers))
	}
}

func TestPublisher_Publish_Error(t *testing.T) {
	mp := &mockProducer{
		results: kgo.ProduceResults{{Record: &kgo.Record{}, Err: errors.New("broker unavailable")}},
	}
	pub := &Publisher{client: mp}

	err := pub.Publish(context.Background(), "test-topic", []byte("key"), []byte("value"), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "kafka publish: broker unavailable" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPublisher_Publish_NilHeaders(t *testing.T) {
	mp := &mockProducer{
		results: kgo.ProduceResults{{Record: &kgo.Record{}, Err: nil}},
	}
	pub := &Publisher{client: mp}

	err := pub.Publish(context.Background(), "test-topic", []byte("key"), []byte("value"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mp.records[0].Headers) != 0 {
		t.Errorf("expected no headers, got %d", len(mp.records[0].Headers))
	}
}

func TestPublisher_Close_Mock(t *testing.T) {
	mp := &mockProducer{}
	pub := &Publisher{client: mp}
	if err := pub.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mp.closed {
		t.Error("expected producer to be closed")
	}
}
