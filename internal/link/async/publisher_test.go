package async

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

type mockBroker struct {
	mu       sync.Mutex
	messages []brokerMessage
	err      error
}

type brokerMessage struct {
	Topic   string
	Key     []byte
	Value   []byte
	Headers map[string]string
}

func (m *mockBroker) Publish(_ context.Context, topic string, key, value []byte, headers map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.messages = append(m.messages, brokerMessage{
		Topic:   topic,
		Key:     key,
		Value:   value,
		Headers: headers,
	})
	return nil
}

func (m *mockBroker) Close() error { return nil }

func TestPublish_Success(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, "events-topic")

	payload := []byte(`{"order_id": 123}`)
	err := pub.Publish(context.Background(), "order.create", payload, "corr-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(broker.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(broker.messages))
	}

	msg := broker.messages[0]
	if msg.Topic != "events-topic" {
		t.Errorf("expected topic events-topic, got %s", msg.Topic)
	}
	if msg.Headers["fiso-correlation-id"] != "corr-123" {
		t.Errorf("expected correlation-id corr-123, got %s", msg.Headers["fiso-correlation-id"])
	}
	if msg.Headers["content-type"] != "application/cloudevents+json" {
		t.Errorf("expected content-type header, got %s", msg.Headers["content-type"])
	}

	// Verify CloudEvent structure
	var ce map[string]interface{}
	if err := json.Unmarshal(msg.Value, &ce); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if ce["specversion"] != "1.0" {
		t.Errorf("expected specversion 1.0, got %v", ce["specversion"])
	}
	if ce["source"] != "fiso-link" {
		t.Errorf("expected source fiso-link, got %v", ce["source"])
	}
	if ce["type"] != "order.create" {
		t.Errorf("expected type order.create, got %v", ce["type"])
	}
}

func TestPublish_GeneratesCorrelationID(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, "events")

	err := pub.Publish(context.Background(), "test", []byte(`{}`), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg := broker.messages[0]
	if msg.Headers["fiso-correlation-id"] == "" {
		t.Error("expected auto-generated correlation-id")
	}
}

func TestPublish_BrokerError(t *testing.T) {
	broker := &mockBroker{err: errors.New("broker down")}
	pub := NewPublisher(broker, "events")

	err := pub.Publish(context.Background(), "test", []byte(`{}`), "corr-1")
	if err == nil {
		t.Fatal("expected error from broker")
	}
}

func TestPublish_Close(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, "events")
	if err := pub.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
