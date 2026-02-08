package async

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func (m *mockBroker) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

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

func TestPublish_WithEmptyPayload(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, "events")

	// Test with empty payload - this should trigger the set data error path
	// because json.RawMessage cannot be empty
	err := pub.Publish(context.Background(), "test.event", []byte{}, "corr-1")
	if err == nil {
		t.Fatal("expected error with empty payload")
	}
	if !strings.Contains(err.Error(), "set event data") {
		t.Errorf("expected 'set event data' error, got: %v", err)
	}
}

func TestPublish_WithInvalidJSONPayload(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, "events")

	// Test with invalid JSON payload - this should also trigger set data error
	err := pub.Publish(context.Background(), "test.event", []byte("invalid json"), "corr-1")
	if err == nil {
		t.Fatal("expected error with invalid JSON payload")
	}
	if !strings.Contains(err.Error(), "set event data") {
		t.Errorf("expected 'set event data' error, got: %v", err)
	}
}

func TestPublish_WithNilPayload(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, "events")

	// Test with nil payload
	err := pub.Publish(context.Background(), "test.event", nil, "corr-1")
	if err != nil {
		t.Fatalf("unexpected error with nil payload: %v", err)
	}

	if len(broker.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(broker.messages))
	}
}

func TestPublish_MultipleEvents(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, "events")

	// Publish multiple events
	for i := 0; i < 5; i++ {
		payload := []byte(`{"count": ` + string(rune('0'+i)) + `}`)
		err := pub.Publish(context.Background(), "test.event", payload, "")
		if err != nil {
			t.Fatalf("unexpected error on message %d: %v", i, err)
		}
	}

	if len(broker.messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(broker.messages))
	}

	// Verify all messages have different IDs
	ids := make(map[string]bool)
	for i, msg := range broker.messages {
		var ce map[string]interface{}
		if err := json.Unmarshal(msg.Value, &ce); err != nil {
			t.Fatalf("failed to unmarshal message %d: %v", i, err)
		}
		id := ce["id"].(string)
		if ids[id] {
			t.Errorf("duplicate CloudEvent ID found: %s", id)
		}
		ids[id] = true
	}
}

func TestPublish_CloudEventFields(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, "test-topic")

	payload := []byte(`{"key": "value"}`)
	err := pub.Publish(context.Background(), "test.event", payload, "test-corr-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(broker.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(broker.messages))
	}

	msg := broker.messages[0]
	var ce map[string]interface{}
	if err := json.Unmarshal(msg.Value, &ce); err != nil {
		t.Fatalf("failed to unmarshal CloudEvent: %v", err)
	}

	// Verify all CloudEvent required fields
	if ce["id"] == "" {
		t.Error("expected non-empty CloudEvent ID")
	}
	if ce["time"] == "" {
		t.Error("expected non-empty CloudEvent time")
	}

	// Verify data field contains the payload
	// When unmarshaling, json.RawMessage becomes the parsed JSON structure
	dataMap, ok := ce["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map[string]interface{}, got %T", ce["data"])
	}
	if dataMap["key"] != "value" {
		t.Errorf("expected data.key to be 'value', got %v", dataMap["key"])
	}
}

func TestPublish_NilKey(t *testing.T) {
	broker := &mockBroker{}
	pub := NewPublisher(broker, "events")

	err := pub.Publish(context.Background(), "test.event", []byte(`{}`), "corr-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(broker.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(broker.messages))
	}

	msg := broker.messages[0]
	// Key should be nil as per the Publish implementation
	if msg.Key != nil {
		t.Errorf("expected nil key, got %v", msg.Key)
	}
}

func TestClose_Error(t *testing.T) {
	// Test Close when broker returns an error
	broker := &mockBroker{err: errors.New("close failed")}
	pub := NewPublisher(broker, "events")

	err := pub.Close()
	if err == nil {
		t.Fatal("expected error from Close")
	}
	if err.Error() != "close failed" {
		t.Errorf("expected 'close failed' error, got %v", err)
	}
}
