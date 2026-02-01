package interceptor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// mockInterceptor is a test interceptor that optionally modifies or rejects requests.
type mockInterceptor struct {
	modifyHeader string
	modifyValue  string
	modifyPayload []byte
	err          error
	closed       bool
	closeErr     error
}

func (m *mockInterceptor) Process(_ context.Context, req *Request) (*Request, error) {
	if m.err != nil {
		return nil, m.err
	}
	result := &Request{
		Payload:   req.Payload,
		Headers:   make(map[string]string),
		Direction: req.Direction,
	}
	for k, v := range req.Headers {
		result.Headers[k] = v
	}
	if m.modifyHeader != "" {
		result.Headers[m.modifyHeader] = m.modifyValue
	}
	if m.modifyPayload != nil {
		result.Payload = m.modifyPayload
	}
	return result, nil
}

func (m *mockInterceptor) Close() error {
	m.closed = true
	return m.closeErr
}

func TestChain_EmptyChain(t *testing.T) {
	chain := NewChain()
	req := &Request{Payload: []byte("hello"), Headers: map[string]string{}}
	result, err := chain.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Payload) != "hello" {
		t.Errorf("expected 'hello', got %q", string(result.Payload))
	}
}

func TestChain_SingleInterceptor(t *testing.T) {
	ic := &mockInterceptor{modifyHeader: "X-Enriched", modifyValue: "true"}
	chain := NewChain(ic)
	req := &Request{Payload: []byte("data"), Headers: map[string]string{}}
	result, err := chain.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Headers["X-Enriched"] != "true" {
		t.Errorf("expected header X-Enriched=true, got %v", result.Headers)
	}
}

func TestChain_MultipleInterceptors(t *testing.T) {
	ic1 := &mockInterceptor{modifyHeader: "X-Step", modifyValue: "1"}
	ic2 := &mockInterceptor{modifyHeader: "X-Step", modifyValue: "2"}
	ic3 := &mockInterceptor{modifyPayload: []byte("modified")}

	chain := NewChain(ic1, ic2, ic3)
	req := &Request{Payload: []byte("original"), Headers: map[string]string{}}
	result, err := chain.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Last interceptor to set X-Step wins
	if result.Headers["X-Step"] != "2" {
		t.Errorf("expected X-Step=2, got %q", result.Headers["X-Step"])
	}
	if string(result.Payload) != "modified" {
		t.Errorf("expected 'modified', got %q", string(result.Payload))
	}
}

func TestChain_InterceptorError(t *testing.T) {
	ic1 := &mockInterceptor{modifyHeader: "X-OK", modifyValue: "true"}
	ic2 := &mockInterceptor{err: errors.New("rejected")}
	ic3 := &mockInterceptor{modifyHeader: "X-Never", modifyValue: "reached"}

	chain := NewChain(ic1, ic2, ic3)
	req := &Request{Payload: []byte("data"), Headers: map[string]string{}}
	_, err := chain.Process(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "interceptor 1") {
		t.Errorf("expected error from interceptor 1, got %q", err.Error())
	}
}

func TestChain_Close(t *testing.T) {
	ic1 := &mockInterceptor{}
	ic2 := &mockInterceptor{}
	chain := NewChain(ic1, ic2)

	err := chain.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ic1.closed {
		t.Error("expected interceptor 1 to be closed")
	}
	if !ic2.closed {
		t.Error("expected interceptor 2 to be closed")
	}
}

func TestChain_CloseError(t *testing.T) {
	ic1 := &mockInterceptor{closeErr: errors.New("close failed")}
	ic2 := &mockInterceptor{}
	chain := NewChain(ic1, ic2)

	err := chain.Close()
	if err == nil {
		t.Fatal("expected error")
	}
	// Both should still be closed even if first errors
	if !ic1.closed || !ic2.closed {
		t.Error("expected both interceptors to be closed")
	}
}

func TestChain_Len(t *testing.T) {
	chain := NewChain(&mockInterceptor{}, &mockInterceptor{})
	if chain.Len() != 2 {
		t.Errorf("expected length 2, got %d", chain.Len())
	}
}

func TestChain_PreservesDirection(t *testing.T) {
	ic := &mockInterceptor{}
	chain := NewChain(ic)
	req := &Request{Payload: []byte("data"), Headers: map[string]string{}, Direction: Outbound}
	result, _ := chain.Process(context.Background(), req)
	if result.Direction != Outbound {
		t.Errorf("expected Outbound direction, got %q", result.Direction)
	}
}

func TestChain_PreservesExistingHeaders(t *testing.T) {
	ic := &mockInterceptor{modifyHeader: "X-New", modifyValue: "added"}
	chain := NewChain(ic)
	req := &Request{
		Payload:   []byte("data"),
		Headers:   map[string]string{"X-Existing": "kept"},
		Direction: Inbound,
	}
	result, _ := chain.Process(context.Background(), req)
	if result.Headers["X-Existing"] != "kept" {
		t.Errorf("expected existing header to be preserved, got %v", result.Headers)
	}
	if result.Headers["X-New"] != "added" {
		t.Errorf("expected new header, got %v", result.Headers)
	}
}
