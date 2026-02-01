package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lsm/fiso/internal/interceptor"
)

type mockClient struct {
	response []byte
	err      error
	closed   bool
	lastData []byte
}

func (m *mockClient) Call(_ context.Context, data []byte) ([]byte, error) {
	m.lastData = data
	return m.response, m.err
}

func (m *mockClient) Close() error {
	m.closed = true
	return nil
}

func TestInterceptor_Process(t *testing.T) {
	resp := responsePayload{
		Payload: json.RawMessage(`{"enriched":true}`),
		Headers: map[string]string{"X-Processed": "yes"},
	}
	respData, _ := json.Marshal(resp)

	mc := &mockClient{response: respData}
	ic := New(mc, 5*time.Second)

	req := &interceptor.Request{
		Payload:   []byte(`{"original":true}`),
		Headers:   map[string]string{"Content-Type": "application/json"},
		Direction: interceptor.Outbound,
	}

	result, err := ic.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Payload) != `{"enriched":true}` {
		t.Errorf("unexpected payload: %s", string(result.Payload))
	}
	if result.Headers["X-Processed"] != "yes" {
		t.Errorf("expected X-Processed header, got %v", result.Headers)
	}
	if result.Direction != interceptor.Outbound {
		t.Errorf("expected direction preserved, got %q", result.Direction)
	}
}

func TestInterceptor_SendsCorrectPayload(t *testing.T) {
	resp, _ := json.Marshal(responsePayload{
		Payload: json.RawMessage(`{}`),
		Headers: map[string]string{},
	})
	mc := &mockClient{response: resp}
	ic := New(mc, 5*time.Second)

	req := &interceptor.Request{
		Payload:   []byte(`{"test":1}`),
		Headers:   map[string]string{"X-Flow": "test"},
		Direction: interceptor.Inbound,
	}
	_, _ = ic.Process(context.Background(), req)

	var sent requestPayload
	if err := json.Unmarshal(mc.lastData, &sent); err != nil {
		t.Fatalf("failed to unmarshal sent data: %v", err)
	}
	if sent.Direction != "inbound" {
		t.Errorf("expected direction 'inbound', got %q", sent.Direction)
	}
	if sent.Headers["X-Flow"] != "test" {
		t.Errorf("expected X-Flow header in sent data, got %v", sent.Headers)
	}
}

func TestInterceptor_ClientError(t *testing.T) {
	mc := &mockClient{err: errors.New("sidecar unavailable")}
	ic := New(mc, 5*time.Second)

	req := &interceptor.Request{Payload: []byte(`{}`), Headers: map[string]string{}}
	_, err := ic.Process(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "sidecar unavailable") {
		t.Errorf("expected 'sidecar unavailable', got %q", err.Error())
	}
}

func TestInterceptor_InvalidResponse(t *testing.T) {
	mc := &mockClient{response: []byte("not json")}
	ic := New(mc, 5*time.Second)

	req := &interceptor.Request{Payload: []byte(`{}`), Headers: map[string]string{}}
	_, err := ic.Process(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestInterceptor_Close(t *testing.T) {
	mc := &mockClient{}
	ic := New(mc, 0) // test default timeout
	_ = ic.Close()
	if !mc.closed {
		t.Error("expected client to be closed")
	}
}

func TestInterceptor_DefaultTimeout(t *testing.T) {
	mc := &mockClient{response: []byte(`{"payload":{},"headers":{}}`)}
	ic := New(mc, 0)
	if ic.timeout != 5*time.Second {
		t.Errorf("expected default timeout 5s, got %v", ic.timeout)
	}
}

func TestInterceptor_ContextTimeout(t *testing.T) {
	// Test that the interceptor respects context timeout
	resp, _ := json.Marshal(responsePayload{
		Payload: json.RawMessage(`{"ok":true}`),
		Headers: map[string]string{},
	})
	mc := &mockClient{response: resp}
	ic := New(mc, 1*time.Second)

	req := &interceptor.Request{
		Payload:   []byte(`{"data":"test"}`),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := ic.Process(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Payload) != `{"ok":true}` {
		t.Errorf("unexpected payload: %s", result.Payload)
	}
}

func TestInterceptor_NilHeaders(t *testing.T) {
	resp, _ := json.Marshal(responsePayload{
		Payload: json.RawMessage(`{}`),
		Headers: nil,
	})
	mc := &mockClient{response: resp}
	ic := New(mc, 5*time.Second)

	req := &interceptor.Request{
		Payload:   []byte(`{}`),
		Headers:   nil,
		Direction: interceptor.Inbound,
	}

	result, err := ic.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Direction != interceptor.Inbound {
		t.Errorf("expected direction preserved")
	}
}
