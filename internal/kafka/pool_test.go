package kafka

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
)

// mockProducer implements the producer interface for testing.
type mockProducer struct {
	records []*kgo.Record
	mu      sync.Mutex
	closed  bool
}

func (m *mockProducer) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, rs...)
	return kgo.ProduceResults{}
}

func (m *mockProducer) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

func TestPooledPublisher_Publish(t *testing.T) {
	mock := &mockProducer{}
	pub := &PooledPublisher{client: mock, name: "test"}

	headers := map[string]string{"key1": "val1", "key2": "val2"}
	err := pub.Publish(context.Background(), "test-topic", []byte("key"), []byte("value"), headers)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if len(mock.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(mock.records))
	}

	rec := mock.records[0]
	if rec.Topic != "test-topic" {
		t.Errorf("Topic = %q, want %q", rec.Topic, "test-topic")
	}
	if string(rec.Key) != "key" {
		t.Errorf("Key = %q, want %q", string(rec.Key), "key")
	}
	if string(rec.Value) != "value" {
		t.Errorf("Value = %q, want %q", string(rec.Value), "value")
	}
	if len(rec.Headers) != 2 {
		t.Errorf("Headers len = %d, want 2", len(rec.Headers))
	}
}

func TestPooledPublisher_Close(t *testing.T) {
	mock := &mockProducer{}
	pub := &PooledPublisher{client: mock, name: "test"}

	// Close should be a no-op for pooled publishers
	err := pub.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Underlying client should NOT be closed
	if mock.closed {
		t.Error("underlying client was closed, but pooled publisher Close() should be no-op")
	}
}

func TestPublisherPool_Get(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register("test", &ClusterConfig{
		Brokers: []string{"localhost:9092"},
	})

	pool := NewPublisherPool(registry)
	defer func() { _ = pool.Close() }()

	// Note: This will fail to connect since there's no actual Kafka
	// but it tests the registry lookup path
	_, err := pool.Get("test")
	// We expect an error because there's no actual Kafka broker
	if err == nil {
		t.Log("Get() succeeded - Kafka broker is available")
	} else {
		// This is expected in unit tests
		t.Logf("Get() error (expected): %v", err)
	}
}

func TestPublisherPool_GetNotFound(t *testing.T) {
	registry := NewRegistry()
	pool := NewPublisherPool(registry)
	defer func() { _ = pool.Close() }()

	_, err := pool.Get("nonexistent")
	if err == nil {
		t.Fatal("Get() error = nil, want not found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Get() error = %v, want error containing 'not found'", err)
	}
}

func TestPublisherPool_GetForConfig(t *testing.T) {
	registry := NewRegistry()
	pool := NewPublisherPool(registry)
	defer func() { _ = pool.Close() }()

	cfg := &ClusterConfig{
		Brokers: []string{"localhost:9092"},
	}

	// Note: This will fail to connect since there's no actual Kafka
	_, err := pool.GetForConfig(cfg)
	if err == nil {
		t.Log("GetForConfig() succeeded - Kafka broker is available")
	} else {
		t.Logf("GetForConfig() error (expected): %v", err)
	}
}

func TestPublisherPool_Close(t *testing.T) {
	registry := NewRegistry()
	pool := NewPublisherPool(registry)

	// Close empty pool should not error
	err := pool.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// errorProducer is a mock that returns errors from ProduceSync.
type errorProducer struct {
	err error
}

func (e *errorProducer) ProduceSync(_ context.Context, _ ...*kgo.Record) kgo.ProduceResults {
	return kgo.ProduceResults{{Err: e.err}}
}

func (e *errorProducer) Close() {}

func TestPooledPublisher_PublishError(t *testing.T) {
	mock := &errorProducer{err: fmt.Errorf("broker unavailable")}
	pub := &PooledPublisher{client: mock, name: "test"}

	err := pub.Publish(context.Background(), "test-topic", []byte("key"), []byte("value"), nil)
	if err == nil {
		t.Fatal("expected error from Publish")
	}
	if !strings.Contains(err.Error(), "broker unavailable") {
		t.Errorf("error = %v, want error containing 'broker unavailable'", err)
	}
}

func TestPooledPublisher_PublishNoHeaders(t *testing.T) {
	mock := &mockProducer{}
	pub := &PooledPublisher{client: mock, name: "test"}

	err := pub.Publish(context.Background(), "test-topic", []byte("key"), []byte("value"), nil)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if len(mock.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(mock.records))
	}

	if len(mock.records[0].Headers) != 0 {
		t.Errorf("expected no headers, got %d", len(mock.records[0].Headers))
	}
}
