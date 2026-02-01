package kafka

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lsm/fiso/internal/source"
	"github.com/twmb/franz-go/pkg/kgo"
)

// mockConsumer implements the consumer interface for testing.
type mockConsumer struct {
	fetches       kgo.Fetches
	commitErr     error
	closed        bool
	committed     []*kgo.Record
	pollCallCount atomic.Int32
	mu            sync.Mutex
}

func (m *mockConsumer) PollFetches(ctx context.Context) kgo.Fetches {
	m.pollCallCount.Add(1)
	if ctx.Err() != nil {
		return nil
	}
	return m.fetches
}

func (m *mockConsumer) MarkCommitRecords(rs ...*kgo.Record) {
	m.mu.Lock()
	m.committed = append(m.committed, rs...)
	m.mu.Unlock()
}

func (m *mockConsumer) CommitMarkedOffsets(_ context.Context) error {
	return m.commitErr
}

func (m *mockConsumer) Close() {
	m.closed = true
}

func TestNewSource_MissingBrokers(t *testing.T) {
	_, err := NewSource(Config{
		Topic:         "test",
		ConsumerGroup: "test-group",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing brokers")
	}
}

func TestNewSource_MissingTopic(t *testing.T) {
	_, err := NewSource(Config{
		Brokers:       []string{"localhost:9092"},
		ConsumerGroup: "test-group",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing topic")
	}
}

func TestNewSource_MissingConsumerGroup(t *testing.T) {
	_, err := NewSource(Config{
		Brokers: []string{"localhost:9092"},
		Topic:   "test",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing consumer group")
	}
}

func TestNewSource_ValidConfig(t *testing.T) {
	s, err := NewSource(Config{
		Brokers:       []string{"localhost:9092"},
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
		StartOffset:   "earliest",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.topic != "test-topic" {
		t.Errorf("expected topic test-topic, got %s", s.topic)
	}
}

func TestNewSource_DefaultOffset(t *testing.T) {
	s, err := NewSource(Config{
		Brokers:       []string{"localhost:9092"},
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = s.Close() }()
}

func TestSource_Close(t *testing.T) {
	s, err := NewSource(Config{
		Brokers:       []string{"localhost:9092"},
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close error: %v", err)
	}
}

func TestSource_StartCancelledContext(t *testing.T) {
	s, err := NewSource(Config{
		Brokers:       []string{"localhost:59092"}, // non-existent broker
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err = s.Start(ctx, func(_ context.Context, _ source.Event) error {
		return nil
	})
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSource_Start_ProcessesRecords(t *testing.T) {
	mc := &mockConsumer{
		fetches: kgo.Fetches{{
			Topics: []kgo.FetchTopic{{
				Topic: "test-topic",
				Partitions: []kgo.FetchPartition{{
					Partition: 0,
					Records: []*kgo.Record{
						{
							Key:   []byte("key1"),
							Value: []byte(`{"event":"test"}`),
							Headers: []kgo.RecordHeader{
								{Key: "ce-type", Value: []byte("test.event")},
							},
							Offset: 42,
							Topic:  "test-topic",
						},
					},
				}},
			}},
		}},
	}

	s := &Source{client: mc, topic: "test-topic", logger: slog.Default()}

	ctx, cancel := context.WithCancel(context.Background())
	var received []source.Event
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		_ = s.Start(ctx, func(_ context.Context, evt source.Event) error {
			mu.Lock()
			received = append(received, evt)
			mu.Unlock()
			cancel()
			return nil
		})
		close(done)
	}()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 {
		t.Fatal("expected at least one event")
	}
	if string(received[0].Key) != "key1" {
		t.Errorf("expected key 'key1', got %q", string(received[0].Key))
	}
	if string(received[0].Value) != `{"event":"test"}` {
		t.Errorf("unexpected value: %s", string(received[0].Value))
	}
	if received[0].Headers["ce-type"] != "test.event" {
		t.Errorf("expected header ce-type=test.event, got %v", received[0].Headers)
	}
	if received[0].Offset != 42 {
		t.Errorf("expected offset 42, got %d", received[0].Offset)
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.committed) == 0 {
		t.Error("expected record to be committed")
	}
}

func TestSource_Start_HandlerError(t *testing.T) {
	mc := &mockConsumer{
		fetches: kgo.Fetches{{
			Topics: []kgo.FetchTopic{{
				Topic: "test-topic",
				Partitions: []kgo.FetchPartition{{
					Partition: 0,
					Records: []*kgo.Record{
						{
							Key:    []byte("key1"),
							Value:  []byte(`{"event":"test"}`),
							Offset: 1,
							Topic:  "test-topic",
						},
					},
				}},
			}},
		}},
	}

	s := &Source{client: mc, topic: "test-topic", logger: slog.Default()}

	ctx, cancel := context.WithCancel(context.Background())
	var handlerCalled atomic.Bool
	done := make(chan struct{})
	go func() {
		_ = s.Start(ctx, func(_ context.Context, _ source.Event) error {
			handlerCalled.Store(true)
			cancel()
			return errors.New("handler failed")
		})
		close(done)
	}()
	<-done

	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.committed) > 0 {
		t.Error("expected no commits after handler error")
	}
	if !handlerCalled.Load() {
		t.Error("expected handler to be called")
	}
}

func TestSource_Start_CommitError(t *testing.T) {
	mc := &mockConsumer{
		fetches: kgo.Fetches{{
			Topics: []kgo.FetchTopic{{
				Topic: "test-topic",
				Partitions: []kgo.FetchPartition{{
					Partition: 0,
					Records: []*kgo.Record{
						{
							Key:   []byte("key1"),
							Value: []byte(`{}`),
							Topic: "test-topic",
						},
					},
				}},
			}},
		}},
		commitErr: errors.New("commit failed"),
	}

	s := &Source{client: mc, topic: "test-topic", logger: slog.Default()}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = s.Start(ctx, func(_ context.Context, _ source.Event) error {
			cancel()
			return nil
		})
		close(done)
	}()
	<-done

	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.committed) == 0 {
		t.Error("expected commit attempt")
	}
}

func TestSource_Start_FetchErrors(t *testing.T) {
	mc := &mockConsumer{
		fetches: kgo.Fetches{{
			Topics: []kgo.FetchTopic{{
				Topic: "test-topic",
				Partitions: []kgo.FetchPartition{{
					Partition: 0,
					Err:       errors.New("partition error"),
				}},
			}},
		}},
	}

	s := &Source{client: mc, topic: "test-topic", logger: slog.Default()}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for mc.pollCallCount.Load() < 2 {
			// spin until at least 2 polls
		}
		cancel()
	}()
	err := s.Start(ctx, func(_ context.Context, _ source.Event) error {
		t.Error("handler should not be called on fetch errors")
		return nil
	})
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSource_Close_Mock(t *testing.T) {
	mc := &mockConsumer{}
	s := &Source{client: mc, topic: "test", logger: slog.Default()}
	if err := s.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mc.closed {
		t.Error("expected client to be closed")
	}
}

func TestSource_Start_MultipleRecords(t *testing.T) {
	mc := &mockConsumer{
		fetches: kgo.Fetches{{
			Topics: []kgo.FetchTopic{{
				Topic: "test-topic",
				Partitions: []kgo.FetchPartition{{
					Partition: 0,
					Records: []*kgo.Record{
						{Key: []byte("k1"), Value: []byte("v1"), Topic: "test-topic", Offset: 0},
						{Key: []byte("k2"), Value: []byte("v2"), Topic: "test-topic", Offset: 1},
						{Key: []byte("k3"), Value: []byte("v3"), Topic: "test-topic", Offset: 2},
					},
				}},
			}},
		}},
	}

	s := &Source{client: mc, topic: "test-topic", logger: slog.Default()}

	ctx, cancel := context.WithCancel(context.Background())
	var count atomic.Int32
	done := make(chan struct{})
	go func() {
		_ = s.Start(ctx, func(_ context.Context, _ source.Event) error {
			if count.Add(1) >= 3 {
				cancel()
			}
			return nil
		})
		close(done)
	}()
	<-done

	if count.Load() < 3 {
		t.Errorf("expected 3 events processed, got %d", count.Load())
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.committed) < 3 {
		t.Errorf("expected 3 commits, got %d", len(mc.committed))
	}
}
