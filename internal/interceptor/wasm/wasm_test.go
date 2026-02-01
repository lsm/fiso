package wasm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lsm/fiso/internal/interceptor"
)

type mockRuntime struct {
	response  []byte
	err       error
	closed    bool
	lastInput []byte
}

func (m *mockRuntime) Call(_ context.Context, input []byte) ([]byte, error) {
	m.lastInput = input
	return m.response, m.err
}

func (m *mockRuntime) Close() error {
	m.closed = true
	return nil
}

func TestWASMInterceptor_Process(t *testing.T) {
	output := wasmOutput{
		Payload: json.RawMessage(`{"enriched":true}`),
		Headers: map[string]string{"X-WASM": "processed"},
	}
	respData, _ := json.Marshal(output)
	mr := &mockRuntime{response: respData}
	ic := New(mr, "test-module")

	req := &interceptor.Request{
		Payload:   []byte(`{"original":true}`),
		Headers:   map[string]string{"Content-Type": "application/json"},
		Direction: interceptor.Inbound,
	}

	result, err := ic.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Payload) != `{"enriched":true}` {
		t.Errorf("unexpected payload: %s", string(result.Payload))
	}
	if result.Headers["X-WASM"] != "processed" {
		t.Errorf("expected X-WASM header, got %v", result.Headers)
	}
	if result.Direction != interceptor.Inbound {
		t.Errorf("expected direction preserved, got %q", result.Direction)
	}
}

func TestWASMInterceptor_SendsCorrectInput(t *testing.T) {
	resp, _ := json.Marshal(wasmOutput{Payload: json.RawMessage(`{}`), Headers: map[string]string{}})
	mr := &mockRuntime{response: resp}
	ic := New(mr, "test")

	req := &interceptor.Request{
		Payload:   []byte(`{"key":"val"}`),
		Headers:   map[string]string{"X-Test": "1"},
		Direction: interceptor.Outbound,
	}
	_, _ = ic.Process(context.Background(), req)

	var input wasmInput
	if err := json.Unmarshal(mr.lastInput, &input); err != nil {
		t.Fatalf("failed to unmarshal input: %v", err)
	}
	if input.Direction != "outbound" {
		t.Errorf("expected direction 'outbound', got %q", input.Direction)
	}
	if input.Headers["X-Test"] != "1" {
		t.Errorf("expected X-Test header, got %v", input.Headers)
	}
}

func TestWASMInterceptor_RuntimeError(t *testing.T) {
	mr := &mockRuntime{err: errors.New("wasm trap")}
	ic := New(mr, "failing-module")

	req := &interceptor.Request{Payload: []byte(`{}`), Headers: map[string]string{}}
	_, err := ic.Process(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failing-module") {
		t.Errorf("expected module name in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "wasm trap") {
		t.Errorf("expected 'wasm trap' in error, got %q", err.Error())
	}
}

func TestWASMInterceptor_InvalidOutput(t *testing.T) {
	mr := &mockRuntime{response: []byte("not json")}
	ic := New(mr, "bad-module")

	req := &interceptor.Request{Payload: []byte(`{}`), Headers: map[string]string{}}
	_, err := ic.Process(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for invalid JSON output")
	}
	if !strings.Contains(err.Error(), "bad-module") {
		t.Errorf("expected module name in error, got %q", err.Error())
	}
}

func TestWASMInterceptor_Close(t *testing.T) {
	mr := &mockRuntime{}
	ic := New(mr, "test")
	_ = ic.Close()
	if !mr.closed {
		t.Error("expected runtime to be closed")
	}
}

func TestWASMInterceptor_DirectionPreserved(t *testing.T) {
	resp, _ := json.Marshal(wasmOutput{
		Payload: json.RawMessage(`{"modified":true}`),
		Headers: map[string]string{},
	})
	mr := &mockRuntime{response: resp}
	ic := New(mr, "test")

	req := &interceptor.Request{
		Payload:   []byte(`{}`),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	result, err := ic.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Direction != interceptor.Outbound {
		t.Errorf("expected outbound direction, got %q", result.Direction)
	}
}

func TestWASMInterceptor_NilHeaders(t *testing.T) {
	resp, _ := json.Marshal(wasmOutput{
		Payload: json.RawMessage(`{}`),
		Headers: nil,
	})
	mr := &mockRuntime{response: resp}
	ic := New(mr, "test")

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
		t.Errorf("expected inbound direction")
	}
}
