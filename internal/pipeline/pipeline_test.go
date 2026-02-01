package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/source"
)

// --- Mocks ---

type mockSource struct {
	events []source.Event
}

func (m *mockSource) Start(ctx context.Context, handler func(context.Context, source.Event) error) error {
	for _, evt := range m.events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := handler(ctx, evt); err != nil {
			// Pipeline handler should not return errors — it handles them internally.
			return fmt.Errorf("unexpected handler error: %w", err)
		}
	}
	// Block until context is cancelled to simulate a real source.
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockSource) Close() error { return nil }

type mockTransformer struct {
	fn func(ctx context.Context, input []byte) ([]byte, error)
}

func (m *mockTransformer) Transform(ctx context.Context, input []byte) ([]byte, error) {
	return m.fn(ctx, input)
}

type mockSink struct {
	mu       sync.Mutex
	received []sinkMessage
	err      error
}

type sinkMessage struct {
	event   []byte
	headers map[string]string
}

func (m *mockSink) Deliver(_ context.Context, event []byte, headers map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.received = append(m.received, sinkMessage{event: event, headers: headers})
	return nil
}

func (m *mockSink) Close() error { return nil }

func (m *mockSink) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.received)
}

type mockPublisher struct {
	mu        sync.Mutex
	published []dlqMessage
	err       error
}

type dlqMessage struct {
	topic   string
	key     []byte
	value   []byte
	headers map[string]string
}

func (m *mockPublisher) Publish(_ context.Context, topic string, key, value []byte, headers map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.published = append(m.published, dlqMessage{topic: topic, key: key, value: value, headers: headers})
	return nil
}

func (m *mockPublisher) Close() error { return nil }

func (m *mockPublisher) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.published)
}

// --- Tests ---

func TestPipeline_HappyPath(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":{"id":"a"}}`), Topic: "orders"},
		},
	}
	transformer := &mockTransformer{
		fn: func(_ context.Context, input []byte) ([]byte, error) {
			return input, nil // passthrough
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow"}, src, transformer, sk, dlqHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatalf("expected 1 delivered event, got %d", sk.count())
	}
	if pub.count() != 0 {
		t.Fatalf("expected 0 DLQ events, got %d", pub.count())
	}
}

func TestPipeline_TransformError_SendsToDLQ(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"bad":"data"}`), Topic: "orders"},
		},
	}
	transformer := &mockTransformer{
		fn: func(_ context.Context, _ []byte) ([]byte, error) {
			return nil, fmt.Errorf("transform failed")
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "order-events"}, src, transformer, sk, dlqHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = p.Run(ctx)

	if sk.count() != 0 {
		t.Fatalf("expected 0 delivered events, got %d", sk.count())
	}
	if pub.count() != 1 {
		t.Fatalf("expected 1 DLQ event, got %d", pub.count())
	}

	msg := pub.published[0]
	if msg.topic != "fiso-dlq-order-events" {
		t.Errorf("expected DLQ topic fiso-dlq-order-events, got %s", msg.topic)
	}
	if msg.headers["fiso-error-code"] != "TRANSFORM_FAILED" {
		t.Errorf("expected error code TRANSFORM_FAILED, got %s", msg.headers["fiso-error-code"])
	}
}

func TestPipeline_SinkError_SendsToDLQ(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"ok"}`), Topic: "orders"},
		},
	}
	transformer := &mockTransformer{
		fn: func(_ context.Context, input []byte) ([]byte, error) {
			return input, nil
		},
	}
	sk := &mockSink{err: fmt.Errorf("sink unavailable")}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "order-events"}, src, transformer, sk, dlqHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = p.Run(ctx)

	if pub.count() != 1 {
		t.Fatalf("expected 1 DLQ event, got %d", pub.count())
	}
	if pub.published[0].headers["fiso-error-code"] != "SINK_DELIVERY_FAILED" {
		t.Errorf("expected SINK_DELIVERY_FAILED, got %s", pub.published[0].headers["fiso-error-code"])
	}
}

func TestPipeline_CloudEventWrapping(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"hello"}`), Topic: "orders"},
		},
	}
	transformer := &mockTransformer{
		fn: func(_ context.Context, input []byte) ([]byte, error) {
			return input, nil
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow", EventType: "order.created"}, src, transformer, sk, dlqHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("failed to unmarshal CloudEvent: %v", err)
	}

	if ce["specversion"] != "1.0" {
		t.Errorf("expected specversion 1.0, got %v", ce["specversion"])
	}
	if ce["type"] != "order.created" {
		t.Errorf("expected type order.created, got %v", ce["type"])
	}
	if ce["source"] != "fiso-flow/test-flow" {
		t.Errorf("expected source fiso-flow/test-flow, got %v", ce["source"])
	}
	if _, ok := ce["id"]; !ok {
		t.Error("expected id field in CloudEvent")
	}
	if _, ok := ce["time"]; !ok {
		t.Error("expected time field in CloudEvent")
	}
}

func TestPipeline_NilTransformer_Passthrough(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"raw"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow"}, src, nil, sk, dlqHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatalf("expected 1 delivered event, got %d", sk.count())
	}
}

func TestPipeline_MultipleEvents(t *testing.T) {
	events := make([]source.Event, 10)
	for i := range events {
		events[i] = source.Event{
			Key:   []byte(fmt.Sprintf("k%d", i)),
			Value: []byte(fmt.Sprintf(`{"data":{"n":%d}}`, i)),
			Topic: "test",
		}
	}

	src := &mockSource{events: events}
	transformer := &mockTransformer{
		fn: func(_ context.Context, input []byte) ([]byte, error) {
			return input, nil
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow"}, src, transformer, sk, dlqHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = p.Run(ctx)

	if sk.count() != 10 {
		t.Fatalf("expected 10 delivered events, got %d", sk.count())
	}
}
