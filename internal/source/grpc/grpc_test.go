package grpc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/lsm/fiso/internal/source"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// rawCodecForClient is the same codec used in the sink package.
type rawCodecForClient struct{}

func (rawCodecForClient) Marshal(v interface{}) ([]byte, error)      { return v.([]byte), nil }
func (rawCodecForClient) Unmarshal(data []byte, v interface{}) error { *v.(*[]byte) = data; return nil }
func (rawCodecForClient) Name() string                               { return "raw" }

func TestSource_ReceivesEvents(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	var mu sync.Mutex
	var received []source.Event

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(ctx context.Context, evt source.Event) error {
			mu.Lock()
			received = append(received, evt)
			mu.Unlock()
			return nil
		})
	}()

	// Wait for server to be ready
	<-src.ready
	actualAddr := src.ListenAddr

	// Connect client
	conn, err := grpc.NewClient(actualAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send event
	event := []byte(`{"id":"test-1"}`)
	md := metadata.New(map[string]string{"x-trace-id": "trace-123"})
	sendCtx := metadata.NewOutgoingContext(context.Background(), md)

	var resp []byte
	err = conn.Invoke(sendCtx, "/fiso.v1.EventService/Deliver", event, &resp, grpc.ForceCodec(rawCodecForClient{}))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}

	cancel()
	<-errCh

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if string(received[0].Value) != `{"id":"test-1"}` {
		t.Errorf("expected event data, got %s", received[0].Value)
	}
	if received[0].Headers["x-trace-id"] != "trace-123" {
		t.Errorf("expected trace header, got %v", received[0].Headers)
	}
	if received[0].Topic != "grpc" {
		t.Errorf("expected topic grpc, got %s", received[0].Topic)
	}
}

func TestNewSource_MissingAddress(t *testing.T) {
	_, err := NewSource(Config{}, nil)
	if err == nil {
		t.Fatal("expected error for missing address")
	}
}

func TestSource_Close(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestSource_CloseAfterStart(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(ctx context.Context, evt source.Event) error {
			return nil
		})
	}()
	<-src.ready

	// Close stops the server
	_ = src.Close()
	cancel()
	<-errCh
}

func TestSource_HandlerError(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(ctx context.Context, evt source.Event) error {
			return errors.New("handler failed")
		})
	}()
	<-src.ready

	conn, err := grpc.NewClient(src.ListenAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	var resp []byte
	err = conn.Invoke(context.Background(), "/test/Method", []byte(`{}`), &resp, grpc.ForceCodec(rawCodecForClient{}))
	if err == nil {
		t.Error("expected error when handler returns error")
	}

	cancel()
	<-errCh
}

func TestSource_StartListenError(t *testing.T) {
	// Use an invalid address to trigger listen failure
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(ctx context.Context, evt source.Event) error {
			return nil
		})
	}()
	<-src.ready

	// Now try to start a second source on the same port - should fail
	src2, _ := NewSource(Config{ListenAddr: src.ListenAddr}, nil)
	err = src2.Start(context.Background(), func(ctx context.Context, evt source.Event) error { return nil })
	if err == nil {
		t.Error("expected error when port already in use")
	}

	cancel()
	<-errCh
}

func TestRawCodec_Marshal(t *testing.T) {
	c := rawCodec{}
	data, err := c.Marshal([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %s", data)
	}
}

func TestRawCodec_MarshalInvalidType(t *testing.T) {
	c := rawCodec{}
	_, err := c.Marshal("not bytes")
	if err == nil {
		t.Fatal("expected error for non-[]byte type")
	}
}

func TestRawCodec_Unmarshal(t *testing.T) {
	c := rawCodec{}
	var out []byte
	if err := c.Unmarshal([]byte("data"), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "data" {
		t.Errorf("expected 'data', got %s", out)
	}
}

func TestRawCodec_UnmarshalInvalidType(t *testing.T) {
	c := rawCodec{}
	var s string
	err := c.Unmarshal([]byte("data"), &s)
	if err == nil {
		t.Fatal("expected error for non-*[]byte type")
	}
}

func TestRawCodec_Name(t *testing.T) {
	c := rawCodec{}
	if c.Name() != "raw" {
		t.Errorf("expected 'raw', got %s", c.Name())
	}
}

func TestSource_MultipleEvents(t *testing.T) {
	src, err := NewSource(Config{ListenAddr: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}

	var mu sync.Mutex
	var count int

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- src.Start(ctx, func(ctx context.Context, evt source.Event) error {
			mu.Lock()
			count++
			mu.Unlock()
			return nil
		})
	}()
	<-src.ready

	conn, err := grpc.NewClient(src.ListenAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	for i := 0; i < 5; i++ {
		var resp []byte
		err = conn.Invoke(context.Background(), "/test/Method", []byte(fmt.Sprintf(`{"n":%d}`, i)), &resp, grpc.ForceCodec(rawCodecForClient{}))
		if err != nil {
			t.Fatalf("invoke %d: %v", i, err)
		}
	}

	cancel()
	<-errCh

	mu.Lock()
	defer mu.Unlock()
	if count != 5 {
		t.Errorf("expected 5 events, got %d", count)
	}
}
