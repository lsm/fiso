package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lsm/fiso/internal/delivery"
	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/interceptor"
	"github.com/lsm/fiso/internal/source"
)

// mockInterceptor adapts a Process function to the interceptor interface.
type mockInterceptor struct {
	process func(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error)
}

func (m *mockInterceptor) Process(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
	return m.process(ctx, req)
}

func (m *mockInterceptor) Close() error { return nil }

// capturingSource records the handler's returned error for one event.
type capturingSource struct {
	evt    source.Event
	gotErr error
	done   chan struct{}
}

func (c *capturingSource) Start(ctx context.Context, handler func(context.Context, source.Event) error) error {
	c.gotErr = handler(ctx, c.evt)
	close(c.done)
	<-ctx.Done()
	return ctx.Err()
}

func (c *capturingSource) Close() error { return nil }

// TestPipeline_InterceptorRejection_NoDLQ_Propagates pins the rejection
// contract for request-response sources: an interceptor refusal is terminal —
// no DLQ publication, no sink delivery — and the typed error reaches the
// source so it can answer with the guest-chosen status (ADR 0007).
func TestPipeline_InterceptorRejection_NoDLQ_Propagates(t *testing.T) {
	rejecting := &mockInterceptor{
		process: func(_ context.Context, _ *interceptor.Request) (*interceptor.Request, error) {
			return nil, &interceptor.RejectedError{Status: 401, Reason: "missing credentials"}
		},
	}
	src := &capturingSource{
		evt:  source.Event{Key: []byte("k1"), Value: []byte(`{"data":"secret"}}`), Topic: "http"},
		done: make(chan struct{}),
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "auth-flow", SourceType: "http"}, src, nil, sk, dlqHandler, interceptor.NewChain(rejecting))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go func() { _ = p.Run(ctx) }()
	<-src.done
	cancel()

	if pub.count() != 0 {
		t.Fatalf("a rejected event must not reach the DLQ, got %d publications", pub.count())
	}
	if sk.count() != 0 {
		t.Fatalf("a rejected event must not be delivered, got %d deliveries", sk.count())
	}
	rej, ok := interceptor.AsRejection(src.gotErr)
	if !ok {
		t.Fatalf("the source must receive the typed rejection, got %v", src.gotErr)
	}
	if rej.Status != 401 || rej.Reason != "missing credentials" {
		t.Fatalf("rejection = %+v, want status 401 with the guest reason", rej)
	}
}

// TestPipeline_InterceptorRejection_Kafka_Acks pins the kafka semantics: a
// rejection is terminally disposed of — logged and acknowledged — so a
// refused message is neither reprocessed forever nor dead-lettered.
func TestPipeline_InterceptorRejection_Kafka_Acks(t *testing.T) {
	rejecting := &mockInterceptor{
		process: func(_ context.Context, _ *interceptor.Request) (*interceptor.Request, error) {
			return nil, &interceptor.RejectedError{Status: 403, Reason: "forbidden"}
		},
	}
	src := &capturingSource{
		evt:  source.Event{Key: []byte("k1"), Value: []byte(`{}`), Topic: "events"},
		done: make(chan struct{}),
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(
		Config{FlowName: "kafka-auth-flow", SourceType: "kafka", CommitPolicy: delivery.CommitPolicySinkOrDLQ},
		src, nil, sk, dlqHandler, interceptor.NewChain(rejecting),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go func() { _ = p.Run(ctx) }()
	<-src.done
	cancel()

	if src.gotErr != nil {
		t.Fatalf("a kafka rejection must be acknowledged (nil handler error), got %v", src.gotErr)
	}
	if pub.count() != 0 {
		t.Fatalf("a rejected kafka message must not reach the DLQ, got %d publications", pub.count())
	}
	if sk.count() != 0 {
		t.Fatalf("a rejected kafka message must not be delivered, got %d deliveries", sk.count())
	}
}

// TestPipeline_InterceptorFailure_StillDLQs guards the ordinary failure path
// against the rejection branch: a non-rejection interceptor error keeps the
// existing DLQ semantics for request-response sources.
func TestPipeline_InterceptorFailure_StillDLQs(t *testing.T) {
	failing := &mockInterceptor{
		process: func(_ context.Context, _ *interceptor.Request) (*interceptor.Request, error) {
			return nil, errors.New("module crashed")
		},
	}
	src := &capturingSource{
		evt:  source.Event{Key: []byte("k1"), Value: []byte(`{}`), Topic: "http"},
		done: make(chan struct{}),
	}
	sk := &mockSink{}
	pub := &mockPublisher{}
	dlqHandler := dlq.NewHandler(pub)

	p := New(Config{FlowName: "fail-flow", SourceType: "http"}, src, nil, sk, dlqHandler, interceptor.NewChain(failing))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go func() { _ = p.Run(ctx) }()
	<-src.done
	cancel()

	if pub.count() != 1 {
		t.Fatalf("an interceptor failure must still reach the DLQ, got %d publications", pub.count())
	}
	if src.gotErr == nil {
		t.Fatal("an interceptor failure must still propagate to the source")
	}
	if _, isRej := interceptor.AsRejection(src.gotErr); isRej {
		t.Fatal("an interceptor failure must not be classified as a rejection")
	}
}
