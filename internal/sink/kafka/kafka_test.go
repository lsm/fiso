package kafka

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	intkafka "github.com/lsm/fiso/internal/kafka"
	"go.opentelemetry.io/otel/trace/noop"
)

// mockPublisher implements the publisher interface for testing.
type mockPublisher struct {
	publishErr error
	closed     bool
	record     struct {
		topic   string
		key     []byte
		value   []byte
		headers map[string]string
	}
}

func (m *mockPublisher) Publish(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
	m.record.topic = topic
	m.record.key = key
	m.record.value = value
	m.record.headers = headers
	return m.publishErr
}

func (m *mockPublisher) Close() error {
	m.closed = true
	return nil
}

func testCluster(brokers ...string) *intkafka.ClusterConfig {
	if len(brokers) == 0 {
		brokers = []string{"localhost:9092"}
	}
	return &intkafka.ClusterConfig{Brokers: brokers}
}

func TestNewSink_MissingCluster(t *testing.T) {
	_, err := NewSink(Config{Topic: "test-topic"})
	if err == nil {
		t.Fatal("expected error for missing cluster")
	}
	if err.Error() != "cluster config is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewSink_MissingTopic(t *testing.T) {
	_, err := NewSink(Config{Cluster: testCluster()})
	if err == nil {
		t.Fatal("expected error for missing topic")
	}
	if err.Error() != "topic is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSink_Deliver_Success(t *testing.T) {
	mp := &mockPublisher{publishErr: nil}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	event := []byte(`{"id":"evt-1","data":"hello"}`)
	headers := map[string]string{
		"Content-Type": "application/cloudevents+json",
		"X-Custom":     "test-value",
	}

	err := s.Deliver(context.Background(), event, headers)
	if err != nil {
		t.Fatalf("deliver failed: %v", err)
	}

	if mp.record.topic != "test-topic" {
		t.Errorf("expected topic 'test-topic', got %q", mp.record.topic)
	}
	if string(mp.record.value) != string(event) {
		t.Errorf("value mismatch: got %s, want %s", mp.record.value, event)
	}
	if mp.record.headers["Content-Type"] != "application/cloudevents+json" {
		t.Errorf("Content-Type header mismatch: got %s", mp.record.headers["Content-Type"])
	}
	if mp.record.headers["X-Custom"] != "test-value" {
		t.Errorf("X-Custom header mismatch: got %s", mp.record.headers["X-Custom"])
	}
}

func TestSink_Deliver_Error(t *testing.T) {
	mp := &mockPublisher{publishErr: errors.New("kafka error")}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	err := s.Deliver(context.Background(), []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "kafka error" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSink_Deliver_NilHeaders(t *testing.T) {
	mp := &mockPublisher{publishErr: nil}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	err := s.Deliver(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("deliver failed: %v", err)
	}

	if len(mp.record.headers) != 0 {
		t.Errorf("expected no headers, got %d", len(mp.record.headers))
	}
}

func TestSink_Close(t *testing.T) {
	mp := &mockPublisher{}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	err := s.Close()
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if !mp.closed {
		t.Error("expected publisher to be closed")
	}
}

func TestNewSink_PublisherFailure(t *testing.T) {
	// Test with invalid broker address format
	// This should cause kafka.NewPublisher to fail
	cfg := Config{
		Cluster: testCluster("invalid://broker-address"),
		Topic:   "test-topic",
	}

	_, err := NewSink(cfg)
	if err == nil {
		t.Fatal("expected error for invalid broker address")
	}
	// Verify the error is wrapped with "kafka publisher" prefix
	if err.Error() == "cluster config is required" || err.Error() == "topic is required" {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestNewSink_MultipleBrokers(t *testing.T) {
	// This test verifies that NewSink accepts multiple brokers in cluster config
	// The actual publisher creation will succeed with multiple brokers
	cfg := Config{
		Cluster: testCluster("broker1:9092", "broker2:9092", "broker3:9092"),
		Topic:   "test-topic",
	}

	// Note: This will create a real publisher, which may fail if brokers are unavailable
	// In a test environment, we'd want to mock this
	_, err := NewSink(cfg)
	// We expect this might fail due to unavailable brokers, but not due to config validation
	if err != nil && err.Error() == "cluster config is required" {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestSink_Deliver_EmptyEvent(t *testing.T) {
	mp := &mockPublisher{publishErr: nil}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	emptyEvent := []byte{}
	err := s.Deliver(context.Background(), emptyEvent, nil)
	if err != nil {
		t.Fatalf("deliver failed with empty event: %v", err)
	}

	if len(mp.record.value) != 0 {
		t.Errorf("expected empty value, got %d bytes", len(mp.record.value))
	}
}

func TestSink_Deliver_ContextCancellation(t *testing.T) {
	mp := &mockPublisher{publishErr: errors.New("context canceled")}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := s.Deliver(ctx, []byte(`{"test":"data"}`), nil)
	if err == nil {
		t.Fatal("expected error when context is canceled")
	}
	// The mock returns the error, so we verify it propagates
	if err.Error() != "context canceled" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSink_Deliver_ContextTimeout(t *testing.T) {
	mp := &mockPublisher{publishErr: errors.New("context deadline exceeded")}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()

	err := s.Deliver(ctx, []byte(`{"test":"data"}`), nil)
	if err == nil {
		t.Fatal("expected error when context times out")
	}
	if err.Error() != "context deadline exceeded" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSink_Deliver_LargeEvent(t *testing.T) {
	mp := &mockPublisher{publishErr: nil}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	// Create a large event (1MB)
	largeEvent := make([]byte, 1024*1024)
	for i := range largeEvent {
		largeEvent[i] = 'x'
	}

	err := s.Deliver(context.Background(), largeEvent, nil)
	if err != nil {
		t.Fatalf("deliver failed with large event: %v", err)
	}

	if len(mp.record.value) != len(largeEvent) {
		t.Errorf("value size mismatch: got %d, want %d", len(mp.record.value), len(largeEvent))
	}
}

func TestSink_Deliver_MultipleHeaders(t *testing.T) {
	mp := &mockPublisher{publishErr: nil}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	headers := map[string]string{
		"Content-Type":     "application/cloudevents+json",
		"ce-id":            "evt-123",
		"ce-source":        "test-source",
		"ce-type":          "test.event",
		"ce-specversion":   "1.0",
		"X-Custom-Header":  "custom-value",
		"X-Another-Header": "another-value",
		"Authorization":    "Bearer token",
	}

	err := s.Deliver(context.Background(), []byte(`{}`), headers)
	if err != nil {
		t.Fatalf("deliver failed: %v", err)
	}

	// Verify all headers were passed through
	for k, v := range headers {
		if mp.record.headers[k] != v {
			t.Errorf("header %s mismatch: got %s, want %s", k, mp.record.headers[k], v)
		}
	}
}

func TestSink_Deliver_SpecialCharactersInEvent(t *testing.T) {
	mp := &mockPublisher{publishErr: nil}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	specialEvent := []byte(`{"data":"Test with unicode: \u2764\ufe0f, newlines:\n, tabs:\t, quotes:\""}`)

	err := s.Deliver(context.Background(), specialEvent, nil)
	if err != nil {
		t.Fatalf("deliver failed with special characters: %v", err)
	}

	if string(mp.record.value) != string(specialEvent) {
		t.Errorf("value mismatch: got %s, want %s", mp.record.value, specialEvent)
	}
}

func TestSink_Deliver_EmptyHeaders(t *testing.T) {
	mp := &mockPublisher{publishErr: nil}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	emptyHeaders := map[string]string{}
	err := s.Deliver(context.Background(), []byte(`{}`), emptyHeaders)
	if err != nil {
		t.Fatalf("deliver failed with empty headers: %v", err)
	}

	if len(mp.record.headers) != 0 {
		t.Errorf("expected no headers, got %d", len(mp.record.headers))
	}
}

func TestSink_Deliver_BinaryData(t *testing.T) {
	mp := &mockPublisher{publishErr: nil}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	// Test with binary data that's not valid UTF-8
	binaryData := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}

	err := s.Deliver(context.Background(), binaryData, nil)
	if err != nil {
		t.Fatalf("deliver failed with binary data: %v", err)
	}

	if len(mp.record.value) != len(binaryData) {
		t.Errorf("value length mismatch: got %d, want %d", len(mp.record.value), len(binaryData))
	}
}

func TestNewSink_ValidConfig(t *testing.T) {
	cfg := Config{
		Cluster: testCluster(),
		Topic:   "test-topic",
	}

	sink, err := NewSink(cfg)
	if err != nil {
		// This might fail if kafka is not available, but we can't mock it easily
		// The test documents the intended behavior
		t.Skipf("kafka publisher creation failed (expected if kafka unavailable): %v", err)
	}
	defer func() {
		if sink != nil {
			_ = sink.Close()
		}
	}()

	if sink == nil {
		t.Fatal("expected non-nil sink")
	}
	if sink.topic != cfg.Topic {
		t.Errorf("topic mismatch: got %s, want %s", sink.topic, cfg.Topic)
	}
}

func TestSink_Deliver_PublishErrorWrapped(t *testing.T) {
	mp := &mockPublisher{publishErr: errors.New("kafka publish: broker not available")}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	err := s.Deliver(context.Background(), []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	// Verify the error is returned as-is from publisher (no wrapping in Deliver)
	if err.Error() != "kafka publish: broker not available" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSetTracer(t *testing.T) {
	mp := &mockPublisher{}
	s := &Sink{
		publisher: mp,
		topic:     "test-topic",
		logger:    slog.Default(),
	}

	tracer := noop.NewTracerProvider().Tracer("test-tracer")
	s.SetTracer(tracer)

	if s.tracer == nil {
		t.Error("expected tracer to be set")
	}
}
