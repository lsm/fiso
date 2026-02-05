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

func TestPipeline_CloudEventsOverrides_DynamicJSONPath(t *testing.T) {
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
			Type:    "$.event_type",
			Subject: "$.order_id",
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
			Type:    "$.nonexistent",
			Subject: "$.also_missing",
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
	// ResolveString falls back to the raw expression on error
	if ce["type"] != "$.nonexistent" {
		t.Errorf("expected type '$.nonexistent' (fallback), got %v", ce["type"])
	}
	if ce["subject"] != "$.also_missing" {
		t.Errorf("expected subject '$.also_missing' (fallback), got %v", ce["subject"])
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
			Subject: "$.order_id",
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
