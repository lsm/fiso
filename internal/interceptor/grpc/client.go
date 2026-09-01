package grpc

import (
	"context"
	"fmt"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// interceptorMethod is the conventional raw-unary method path an interceptor
// sidecar implements, mirroring the Flow gRPC sink's contract: the request
// message is the JSON envelope and the response message is the JSON
// response, both as raw bytes with no protobuf.
const interceptorMethod = "/fiso.v1.InterceptorService/Process"

// connClient is a Client that invokes the sidecar over a gRPC client
// connection using the raw codec.
type connClient struct {
	conn *grpc.ClientConn
}

// NewConnClient dials the interceptor sidecar service at addr. The
// connection is lazy; construction succeeds without a listening server.
func NewConnClient(addr string) (Client, error) {
	if addr == "" {
		return nil, fmt.Errorf("grpc interceptor address is required")
	}
	// Interceptor responses carry the full transformed payload; the gRPC
	// default 4 MiB receive limit would reject large legitimate events.
	const maxReceiveBytes = 64 << 20
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxReceiveBytes)),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial: %w", err)
	}
	return &connClient{conn: conn}, nil
}

// Call invokes the interceptor method with the JSON envelope.
func (c *connClient) Call(ctx context.Context, data []byte) ([]byte, error) {
	var resp []byte
	if err := c.conn.Invoke(ctx, interceptorMethod, data, &resp, grpc.ForceCodec(rawCodec{})); err != nil {
		return nil, err
	}
	return resp, nil
}

// Close closes the underlying connection.
func (c *connClient) Close() error {
	return c.conn.Close()
}

// rawCodec is a gRPC codec that sends/receives raw bytes without protobuf.
type rawCodec struct{}

func (rawCodec) Marshal(v interface{}) ([]byte, error) {
	b, ok := v.([]byte)
	if !ok {
		return nil, fmt.Errorf("rawCodec: expected []byte, got %T", v)
	}
	return b, nil
}

func (rawCodec) Unmarshal(data []byte, v interface{}) error {
	bp, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("rawCodec: expected *[]byte, got %T", v)
	}
	// Copy: gRPC-Go may recycle the receive buffer after Unmarshal returns,
	// and the caller reads the bytes afterwards (concurrent RPCs would
	// otherwise race on pooled memory).
	*bp = append([]byte(nil), data...)
	return nil
}

func (rawCodec) Name() string { return "raw" }
