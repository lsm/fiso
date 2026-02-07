package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/interceptor"
	"github.com/lsm/fiso/internal/source"
)

// --- Mocks ---

type mockSource struct {
	events   []source.Event
	closeErr error
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

func (m *mockSource) Close() error { return m.closeErr }

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
	closeErr error
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

func (m *mockSink) Close() error { return m.closeErr }

func (m *mockSink) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.received)
}

type mockPublisher struct {
	mu        sync.Mutex
	published []dlqMessage
	err       error
	closeErr  error
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

func (m *mockPublisher) Close() error { return m.closeErr }

var _ dlq.Publisher = (*mockPublisher)(nil)

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

	p := New(Config{FlowName: "test-flow"}, src, transformer, sk, dlqHandler, nil)

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

	p := New(Config{FlowName: "order-events"}, src, transformer, sk, dlqHandler, nil)

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

	p := New(Config{FlowName: "order-events"}, src, transformer, sk, dlqHandler, nil)

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

	p := New(Config{FlowName: "test-flow", EventType: "order.created"}, src, transformer, sk, dlqHandler, nil)

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

	p := New(Config{FlowName: "test-flow"}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatalf("expected 1 delivered event, got %d", sk.count())
	}
}

func TestPipeline_DLQPublishError(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"ok"}`), Topic: "orders"},
		},
	}
	transformer := &mockTransformer{
		fn: func(_ context.Context, _ []byte) ([]byte, error) {
			return nil, fmt.Errorf("transform error")
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{err: fmt.Errorf("dlq unavailable")}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow"}, src, transformer, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// Should not panic even when DLQ publish fails
	_ = p.Run(ctx)
}

func TestPipeline_DefaultEventType(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"x"}`), Topic: "test"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test"}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 event")
	}
	var ce map[string]interface{}
	_ = json.Unmarshal(sk.received[0].event, &ce)
	if ce["type"] != "fiso.event" {
		t.Errorf("expected default type 'fiso.event', got %v", ce["type"])
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

	p := New(Config{FlowName: "test-flow"}, src, transformer, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = p.Run(ctx)

	if sk.count() != 10 {
		t.Fatalf("expected 10 delivered events, got %d", sk.count())
	}
}

func TestPipeline_Shutdown_ClosesAll(t *testing.T) {
	src := &mockSource{}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow"}, src, nil, sk, dlqHandler, nil)

	err := p.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPipeline_PropagateErrors_ReturnsToSource(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"bad":"data"}`), Topic: "http"},
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

	p := New(Config{FlowName: "http-flow", PropagateErrors: true}, src, transformer, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := p.Run(ctx)
	if err == nil {
		t.Fatal("expected error from Run when PropagateErrors is true")
	}
	if !strings.Contains(err.Error(), "transform failed") {
		t.Errorf("expected error to contain 'transform failed', got %s", err.Error())
	}
	// DLQ should still receive the event
	if pub.count() != 1 {
		t.Fatalf("expected 1 DLQ event, got %d", pub.count())
	}
}

func TestPipeline_PropagateErrors_HappyPathNoError(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"ok"}`), Topic: "http"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "http-flow", PropagateErrors: true}, src, nil, sk, dlqHandler, nil)

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

func TestPipeline_CloudEventsOverrides_Static(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"order_id":"abc"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			Type:    "order.created",
			Source:  "my-service",
			Subject: "orders",
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}
	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ce["type"] != "order.created" {
		t.Errorf("expected type 'order.created', got %v", ce["type"])
	}
	if ce["source"] != "my-service" {
		t.Errorf("expected source 'my-service', got %v", ce["source"])
	}
	if ce["subject"] != "orders" {
		t.Errorf("expected subject 'orders', got %v", ce["subject"])
	}
}

func TestPipeline_CloudEventsOverrides_DynamicCEL(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"order_id":"abc-123","event_type":"order.shipped"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			Type:    "data.event_type",
			Subject: "data.order_id",
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}
	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ce["type"] != "order.shipped" {
		t.Errorf("expected type 'order.shipped', got %v", ce["type"])
	}
	if ce["subject"] != "abc-123" {
		t.Errorf("expected subject 'abc-123', got %v", ce["subject"])
	}
	// Source should be the default since no override was set
	if ce["source"] != "fiso-flow/test-flow" {
		t.Errorf("expected source 'fiso-flow/test-flow', got %v", ce["source"])
	}
}

func TestPipeline_CloudEventsOverrides_MissingFieldFallback(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"order_id":"abc"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			Type:    "data.nonexistent",
			Subject: "data.also_missing",
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}
	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// CEL evaluation returns empty string for missing fields, which becomes default or empty
	if ce["type"] != "" && ce["type"] != "fiso.event" {
		t.Errorf("expected empty or default type, got %v", ce["type"])
	}
	// Subject field is optional and not set when empty
	if subject, ok := ce["subject"]; ok && subject != "" && subject != nil {
		t.Errorf("expected subject to be unset, empty, or nil, got %v", subject)
	}
}

func TestPipeline_CloudEventsOverrides_WithTransform(t *testing.T) {
	// Verifies that CE overrides resolve against the ORIGINAL input, not the transformed output
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"order_id":"orig-123","name":"Alice"}`), Topic: "orders"},
		},
	}
	transformer := &mockTransformer{
		fn: func(_ context.Context, _ []byte) ([]byte, error) {
			// Transform completely replaces the payload — original fields not present
			return []byte(`{"transformed":true}`), nil
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			Subject: "data.order_id",
		},
	}, src, transformer, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}
	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Subject should be resolved from original input, not transformed output
	if ce["subject"] != "orig-123" {
		t.Errorf("expected subject 'orig-123' (from original), got %v", ce["subject"])
	}
	// Data should be the transformed output
	dataBytes, _ := json.Marshal(ce["data"])
	if !strings.Contains(string(dataBytes), "transformed") {
		t.Errorf("expected transformed data, got %s", string(dataBytes))
	}
}

func TestPipeline_CloudEventsOverrides_NilNoRegression(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"ok"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow", EventType: "test.event"}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}
	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ce["type"] != "test.event" {
		t.Errorf("expected type 'test.event', got %v", ce["type"])
	}
	if ce["source"] != "fiso-flow/test-flow" {
		t.Errorf("expected source 'fiso-flow/test-flow', got %v", ce["source"])
	}
	if _, ok := ce["subject"]; ok {
		t.Error("expected no subject field when CloudEvents is nil")
	}
}

func TestPipeline_CloudEventsOverrides_InvalidJSON(t *testing.T) {
	// When original input is not valid JSON, CE overrides should fall back to literal values.
	// A transformer converts non-JSON input into valid JSON for the CloudEvent data field.
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`not valid json`), Topic: "orders"},
		},
	}
	transformer := &mockTransformer{
		fn: func(_ context.Context, _ []byte) ([]byte, error) {
			return []byte(`{"converted":true}`), nil
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			Type:    "custom.event",
			Source:  "my-source",
			Subject: "my-subject",
		},
	}, src, transformer, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}
	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Should use literal values since original JSON parse failed
	if ce["type"] != "custom.event" {
		t.Errorf("expected type 'custom.event', got %v", ce["type"])
	}
	if ce["source"] != "my-source" {
		t.Errorf("expected source 'my-source', got %v", ce["source"])
	}
	if ce["subject"] != "my-subject" {
		t.Errorf("expected subject 'my-subject', got %v", ce["subject"])
	}
}

func TestPipeline_CloudEventsOverrides_PartialOverrides(t *testing.T) {
	// Only override Type, leave Source and Subject as defaults
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"ok"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			Type: "order.created",
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}
	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ce["type"] != "order.created" {
		t.Errorf("expected type 'order.created', got %v", ce["type"])
	}
	// Source should remain the default
	if ce["source"] != "fiso-flow/test-flow" {
		t.Errorf("expected source 'fiso-flow/test-flow', got %v", ce["source"])
	}
	// No subject should be set
	if _, ok := ce["subject"]; ok {
		t.Error("expected no subject field when Subject override is empty")
	}
}

func TestPipeline_CloudEventsOverrides_CustomID(t *testing.T) {
	// Test custom CloudEvent ID using CEL for idempotency
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"eventId":"evt-123","CTN":"456"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			ID: "data.eventId",
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}
	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ce["id"] != "evt-123" {
		t.Errorf("expected id 'evt-123', got %v", ce["id"])
	}
}

func TestPipeline_CloudEventsOverrides_StaticID(t *testing.T) {
	// Test static CloudEvent ID (not recommended, but should work)
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"test"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			ID: "static-id-123",
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}
	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ce["id"] != "static-id-123" {
		t.Errorf("expected id 'static-id-123', got %v", ce["id"])
	}
}

func TestPipeline_CloudEventsID_WithTransform_Idempotency(t *testing.T) {
	// Real-world pattern: Original input has idempotency key pre-constructed,
	// CloudEvent ID references it, and transform reshapes the data
	src := &mockSource{
		events: []source.Event{
			{
				Key: []byte("k1"),
				// Upstream system already combines fields into idempotencyKey
				Value: []byte(`{"idempotencyKey":"evt-123-456","eventId":"evt-123","CTN":"456","order":{"id":"ord-789","total":100}}`),
				Topic: "orders",
			},
		},
	}

	// Transform reshapes data but idempotencyKey is preserved
	transformer := &mockTransformer{
		fn: func(_ context.Context, input []byte) ([]byte, error) {
			var data map[string]interface{}
			if err := json.Unmarshal(input, &data); err != nil {
				return nil, err
			}

			output := map[string]interface{}{
				"idempotencyKey": data["idempotencyKey"], // Preserve the key
				"order":          data["order"],
			}
			return json.Marshal(output)
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "order-processing",
		CloudEvents: &CloudEventsOverrides{
			ID:   "data.idempotencyKey", // Reference from ORIGINAL input
			Type: "order.created",
		},
	}, src, transformer, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	// Verify CloudEvent ID uses the idempotency key from original input
	if ce["id"] != "evt-123-456" {
		t.Errorf("expected CloudEvent id 'evt-123-456', got %v", ce["id"])
	}
	if ce["type"] != "order.created" {
		t.Errorf("expected type 'order.created', got %v", ce["type"])
	}
	if ce["source"] != "fiso-flow/order-processing" {
		t.Errorf("expected source 'fiso-flow/order-processing', got %v", ce["source"])
	}

	// Verify the data contains the transformed payload
	data, ok := ce["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", ce["data"])
	}
	if data["idempotencyKey"] != "evt-123-456" {
		t.Errorf("expected data.idempotencyKey 'evt-123-456', got %v", data["idempotencyKey"])
	}

	// Order data should be preserved
	order, ok := data["order"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected order to be map, got %T", data["order"])
	}
	if order["id"] != "ord-789" {
		t.Errorf("expected order.id 'ord-789', got %v", order["id"])
	}
}

func TestPipeline_CloudEventsID_ResolvesFromOriginalInput(t *testing.T) {
	// Verify that CloudEvent ID is resolved from ORIGINAL input, not transformed output
	// This matches the behavior of other CloudEvent overrides (type, source, subject)
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"requestId":"req-999","payload":"data"}`),
				Topic: "requests",
			},
		},
	}

	// Transformer completely replaces the payload
	transformer := &mockTransformer{
		fn: func(_ context.Context, _ []byte) ([]byte, error) {
			return []byte(`{"transformed":true,"newField":"value"}`), nil
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "request-processor",
		CloudEvents: &CloudEventsOverrides{
			ID:      "data.requestId", // Should resolve from ORIGINAL input
			Subject: "data.payload",   // Also from original
		},
	}, src, transformer, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	// CloudEvent ID should come from original input, not transformed output
	if ce["id"] != "req-999" {
		t.Errorf("expected id 'req-999' from original input, got %v", ce["id"])
	}
	if ce["subject"] != "data" {
		t.Errorf("expected subject 'data' from original input, got %v", ce["subject"])
	}

	// But the data field should contain the TRANSFORMED payload
	data, ok := ce["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", ce["data"])
	}
	if data["transformed"] != true {
		t.Errorf("expected data.transformed=true, got %v", data["transformed"])
	}
	if _, exists := data["requestId"]; exists {
		t.Error("original requestId should not be in transformed data")
	}
}

func TestPipeline_CloudEventsID_CELCombineFields(t *testing.T) {
	// Test CEL expression combining multiple fields for idempotency
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"eventId":"evt-123","CTN":"456","order":{"id":"ord-789"}}`),
				Topic: "orders",
			},
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "order-processing",
		CloudEvents: &CloudEventsOverrides{
			ID:      `data.eventId + "-" + data.CTN`, // CEL expression!
			Type:    "order.created",
			Subject: "data.order.id",
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	// Verify CloudEvent ID was constructed from CEL expression
	if ce["id"] != "evt-123-456" {
		t.Errorf("expected combined id 'evt-123-456', got %v", ce["id"])
	}
	if ce["subject"] != "ord-789" {
		t.Errorf("expected subject 'ord-789', got %v", ce["subject"])
	}
}

func TestPipeline_CloudEventsID_CELConditional(t *testing.T) {
	// Test CEL conditional expression for dynamic type
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"amount":1500}`), Topic: "payments"},
			{Key: []byte("k2"), Value: []byte(`{"amount":500}`), Topic: "payments"},
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "payment-processor",
		CloudEvents: &CloudEventsOverrides{
			Type: `data.amount > 1000 ? "payment.high-value" : "payment.standard"`,
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 2 {
		t.Fatalf("expected 2 delivered events, got %d", sk.count())
	}

	// First event: high value
	var ce1 map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce1); err != nil {
		t.Fatalf("unmarshal first event: %v", err)
	}
	if ce1["type"] != "payment.high-value" {
		t.Errorf("expected type 'payment.high-value', got %v", ce1["type"])
	}

	// Second event: standard
	var ce2 map[string]interface{}
	if err := json.Unmarshal(sk.received[1].event, &ce2); err != nil {
		t.Fatalf("unmarshal second event: %v", err)
	}
	if ce2["type"] != "payment.standard" {
		t.Errorf("expected type 'payment.standard', got %v", ce2["type"])
	}
}

func TestPipeline_CloudEventsID_CELSimpleField(t *testing.T) {
	// Verify simple field access with CEL
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"requestId":"req-999"}`), Topic: "requests"},
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "request-processor",
		CloudEvents: &CloudEventsOverrides{
			ID: "data.requestId", // Simple field access
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	if ce["id"] != "req-999" {
		t.Errorf("expected id 'req-999', got %v", ce["id"])
	}
}

func TestPipeline_CloudEventsID_CELStringOperations(t *testing.T) {
	// Test CEL string operations
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"region":"us-west","service":"payments"}`),
				Topic: "events",
			},
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "regional-processor",
		CloudEvents: &CloudEventsOverrides{
			Source: `"service-" + data.region`,
			Type:   `data.service + ".processed"`,
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	if ce["source"] != "service-us-west" {
		t.Errorf("expected source 'service-us-west', got %v", ce["source"])
	}
	if ce["type"] != "payments.processed" {
		t.Errorf("expected type 'payments.processed', got %v", ce["type"])
	}
}

func TestPipeline_Shutdown_PropagatesErrors(t *testing.T) {
	src := &mockSource{closeErr: errors.New("source close failed")}
	sk := &mockSink{closeErr: errors.New("sink close failed")}
	pub := &mockPublisher{closeErr: errors.New("dlq close failed")}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow"}, src, nil, sk, dlqHandler, nil)

	err := p.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected error from shutdown")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "source close failed") {
		t.Errorf("expected source close error, got %s", errMsg)
	}
	if !strings.Contains(errMsg, "sink close failed") {
		t.Errorf("expected sink close error, got %s", errMsg)
	}
	if !strings.Contains(errMsg, "dlq close failed") {
		t.Errorf("expected dlq close error, got %s", errMsg)
	}
}

func TestPipeline_Shutdown_WithInterceptorsAndAllErrors(t *testing.T) {
	src := &mockSource{closeErr: errors.New("source close failed")}
	sk := &mockSink{closeErr: errors.New("sink close failed")}
	pub := &mockPublisher{closeErr: errors.New("dlq close failed")}
	dlqHandler := dlq.NewHandler(pub)

	// Create an interceptor that will fail on close
	ic := &closingInterceptor{closeErr: errors.New("interceptor close failed")}
	chain := interceptor.NewChain(ic)

	p := New(Config{FlowName: "test-flow"}, src, nil, sk, dlqHandler, chain)

	err := p.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected error from shutdown")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "source close failed") {
		t.Errorf("expected source close error, got %s", errMsg)
	}
	if !strings.Contains(errMsg, "interceptor close failed") {
		t.Errorf("expected interceptor close error, got %s", errMsg)
	}
	if !strings.Contains(errMsg, "sink close failed") {
		t.Errorf("expected sink close error, got %s", errMsg)
	}
	if !strings.Contains(errMsg, "dlq close failed") {
		t.Errorf("expected dlq close error, got %s", errMsg)
	}
}

func TestPipeline_CloudEventsData_CELCustomField(t *testing.T) {
	// Test custom data field using CEL expression to extract a nested field
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"metadata":{"id":"meta-123"},"payload":{"user":"alice","action":"login"}}`),
				Topic: "events",
			},
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			Data: "data.payload", // Use only payload as data
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	// Data should contain only the payload field
	data, ok := ce["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", ce["data"])
	}
	if data["user"] != "alice" {
		t.Errorf("expected data.user 'alice', got %v", data["user"])
	}
	if data["action"] != "login" {
		t.Errorf("expected data.action 'login', got %v", data["action"])
	}
	// Metadata should NOT be in data
	if _, exists := data["metadata"]; exists {
		t.Error("metadata should not be in data field")
	}
}

func TestPipeline_CloudEventsData_CELNestedField(t *testing.T) {
	// Test custom data field using CEL to extract a nested field
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"wrapper":{"inner":{"value":"extracted"}}}`),
				Topic: "events",
			},
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			Data: "data.wrapper.inner", // CEL extraction
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	// Data should contain only the extracted inner object
	data, ok := ce["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", ce["data"])
	}
	if data["value"] != "extracted" {
		t.Errorf("expected data.value 'extracted', got %v", data["value"])
	}
}

func TestPipeline_CloudEventsData_EntireInput(t *testing.T) {
	// Test using entire original input as data (CEL "data")
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"field1":"value1","field2":"value2"}`),
				Topic: "events",
			},
		},
	}

	transformer := &mockTransformer{
		fn: func(_ context.Context, _ []byte) ([]byte, error) {
			// Transform completely changes the payload
			return []byte(`{"transformed":"yes"}`), nil
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			Data: "data", // Use entire original input as data
		},
	}, src, transformer, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	// Data should be the ORIGINAL input, not the transformed output
	dataMap, ok := ce["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", ce["data"])
	}
	if dataMap["field1"] != "value1" {
		t.Errorf("expected data.field1 'value1', got %v", dataMap["field1"])
	}
	if dataMap["field2"] != "value2" {
		t.Errorf("expected data.field2 'value2', got %v", dataMap["field2"])
	}
	// Transformed field should NOT be present
	if _, exists := dataMap["transformed"]; exists {
		t.Error("transformed field should not be in data (should use original)")
	}
}

func TestPipeline_CloudEventsData_DefaultBehavior(t *testing.T) {
	// Test default behavior: when Data is not specified, use transformed payload
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"original":"input"}`),
				Topic: "events",
			},
		},
	}

	transformer := &mockTransformer{
		fn: func(_ context.Context, _ []byte) ([]byte, error) {
			return []byte(`{"transformed":"output"}`), nil
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	// No Data field specified in CloudEventsOverrides
	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			Type: "test.event",
		},
	}, src, transformer, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	// Data should be the TRANSFORMED payload (default behavior)
	data, ok := ce["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", ce["data"])
	}
	if data["transformed"] != "output" {
		t.Errorf("expected data.transformed 'output', got %v", data["transformed"])
	}
	// Original field should NOT be present
	if _, exists := data["original"]; exists {
		t.Error("original field should not be in data (should use transformed)")
	}
}

func TestPipeline_CloudEventsDataContentType_Custom(t *testing.T) {
	// Test custom datacontenttype
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"contentType":"application/xml","data":"<xml/>"}`),
				Topic: "events",
			},
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			DataContentType: "data.contentType", // Extract from input
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	if ce["datacontenttype"] != "application/xml" {
		t.Errorf("expected datacontenttype 'application/xml', got %v", ce["datacontenttype"])
	}
}

func TestPipeline_CloudEventsDataContentType_CEL(t *testing.T) {
	// Test CEL expression for datacontenttype
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"format":"xml","data":"test"}`),
				Topic: "events",
			},
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			DataContentType: `"application/" + data.format`, // CEL expression
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	if ce["datacontenttype"] != "application/xml" {
		t.Errorf("expected datacontenttype 'application/xml', got %v", ce["datacontenttype"])
	}
}

func TestPipeline_CloudEventsDataSchema_Static(t *testing.T) {
	// Test static dataschema
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"data":"test"}`),
				Topic: "events",
			},
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			DataSchema: "https://example.com/schemas/v1/event.json",
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	if ce["dataschema"] != "https://example.com/schemas/v1/event.json" {
		t.Errorf("expected dataschema 'https://example.com/schemas/v1/event.json', got %v", ce["dataschema"])
	}
}

func TestPipeline_CloudEventsDataSchema_CEL(t *testing.T) {
	// Test CEL expression for dataschema
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"schemaVersion":"v2","eventType":"order"}`),
				Topic: "events",
			},
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			DataSchema: `"https://example.com/schemas/" + data.schemaVersion + "/" + data.eventType + ".json"`,
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	if ce["dataschema"] != "https://example.com/schemas/v2/order.json" {
		t.Errorf("expected dataschema 'https://example.com/schemas/v2/order.json', got %v", ce["dataschema"])
	}
}

func TestPipeline_CloudEventsFullSpec_AllFields(t *testing.T) {
	// Test all CloudEvents fields together
	src := &mockSource{
		events: []source.Event{
			{
				Key:   []byte("k1"),
				Value: []byte(`{"eventId":"evt-999","region":"us-west","eventType":"payment","amount":100,"payload":{"txn":"abc"}}`),
				Topic: "events",
			},
		},
	}

	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "payment-processor",
		CloudEvents: &CloudEventsOverrides{
			ID:              "data.eventId",
			Type:            "data.eventType",
			Source:          `"service-" + data.region`,
			Subject:         "data.eventType",
			Data:            "data.payload",
			DataContentType: `"application/json"`,
			DataSchema:      `"https://example.com/schemas/" + data.eventType + ".json"`,
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal CloudEvent: %v", err)
	}

	// Verify all fields
	if ce["id"] != "evt-999" {
		t.Errorf("expected id 'evt-999', got %v", ce["id"])
	}
	if ce["type"] != "payment" {
		t.Errorf("expected type 'payment', got %v", ce["type"])
	}
	if ce["source"] != "service-us-west" {
		t.Errorf("expected source 'service-us-west', got %v", ce["source"])
	}
	if ce["subject"] != "payment" {
		t.Errorf("expected subject 'payment', got %v", ce["subject"])
	}
	if ce["datacontenttype"] != "application/json" {
		t.Errorf("expected datacontenttype 'application/json', got %v", ce["datacontenttype"])
	}
	if ce["dataschema"] != "https://example.com/schemas/payment.json" {
		t.Errorf("expected dataschema 'https://example.com/schemas/payment.json', got %v", ce["dataschema"])
	}

	// Verify data contains only the payload
	data, ok := ce["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", ce["data"])
	}
	if data["txn"] != "abc" {
		t.Errorf("expected data.txn 'abc', got %v", data["txn"])
	}
	// Other fields should NOT be in data
	if _, exists := data["eventId"]; exists {
		t.Error("eventId should not be in data field")
	}
	if _, exists := data["region"]; exists {
		t.Error("region should not be in data field")
	}
}

// --- CloudEvent Detection Tests ---

func TestPipeline_CloudEventDetection_PassThrough(t *testing.T) {
	// Input is already a CloudEvent - should pass through without re-wrapping
	ceInput := `{"specversion":"1.0","type":"order.created","source":"external-service","id":"ce-123","data":{"orderId":"456"}}`
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(ceInput), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow"}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should preserve original CE fields
	if ce["specversion"] != "1.0" {
		t.Errorf("expected specversion '1.0', got %v", ce["specversion"])
	}
	if ce["type"] != "order.created" {
		t.Errorf("expected type 'order.created', got %v", ce["type"])
	}
	if ce["source"] != "external-service" {
		t.Errorf("expected source 'external-service', got %v", ce["source"])
	}
	if ce["id"] != "ce-123" {
		t.Errorf("expected id 'ce-123', got %v", ce["id"])
	}

	// Should NOT be double-wrapped (data should contain original payload, not another CE)
	data, ok := ce["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", ce["data"])
	}
	if data["orderId"] != "456" {
		t.Errorf("expected data.orderId '456', got %v", data["orderId"])
	}
	// Ensure no nested specversion (double-wrap indicator)
	if _, exists := data["specversion"]; exists {
		t.Error("data should not contain nested CloudEvent (double-wrapped)")
	}
}

func TestPipeline_CloudEventDetection_WithOverrides(t *testing.T) {
	// Input is already a CloudEvent - apply overrides to merge into it
	ceInput := `{"specversion":"1.0","type":"original.type","source":"original-source","id":"ce-123","data":{"orderId":"456"}}`
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(ceInput), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{
		FlowName: "test-flow",
		CloudEvents: &CloudEventsOverrides{
			Type:    "overridden.type",
			Subject: "data.orderId",
		},
	}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Type should be overridden
	if ce["type"] != "overridden.type" {
		t.Errorf("expected type 'overridden.type', got %v", ce["type"])
	}
	// Subject should be resolved from CE's data field
	if ce["subject"] != "456" {
		t.Errorf("expected subject '456', got %v", ce["subject"])
	}
	// Source should remain from original CE (not overridden)
	if ce["source"] != "original-source" {
		t.Errorf("expected source 'original-source', got %v", ce["source"])
	}
}

func TestPipeline_CloudEventDetection_NonCE_WrapsNormally(t *testing.T) {
	// Regular JSON input should be wrapped in CloudEvent
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"orderId":"123"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow", EventType: "order.created"}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should be wrapped in CE
	if ce["specversion"] != "1.0" {
		t.Errorf("expected specversion '1.0', got %v", ce["specversion"])
	}
	if ce["type"] != "order.created" {
		t.Errorf("expected type 'order.created', got %v", ce["type"])
	}
	if ce["source"] != "fiso-flow/test-flow" {
		t.Errorf("expected source 'fiso-flow/test-flow', got %v", ce["source"])
	}

	// Original data should be in the data field
	data, ok := ce["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", ce["data"])
	}
	if data["orderId"] != "123" {
		t.Errorf("expected data.orderId '123', got %v", data["orderId"])
	}
}

func TestPipeline_CloudEventDetection_PartialCE_WrapsNormally(t *testing.T) {
	// Partial CE (missing required fields) should be treated as regular JSON
	partialCE := `{"specversion":"1.0","data":{"orderId":"123"}}`
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(partialCE), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow", EventType: "wrapped.event"}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatal("expected 1 delivered event")
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should be wrapped (because original was missing type and source)
	if ce["type"] != "wrapped.event" {
		t.Errorf("expected type 'wrapped.event', got %v", ce["type"])
	}
	if ce["source"] != "fiso-flow/test-flow" {
		t.Errorf("expected source 'fiso-flow/test-flow', got %v", ce["source"])
	}
}

func TestIsCloudEvent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid CloudEvent",
			input:    `{"specversion":"1.0","type":"test.event","source":"test-source","id":"123","data":{}}`,
			expected: true,
		},
		{
			name:     "missing specversion",
			input:    `{"type":"test.event","source":"test-source","id":"123"}`,
			expected: false,
		},
		{
			name:     "missing type",
			input:    `{"specversion":"1.0","source":"test-source","id":"123"}`,
			expected: false,
		},
		{
			name:     "missing source",
			input:    `{"specversion":"1.0","type":"test.event","id":"123"}`,
			expected: false,
		},
		{
			name:     "regular JSON",
			input:    `{"orderId":"123","name":"test"}`,
			expected: false,
		},
		{
			name:     "invalid JSON",
			input:    `not valid json`,
			expected: false,
		},
		{
			name:     "empty object",
			input:    `{}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCloudEvent([]byte(tt.input))
			if result != tt.expected {
				t.Errorf("isCloudEvent(%s) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// closingInterceptor is a mock interceptor that can fail on close
type closingInterceptor struct {
	closeErr error
}

func (c *closingInterceptor) Process(_ context.Context, req *interceptor.Request) (*interceptor.Request, error) {
	return req, nil
}

func (c *closingInterceptor) Close() error {
	return c.closeErr
}

// --- Interceptor Tests ---

type pipelineInterceptor struct {
	modifyPayload []byte
	addHeader     string
	addValue      string
	err           error
	closed        bool
}

func (m *pipelineInterceptor) Process(_ context.Context, req *interceptor.Request) (*interceptor.Request, error) {
	if m.err != nil {
		return nil, m.err
	}
	result := &interceptor.Request{
		Payload:   req.Payload,
		Headers:   make(map[string]string),
		Direction: req.Direction,
	}
	for k, v := range req.Headers {
		result.Headers[k] = v
	}
	if m.modifyPayload != nil {
		result.Payload = m.modifyPayload
	}
	if m.addHeader != "" {
		result.Headers[m.addHeader] = m.addValue
	}
	return result, nil
}

func (m *pipelineInterceptor) Close() error {
	m.closed = true
	return nil
}

func TestPipeline_WithInterceptor(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"original"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	ic := &pipelineInterceptor{modifyPayload: []byte(`{"data":"intercepted"}`)}
	chain := interceptor.NewChain(ic)

	p := New(Config{FlowName: "test-flow"}, src, nil, sk, dlqHandler, chain)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatalf("expected 1 delivered event, got %d", sk.count())
	}

	var ce map[string]interface{}
	if err := json.Unmarshal(sk.received[0].event, &ce); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	dataBytes, _ := json.Marshal(ce["data"])
	if !strings.Contains(string(dataBytes), "intercepted") {
		t.Errorf("expected intercepted data, got %s", string(dataBytes))
	}
}

func TestPipeline_InterceptorError_SendsToDLQ(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"ok"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	ic := &pipelineInterceptor{err: fmt.Errorf("interceptor rejected")}
	chain := interceptor.NewChain(ic)

	p := New(Config{FlowName: "test-flow"}, src, nil, sk, dlqHandler, chain)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 0 {
		t.Fatalf("expected 0 delivered events, got %d", sk.count())
	}
	if pub.count() != 1 {
		t.Fatalf("expected 1 DLQ event, got %d", pub.count())
	}
	if pub.published[0].headers["fiso-error-code"] != "INTERCEPTOR_FAILED" {
		t.Errorf("expected INTERCEPTOR_FAILED, got %s", pub.published[0].headers["fiso-error-code"])
	}
}

func TestPipeline_NilInterceptors_Passthrough(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"ok"}`), Topic: "orders"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "test-flow"}, src, nil, sk, dlqHandler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	if sk.count() != 1 {
		t.Fatalf("expected 1 delivered event, got %d", sk.count())
	}
}

func TestPipeline_Shutdown_WithInterceptorCloseError(t *testing.T) {
	src := &mockSource{}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	ic := &pipelineInterceptor{}
	ic.closed = false
	chain := interceptor.NewChain(ic)

	p := New(Config{FlowName: "test-flow"}, src, nil, sk, dlqHandler, chain)

	err := p.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ic.closed {
		t.Error("expected interceptor to be closed")
	}
}

func TestPipeline_InterceptorError_PropagateErrors(t *testing.T) {
	src := &mockSource{
		events: []source.Event{
			{Key: []byte("k1"), Value: []byte(`{"data":"ok"}`), Topic: "http"},
		},
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	ic := &pipelineInterceptor{err: fmt.Errorf("interceptor failed")}
	chain := interceptor.NewChain(ic)

	p := New(Config{FlowName: "http-flow", PropagateErrors: true}, src, nil, sk, dlqHandler, chain)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := p.Run(ctx)
	if err == nil {
		t.Fatal("expected error from Run when PropagateErrors is true")
	}
	if !strings.Contains(err.Error(), "interceptor failed") {
		t.Errorf("expected error to contain 'interceptor failed', got %s", err.Error())
	}
	if pub.count() != 1 {
		t.Fatalf("expected 1 DLQ event, got %d", pub.count())
	}
}
