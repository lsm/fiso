package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lsm/fiso/internal/delivery"
	intkafka "github.com/lsm/fiso/internal/kafka"
	"github.com/lsm/fiso/internal/source"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/trace/noop"
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

type txEndResult struct {
	committed bool
	err       error
}

type mockTxSession struct {
	fetches      []kgo.Fetches
	pollCount    atomic.Int32
	beginErr     error
	endResults   []txEndResult
	endCallCount int
	endCount     atomic.Int32
	endCalls     []kgo.TransactionEndTry
	produced     []*kgo.Record
	closed       bool
}

func (m *mockTxSession) PollFetches(ctx context.Context) kgo.Fetches {
	m.pollCount.Add(1)
	if ctx.Err() != nil {
		return nil
	}
	if len(m.fetches) == 0 {
		return kgo.Fetches{}
	}
	idx := int(m.pollCount.Load()) - 1
	if idx < len(m.fetches) {
		return m.fetches[idx]
	}
	return m.fetches[len(m.fetches)-1]
}

func (m *mockTxSession) Begin() error {
	return m.beginErr
}

func (m *mockTxSession) End(_ context.Context, commit kgo.TransactionEndTry) (bool, error) {
	m.endCalls = append(m.endCalls, commit)
	m.endCount.Add(1)
	idx := m.endCallCount
	m.endCallCount++
	if idx < len(m.endResults) {
		return m.endResults[idx].committed, m.endResults[idx].err
	}
	if commit == kgo.TryCommit {
		return true, nil
	}
	return false, nil
}

func (m *mockTxSession) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	m.produced = append(m.produced, rs...)
	results := make(kgo.ProduceResults, len(rs))
	for i, r := range rs {
		results[i] = kgo.ProduceResult{Record: r}
	}
	return results
}

func (m *mockTxSession) Close() {
	m.closed = true
}

func testCluster(brokers ...string) *intkafka.ClusterConfig {
	if len(brokers) == 0 {
		brokers = []string{"localhost:9092"}
	}
	return &intkafka.ClusterConfig{Brokers: brokers}
}

func txFetch(topic string, partition int32, records []*kgo.Record, fetchErr error) kgo.Fetches {
	return kgo.Fetches{{
		Topics: []kgo.FetchTopic{{
			Topic: topic,
			Partitions: []kgo.FetchPartition{{
				Partition: partition,
				Records:   records,
				Err:       fetchErr,
			}},
		}},
	}}
}

func TestNewSource_MissingCluster(t *testing.T) {
	_, err := NewSource(Config{
		Topic:         "test",
		ConsumerGroup: "test-group",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing cluster")
	}
}

func TestNewSource_MissingTopic(t *testing.T) {
	_, err := NewSource(Config{
		Cluster:       testCluster(),
		ConsumerGroup: "test-group",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing topic")
	}
}

func TestNewSource_MissingConsumerGroup(t *testing.T) {
	_, err := NewSource(Config{
		Cluster: testCluster(),
		Topic:   "test",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing consumer group")
	}
}

func TestNewSource_ValidConfig(t *testing.T) {
	s, err := NewSource(Config{
		Cluster:       testCluster(),
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
		Cluster:       testCluster(),
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = s.Close() }()
}

func TestNewSource_NumericStartOffset(t *testing.T) {
	s, err := NewSource(Config{
		Cluster:       testCluster(),
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
		StartOffset:   231,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = s.Close() }()
}

func TestNewSource_InvalidStartOffset(t *testing.T) {
	_, err := NewSource(Config{
		Cluster:       testCluster(),
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
		StartOffset:   "middle",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid startOffset")
	}
	if !strings.Contains(err.Error(), "invalid startOffset") {
		t.Fatalf("expected invalid startOffset error, got: %v", err)
	}
}

func TestResolveStartOffset(t *testing.T) {
	tests := []struct {
		name        string
		in          any
		want        int64
		wantErrText string
	}{
		{name: "nil defaults to latest", in: nil, want: -1},
		{name: "empty string defaults to latest", in: "", want: -1},
		{name: "latest keyword", in: "latest", want: -1},
		{name: "earliest keyword", in: "earliest", want: -2},
		{name: "numeric int", in: 231, want: 231},
		{name: "numeric string", in: "231", want: 231},
		{name: "negative numeric string", in: "-1", wantErrText: "must be >= 0"},
		{name: "numeric float64 integer", in: float64(231), want: 231},
		{name: "numeric float32 integer", in: float32(22), want: 22},
		{name: "invalid text", in: "middle", wantErrText: "must be \"earliest\""},
		{name: "negative int", in: -1, wantErrText: "must be >= 0"},
		{name: "fractional float", in: 2.5, wantErrText: "must be an integer"},
		{name: "invalid type", in: true, wantErrText: "got bool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveStartOffset(tt.in)
			if tt.wantErrText != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErrText)
				}
				if !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErrText, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.EpochOffset().Offset != tt.want {
				t.Fatalf("expected offset %d, got %s", tt.want, got.String())
			}
		})
	}
}

func TestParseNumericStartOffset_Branches(t *testing.T) {
	tests := []struct {
		name    string
		in      any
		want    int64
		ok      bool
		errText string
	}{
		{name: "int8", in: int8(5), want: 5, ok: true},
		{name: "int8 negative", in: int8(-5), ok: true, errText: "must be >= 0"},
		{name: "int16", in: int16(6), want: 6, ok: true},
		{name: "int16 negative", in: int16(-6), ok: true, errText: "must be >= 0"},
		{name: "int32", in: int32(7), want: 7, ok: true},
		{name: "int32 negative", in: int32(-7), ok: true, errText: "must be >= 0"},
		{name: "int64", in: int64(8), want: 8, ok: true},
		{name: "int64 negative", in: int64(-8), ok: true, errText: "must be >= 0"},
		{name: "uint", in: uint(9), want: 9, ok: true},
		{name: "uint8", in: uint8(10), want: 10, ok: true},
		{name: "uint16", in: uint16(11), want: 11, ok: true},
		{name: "uint32", in: uint32(12), want: 12, ok: true},
		{name: "uint64", in: uint64(14), want: 14, ok: true},
		{name: "float32 integer", in: float32(13), want: 13, ok: true},
		{name: "float32 fractional", in: float32(13.5), ok: true, errText: "must be an integer"},
		{name: "float32 negative", in: float32(-1), ok: true, errText: "must be >= 0"},
		{name: "float64 fractional", in: float64(2.5), ok: true, errText: "must be an integer"},
		{name: "float64 negative", in: float64(-2), ok: true, errText: "must be >= 0"},
		{name: "float64 overflow", in: float64(math.MaxInt64) * 2, ok: true, errText: "overflows"},
		{name: "uint64 overflow", in: uint64(math.MaxInt64) + 1, ok: true, errText: "overflows"},
		{name: "unsupported type", in: struct{}{}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := parseNumericStartOffset(tt.in)
			if ok != tt.ok {
				t.Fatalf("expected ok=%v, got %v", tt.ok, ok)
			}
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("expected error containing %q, got %v", tt.errText, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok && got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestNewSource_TransactionalConfig(t *testing.T) {
	s, err := NewSource(Config{
		Cluster:                  testCluster(),
		Topic:                    "test-topic",
		ConsumerGroup:            "test-group",
		TransactionalID:          "tx-test-1",
		TransactionTimeout:       time.Second,
		RequireStableFetchOffset: true,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.txSession == nil {
		t.Fatal("expected transactional session to be initialized")
	}
	if s.client != nil {
		t.Fatal("expected manual client to be nil when transactional session is enabled")
	}
}

func TestNewSource_ClusterOptionsError(t *testing.T) {
	_, err := NewSource(Config{
		Cluster: &intkafka.ClusterConfig{
			Brokers: []string{"localhost:9092"},
			Auth:    intkafka.AuthConfig{Mechanism: "INVALID"},
		},
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "cluster options") {
		t.Fatalf("expected cluster options error, got %v", err)
	}
}

func TestNewSource_ClientCreationError_NoBrokers(t *testing.T) {
	_, err := NewSource(Config{
		Cluster:       &intkafka.ClusterConfig{Brokers: nil},
		Topic:         "test-topic",
		ConsumerGroup: "test-group",
	}, nil)
	if err == nil {
		t.Fatal("expected client creation error when brokers are missing")
	}
}

func TestNewSource_TransactionalSessionError_NoBrokers(t *testing.T) {
	_, err := NewSource(Config{
		Cluster:         &intkafka.ClusterConfig{Brokers: nil},
		Topic:           "test-topic",
		ConsumerGroup:   "test-group",
		TransactionalID: "tx-test",
	}, nil)
	if err == nil {
		t.Fatal("expected transactional session creation error when brokers are missing")
	}
}

func TestSource_Close(t *testing.T) {
	s, err := NewSource(Config{
		Cluster:       testCluster(),
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
		Cluster:       testCluster("localhost:59092"), // non-existent broker
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

func TestSource_Start_StopOnHandlerError_ReturnsError(t *testing.T) {
	mc := &mockConsumer{
		fetches: kgo.Fetches{{
			Topics: []kgo.FetchTopic{{
				Topic: "test-topic",
				Partitions: []kgo.FetchPartition{{
					Partition: 0,
					Records: []*kgo.Record{{
						Key:    []byte("key1"),
						Value:  []byte(`{"event":"test"}`),
						Offset: 1,
						Topic:  "test-topic",
					}},
				}},
			}},
		}},
	}

	s := &Source{client: mc, topic: "test-topic", stopOnHandlerError: true, logger: slog.Default()}

	err := s.Start(context.Background(), func(_ context.Context, _ source.Event) error {
		return errors.New("handler failed")
	})
	if err == nil {
		t.Fatal("expected handler error to be returned")
	}
	if !strings.Contains(err.Error(), "handler failed") {
		t.Fatalf("unexpected error: %v", err)
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

func TestSource_Start_DrainsAfterCancel(t *testing.T) {
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
			n := count.Add(1)
			// Cancel after first record — remaining records in the batch should still be processed
			if n == 1 {
				cancel()
			}
			return nil
		})
		close(done)
	}()
	<-done

	// All 3 records from the batch should be processed despite cancellation after the first
	if count.Load() != 3 {
		t.Errorf("expected all 3 records to be drained, got %d", count.Load())
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.committed) != 3 {
		t.Errorf("expected 3 commits after drain, got %d", len(mc.committed))
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

func TestSetTracer(t *testing.T) {
	mc := &mockConsumer{}
	s := &Source{client: mc, topic: "test-topic", logger: slog.Default()}

	tracer := noop.NewTracerProvider().Tracer("test-tracer")
	s.SetTracer(tracer)

	if s.tracer == nil {
		t.Error("expected tracer to be set")
	}
}

func TestSource_Start_Transactional_SuccessAndCommit(t *testing.T) {
	rec := &kgo.Record{Key: []byte("k1"), Value: []byte(`{"event":"ok"}`), Offset: 10, Topic: "test-topic", Partition: 0}
	tx := &mockTxSession{fetches: []kgo.Fetches{txFetch("test-topic", 0, []*kgo.Record{rec}, nil)}}

	s := &Source{txSession: tx, topic: "test-topic", stopOnHandlerError: true, logger: slog.Default(), tracer: noop.NewTracerProvider().Tracer("test")}
	ctx, cancel := context.WithCancel(context.Background())
	err := s.Start(ctx, func(hctx context.Context, evt source.Event) error {
		if evt.Offset != 10 {
			t.Fatalf("expected offset 10, got %d", evt.Offset)
		}
		producer, ok := delivery.KafkaTransactionalProducerFromContext(hctx)
		if !ok {
			t.Fatal("expected transactional producer in handler context")
		}
		results := producer.ProduceSync(hctx, &kgo.Record{Topic: "sink-topic", Value: []byte("x")})
		if err := results.FirstErr(); err != nil {
			t.Fatalf("unexpected produce error: %v", err)
		}
		cancel()
		return nil
	})

	if err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if len(tx.endCalls) != 1 || tx.endCalls[0] != kgo.TryCommit {
		t.Fatalf("expected one commit end call, got %+v", tx.endCalls)
	}
	if len(tx.produced) != 1 {
		t.Fatalf("expected one produced record via tx producer, got %d", len(tx.produced))
	}
}

func TestSource_Start_Transactional_BeginError(t *testing.T) {
	rec := &kgo.Record{Key: []byte("k1"), Value: []byte(`{"event":"ok"}`), Topic: "test-topic"}
	tx := &mockTxSession{
		fetches:  []kgo.Fetches{txFetch("test-topic", 0, []*kgo.Record{rec}, nil)},
		beginErr: errors.New("begin failed"),
	}
	s := &Source{txSession: tx, topic: "test-topic", stopOnHandlerError: true, logger: slog.Default()}

	err := s.Start(context.Background(), func(_ context.Context, _ source.Event) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "begin transaction") {
		t.Fatalf("expected begin transaction error, got %v", err)
	}
}

func TestSource_Start_Transactional_HandlerErrorAbort(t *testing.T) {
	rec := &kgo.Record{Key: []byte("k1"), Value: []byte(`{"event":"bad"}`), Topic: "test-topic"}
	tx := &mockTxSession{fetches: []kgo.Fetches{txFetch("test-topic", 0, []*kgo.Record{rec}, nil)}}
	s := &Source{txSession: tx, topic: "test-topic", stopOnHandlerError: true, logger: slog.Default(), tracer: noop.NewTracerProvider().Tracer("test")}

	err := s.Start(context.Background(), func(_ context.Context, _ source.Event) error {
		return errors.New("handler failed")
	})
	if err == nil || !strings.Contains(err.Error(), "handler failed") {
		t.Fatalf("expected handler error, got %v", err)
	}
	if len(tx.endCalls) != 1 || tx.endCalls[0] != kgo.TryAbort {
		t.Fatalf("expected abort end call, got %+v", tx.endCalls)
	}
}

func TestSource_Start_Transactional_AbortError(t *testing.T) {
	rec := &kgo.Record{Key: []byte("k1"), Value: []byte(`{"event":"bad"}`), Topic: "test-topic"}
	tx := &mockTxSession{
		fetches:    []kgo.Fetches{txFetch("test-topic", 0, []*kgo.Record{rec}, nil)},
		endResults: []txEndResult{{committed: false, err: errors.New("abort failed")}},
	}
	s := &Source{txSession: tx, topic: "test-topic", stopOnHandlerError: true, logger: slog.Default()}

	err := s.Start(context.Background(), func(_ context.Context, _ source.Event) error {
		return errors.New("handler failed")
	})
	if err == nil || !strings.Contains(err.Error(), "abort transaction after handler error") {
		t.Fatalf("expected abort error, got %v", err)
	}
}

func TestSource_Start_Transactional_CommitErrorAndAborted(t *testing.T) {
	tests := []struct {
		name       string
		result     txEndResult
		errContain string
	}{
		{name: "commit error", result: txEndResult{err: errors.New("commit failed")}, errContain: "commit transaction"},
		{name: "commit aborted", result: txEndResult{committed: false, err: nil}, errContain: "transaction commit was aborted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &kgo.Record{Key: []byte("k1"), Value: []byte(`{"event":"ok"}`), Topic: "test-topic"}
			tx := &mockTxSession{
				fetches:    []kgo.Fetches{txFetch("test-topic", 0, []*kgo.Record{rec}, nil)},
				endResults: []txEndResult{tt.result},
			}
			s := &Source{txSession: tx, topic: "test-topic", stopOnHandlerError: true, logger: slog.Default(), tracer: noop.NewTracerProvider().Tracer("test")}
			err := s.Start(context.Background(), func(_ context.Context, _ source.Event) error { return nil })
			if err == nil || !strings.Contains(err.Error(), tt.errContain) {
				t.Fatalf("expected error containing %q, got %v", tt.errContain, err)
			}
		})
	}
}

func TestSource_Start_Transactional_FetchErrorsThenCancel(t *testing.T) {
	tx := &mockTxSession{fetches: []kgo.Fetches{txFetch("test-topic", 0, nil, errors.New("fetch failed"))}}
	s := &Source{txSession: tx, topic: "test-topic", logger: slog.Default()}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for tx.pollCount.Load() < 2 {
		}
		cancel()
	}()

	err := s.Start(ctx, func(_ context.Context, _ source.Event) error { return nil })
	if err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestSource_Close_TransactionalSession(t *testing.T) {
	tx := &mockTxSession{}
	s := &Source{txSession: tx, topic: "test-topic", logger: slog.Default()}
	if err := s.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}
	if !tx.closed {
		t.Fatal("expected transactional session to be closed")
	}
}

func TestBuildEvent_GeneratesCorrelationID(t *testing.T) {
	rec := &kgo.Record{Key: []byte("k"), Value: []byte("v"), Topic: "t", Offset: 1, Headers: []kgo.RecordHeader{{Key: "x", Value: []byte("y")}}}
	evt := buildEvent(rec)
	if evt.CorrelationID == "" {
		t.Fatal("expected correlation id to be populated")
	}
	if got := evt.Headers["x"]; got != "y" {
		t.Fatalf("expected header x=y, got %q", got)
	}
}

func TestSource_Start_Transactional_AbortAndContinueWhenNotStrict(t *testing.T) {
	rec := &kgo.Record{Key: []byte("k1"), Value: []byte(`{"event":"bad"}`), Topic: "test-topic"}
	tx := &mockTxSession{fetches: []kgo.Fetches{txFetch("test-topic", 0, []*kgo.Record{rec}, nil)}}
	s := &Source{txSession: tx, topic: "test-topic", stopOnHandlerError: false, logger: slog.Default(), tracer: noop.NewTracerProvider().Tracer("test")}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for tx.endCount.Load() < 1 {
		}
		cancel()
	}()

	err := s.Start(ctx, func(_ context.Context, _ source.Event) error { return fmt.Errorf("boom") })
	if err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if len(tx.endCalls) == 0 || tx.endCalls[0] != kgo.TryAbort {
		t.Fatalf("expected first end call to abort, got %+v", tx.endCalls)
	}
}
