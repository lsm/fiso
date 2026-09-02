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

// TestWASMInterceptor_Reject pins the rejection ABI: a guest answering with
// a reject object surfaces as the typed interceptor.RejectedError carrying
// the guest-chosen status and reason (ADR 0007).
func TestWASMInterceptor_Reject(t *testing.T) {
	resp, _ := json.Marshal(wasmOutput{
		Reject: &wasmRejection{Status: 401, Reason: "missing credentials"},
	})
	mr := &mockRuntime{response: resp}
	ic := New(mr, "auth")

	result, err := ic.Process(context.Background(), &interceptor.Request{
		Payload: []byte(`{}`), Headers: map[string]string{}, Direction: interceptor.Inbound,
	})
	if err == nil {
		t.Fatal("expected a rejection error, got nil")
	}
	if result != nil {
		t.Fatalf("a rejected request must not produce a transformed result, got %+v", result)
	}
	rej, ok := interceptor.AsRejection(err)
	if !ok {
		t.Fatalf("expected a typed rejection, got %v", err)
	}
	if rej.Status != 401 || rej.Reason != "missing credentials" {
		t.Fatalf("rejection = %+v, want status 401 and the guest reason", rej)
	}
}

// TestWASMInterceptor_RejectInvalidStatus pins the status range: only
// 400–599 are caller-facing refusals; anything else is a contract violation
// that follows the failure path, never a silent rewrite.
func TestWASMInterceptor_RejectInvalidStatus(t *testing.T) {
	for _, status := range []int{0, 302, 399, 600, 1000} {
		resp, _ := json.Marshal(wasmOutput{
			Reject: &wasmRejection{Status: status, Reason: "x"},
		})
		mr := &mockRuntime{response: resp}
		ic := New(mr, "auth")

		_, err := ic.Process(context.Background(), &interceptor.Request{
			Payload: []byte(`{}`), Headers: map[string]string{}, Direction: interceptor.Inbound,
		})
		if err == nil {
			t.Fatalf("status %d: expected an error", status)
		}
		if rej, ok := interceptor.AsRejection(err); ok {
			t.Fatalf("status %d: out-of-range status must not classify as a rejection, got %+v", status, rej)
		}
	}
}

// TestWASMInterceptor_RejectEmptyReasonAllows pins that an empty reason is a
// valid refusal (the status alone carries the meaning).
func TestWASMInterceptor_RejectEmptyReasonAllows(t *testing.T) {
	resp, _ := json.Marshal(wasmOutput{
		Reject: &wasmRejection{Status: 403},
	})
	mr := &mockRuntime{response: resp}
	ic := New(mr, "auth")

	_, err := ic.Process(context.Background(), &interceptor.Request{
		Payload: []byte(`{}`), Headers: map[string]string{}, Direction: interceptor.Inbound,
	})
	rej, ok := interceptor.AsRejection(err)
	if !ok {
		t.Fatalf("expected a typed rejection, got %v", err)
	}
	if rej.Status != 403 {
		t.Fatalf("status = %d, want 403", rej.Status)
	}
}
