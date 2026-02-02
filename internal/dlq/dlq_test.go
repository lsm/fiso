package dlq

import (
	"context"
	"fmt"
	"testing"
)

type mockPublisher struct {
	published []publishedMessage
	err       error
}

type publishedMessage struct {
	topic   string
	key     []byte
	value   []byte
	headers map[string]string
}

func (m *mockPublisher) Publish(_ context.Context, topic string, key, value []byte, headers map[string]string) error {
	if m.err != nil {
		return m.err
	}
	m.published = append(m.published, publishedMessage{
		topic:   topic,
		key:     key,
		value:   value,
		headers: headers,
	})
	return nil
}

func (m *mockPublisher) Close() error { return nil }

func TestSend_DefaultTopic(t *testing.T) {
	pub := &mockPublisher{}
	h := NewHandler(pub)

	err := h.Send(context.Background(), []byte("key-1"), []byte(`{"id":1}`), FailureInfo{
		OriginalTopic: "orders",
		ErrorCode:     "500",
		ErrorMessage:  "internal error",
		RetryCount:    3,
		FlowName:      "order-events",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(pub.published))
	}

	msg := pub.published[0]
	if msg.topic != "fiso-dlq-order-events" {
		t.Errorf("expected topic fiso-dlq-order-events, got %s", msg.topic)
	}
	if string(msg.key) != "key-1" {
		t.Errorf("expected key key-1, got %s", msg.key)
	}
	if string(msg.value) != `{"id":1}` {
		t.Errorf("expected value {\"id\":1}, got %s", msg.value)
	}
}

func TestSend_HeadersPopulated(t *testing.T) {
	pub := &mockPublisher{}
	h := NewHandler(pub)

	err := h.Send(context.Background(), nil, []byte(`{}`), FailureInfo{
		OriginalTopic: "payments",
		ErrorCode:     "TRANSFORM_FAILED",
		ErrorMessage:  "jq error: null",
		RetryCount:    0,
		FlowName:      "payment-flow",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headers := pub.published[0].headers

	tests := map[string]string{
		"fiso-original-topic": "payments",
		"fiso-error-code":     "TRANSFORM_FAILED",
		"fiso-error-message":  "jq error: null",
		"fiso-retry-count":    "0",
		"fiso-flow-name":      "payment-flow",
	}

	for k, want := range tests {
		got, ok := headers[k]
		if !ok {
			t.Errorf("missing header %s", k)
			continue
		}
		if got != want {
			t.Errorf("header %s: got %q, want %q", k, got, want)
		}
	}

	// fiso-failed-at should be present and non-empty
	if headers["fiso-failed-at"] == "" {
		t.Error("fiso-failed-at header is empty")
	}
}

func TestSend_CustomTopicFunc(t *testing.T) {
	pub := &mockPublisher{}
	h := NewHandler(pub, WithTopicFunc(func(flowName string) string {
		return "custom-dlq-" + flowName
	}))

	err := h.Send(context.Background(), nil, []byte(`{}`), FailureInfo{
		FlowName: "test-flow",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pub.published[0].topic != "custom-dlq-test-flow" {
		t.Errorf("expected custom topic, got %s", pub.published[0].topic)
	}
}

func TestSend_PublisherError(t *testing.T) {
	pub := &mockPublisher{err: fmt.Errorf("broker unavailable")}
	h := NewHandler(pub)

	err := h.Send(context.Background(), nil, []byte(`{}`), FailureInfo{
		FlowName: "test-flow",
	})
	if err == nil {
		t.Fatal("expected error when publisher fails")
	}
}

func TestClose(t *testing.T) {
	pub := &mockPublisher{}
	h := NewHandler(pub)
	if err := h.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNoopPublisher(t *testing.T) {
	var pub Publisher = &NoopPublisher{}
	if err := pub.Publish(context.Background(), "topic", nil, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := pub.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}
}

func TestNoopPublisher_WithHandler(t *testing.T) {
	h := NewHandler(&NoopPublisher{})
	err := h.Send(context.Background(), []byte("key"), []byte("val"), FailureInfo{
		FlowName:  "test",
		ErrorCode: "TEST",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}
}
