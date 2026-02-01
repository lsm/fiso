package grpc

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// testServer is a gRPC server that records received events using
// the UnknownServiceHandler pattern.
type testServer struct {
	mu       sync.Mutex
	events   [][]byte
	headers  []metadata.MD
	listener *bufconn.Listener
	server   *grpc.Server
}

func newTestServer() *testServer {
	ts := &testServer{
		listener: bufconn.Listen(bufSize),
	}
	ts.server = grpc.NewServer(
		grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(ts.handleUnknown),
	)
	return ts
}

func (ts *testServer) handleUnknown(_ interface{}, stream grpc.ServerStream) error {
	var data []byte
	if err := stream.RecvMsg(&data); err != nil {
		return err
	}

	md, _ := metadata.FromIncomingContext(stream.Context())
	ts.mu.Lock()
	ts.events = append(ts.events, data)
	ts.headers = append(ts.headers, md)
	ts.mu.Unlock()

	return stream.SendMsg([]byte("ok"))
}

func (ts *testServer) start() {
	go func() { _ = ts.server.Serve(ts.listener) }()
}

func (ts *testServer) stop() {
	ts.server.Stop()
}

func (ts *testServer) dialer() func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, _ string) (net.Conn, error) {
		return ts.listener.DialContext(ctx)
	}
}

func TestSink_Deliver(t *testing.T) {
	ts := newTestServer()
	ts.start()
	defer ts.stop()

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(ts.dialer()),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	s := &Sink{conn: conn, timeout: 5e9}

	event := []byte(`{"id":"123","type":"test.event"}`)
	headers := map[string]string{
		"content-type":    "application/cloudevents+json",
		"x-custom-header": "value",
	}

	if err := s.Deliver(context.Background(), event, headers); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	if len(ts.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(ts.events))
	}
	if string(ts.events[0]) != string(event) {
		t.Errorf("expected event %s, got %s", event, ts.events[0])
	}
	if xc := ts.headers[0].Get("x-custom-header"); len(xc) == 0 || xc[0] != "value" {
		t.Errorf("expected x-custom-header, got %v", ts.headers[0])
	}
}

func TestSink_MultipleDeliveries(t *testing.T) {
	ts := newTestServer()
	ts.start()
	defer ts.stop()

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(ts.dialer()),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	s := &Sink{conn: conn, timeout: 5e9}

	for i := 0; i < 5; i++ {
		if err := s.Deliver(context.Background(), []byte(`{"n":1}`), nil); err != nil {
			t.Fatalf("deliver %d: %v", i, err)
		}
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.events) != 5 {
		t.Errorf("expected 5 events, got %d", len(ts.events))
	}
}

func TestSink_Close(t *testing.T) {
	ts := newTestServer()
	ts.start()
	defer ts.stop()

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(ts.dialer()),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	s := &Sink{conn: conn, timeout: 5e9}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestNewSink_MissingAddress(t *testing.T) {
	_, err := NewSink(Config{})
	if err == nil {
		t.Fatal("expected error for missing address")
	}
}

func TestRawCodec_MarshalInvalidType(t *testing.T) {
	c := rawCodec{}
	_, err := c.Marshal("not-bytes")
	if err == nil {
		t.Fatal("expected error for non-[]byte type")
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
		t.Errorf("expected name raw, got %s", c.Name())
	}
}

func TestNewSink_ValidConfig(t *testing.T) {
	s, err := NewSink(Config{Address: "localhost:50051"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil sink")
	}
	_ = s.Close()
}

func TestNewSink_TLSRequiresCredentials(t *testing.T) {
	// When TLS is true but no credentials are configured, grpc.NewClient fails
	_, err := NewSink(Config{Address: "localhost:50051", TLS: true})
	if err == nil {
		t.Fatal("expected error for TLS without credentials")
	}
}

func TestNewSink_DefaultTimeout(t *testing.T) {
	s, err := NewSink(Config{Address: "localhost:50051"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = s.Close() }()
	if s.timeout != 30e9 {
		t.Errorf("expected default timeout 30s, got %v", s.timeout)
	}
}

func TestNewSink_CustomTimeout(t *testing.T) {
	s, err := NewSink(Config{Address: "localhost:50051", Timeout: 5e9})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = s.Close() }()
	if s.timeout != 5e9 {
		t.Errorf("expected timeout 5s, got %v", s.timeout)
	}
}

// Ensure Sink satisfies io.Closer
var _ io.Closer = (*Sink)(nil)
