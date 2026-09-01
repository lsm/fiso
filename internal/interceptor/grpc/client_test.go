package grpc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
)

// startSidecar runs a real gRPC server implementing the raw interceptor
// method and returns its address.
func startSidecar(t *testing.T, respond func(req []byte) ([]byte, error)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer(grpc.ForceServerCodec(rawCodec{}))
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "fiso.v1.InterceptorService",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Process",
			Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
				var req []byte
				if err := dec(&req); err != nil {
					return nil, err
				}
				return respond(req)
			},
		}},
	}, nil)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(srv.Stop)
	return ln.Addr().String()
}

// TestNewConnClient_RequiresAddress pins the constructor's address contract.
func TestNewConnClient_RequiresAddress(t *testing.T) {
	if _, err := NewConnClient(""); err == nil {
		t.Fatal("expected error for empty address")
	}
}

// TestConnClient_Call pins the executable sidecar contract: the raw JSON
// envelope round-trips through the conventional method path.
func TestConnClient_Call(t *testing.T) {
	addr := startSidecar(t, func(req []byte) ([]byte, error) {
		if len(req) == 0 {
			return nil, errors.New("empty request")
		}
		return append([]byte(`{"echo":`), append(req, '}')...), nil
	})
	client, err := NewConnClient(addr)
	if err != nil {
		t.Fatalf("NewConnClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	resp, err := client.Call(context.Background(), []byte(`{"payload":{"x":1}}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if string(resp) != `{"echo":{"payload":{"x":1}}}` {
		t.Fatalf("unexpected response: %s", resp)
	}
}

// TestConnClient_ServerError pins that a failing sidecar surfaces the error.
func TestConnClient_ServerError(t *testing.T) {
	addr := startSidecar(t, func([]byte) ([]byte, error) {
		return nil, errors.New("sidecar failure")
	})
	client, err := NewConnClient(addr)
	if err != nil {
		t.Fatalf("NewConnClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	if _, err := client.Call(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected the sidecar error to surface")
	}
}

// TestConnClient_CallTimeout pins that the per-call context deadline is
// honored against a sidecar that never responds.
func TestConnClient_CallTimeout(t *testing.T) {
	addr := startSidecar(t, func([]byte) ([]byte, error) {
		time.Sleep(2 * time.Second)
		return []byte(`{}`), nil
	})
	client, err := NewConnClient(addr)
	if err != nil {
		t.Fatalf("NewConnClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := client.Call(ctx, []byte(`{}`)); err == nil {
		t.Fatal("expected deadline error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("call did not honor the deadline: %v", elapsed)
	}
}

// TestRawCodec_TypeErrors pins the codec's error paths.
func TestRawCodec_TypeErrors(t *testing.T) {
	c := rawCodec{}
	if _, err := c.Marshal("not-bytes"); err == nil {
		t.Error("expected marshal error for non-bytes")
	}
	var s string
	if err := c.Unmarshal([]byte("x"), &s); err == nil {
		t.Error("expected unmarshal error for non-*[]byte")
	}
}
