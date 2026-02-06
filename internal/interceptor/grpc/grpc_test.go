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
	closeErr error
}

func (m *mockClient) Call(_ context.Context, data []byte) ([]byte, error) {
	m.lastData = data
	return m.response, m.err
}

func (m *mockClient) Close() error {
	m.closed = true
	if m.closeErr != nil {
		return m.closeErr
	}
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

func TestInterceptor_EmptyPayload(t *testing.T) {
	resp, _ := json.Marshal(responsePayload{
		Payload: json.RawMessage(`{}`),
		Headers: map[string]string{},
	})
	mc := &mockClient{response: resp}
	ic := New(mc, 5*time.Second)

	// Test with empty JSON object
	req := &interceptor.Request{
		Payload:   []byte(`{}`),
		Headers:   map[string]string{},
		Direction: interceptor.Inbound,
	}

	result, err := ic.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error with empty payload: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Test with null JSON
	req2 := &interceptor.Request{
		Payload:   []byte(`null`),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	result2, err := ic.Process(context.Background(), req2)
	if err != nil {
		t.Fatalf("unexpected error with null payload: %v", err)
	}
	if result2 == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestInterceptor_LargePayload(t *testing.T) {
	// Test with a large payload to ensure marshaling works correctly
	largePayload := make([]byte, 10*1024) // 10KB
	for i := range largePayload {
		largePayload[i] = byte('a' + (i % 26))
	}
	largeJSON := `{"data":"` + string(largePayload) + `"}`

	resp, _ := json.Marshal(responsePayload{
		Payload: json.RawMessage(`{"processed":true}`),
		Headers: map[string]string{"X-Size": "large"},
	})
	mc := &mockClient{response: resp}
	ic := New(mc, 5*time.Second)

	req := &interceptor.Request{
		Payload:   []byte(largeJSON),
		Headers:   map[string]string{"Content-Type": "application/json"},
		Direction: interceptor.Outbound,
	}

	result, err := ic.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error with large payload: %v", err)
	}
	if result.Headers["X-Size"] != "large" {
		t.Errorf("expected X-Size header, got %v", result.Headers)
	}
}

func TestInterceptor_MultipleDirections(t *testing.T) {
	resp, _ := json.Marshal(responsePayload{
		Payload: json.RawMessage(`{"ok":true}`),
		Headers: map[string]string{},
	})
	mc := &mockClient{response: resp}
	ic := New(mc, 5*time.Second)

	// Test outbound direction
	reqOut := &interceptor.Request{
		Payload:   []byte(`{"direction":"test"}`),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	resultOut, err := ic.Process(context.Background(), reqOut)
	if err != nil {
		t.Fatalf("unexpected error for outbound: %v", err)
	}
	if resultOut.Direction != interceptor.Outbound {
		t.Errorf("expected outbound direction preserved")
	}

	// Verify the sent direction was "outbound"
	var sentOut requestPayload
	json.Unmarshal(mc.lastData, &sentOut)
	if sentOut.Direction != "outbound" {
		t.Errorf("expected sent direction 'outbound', got %q", sentOut.Direction)
	}

	// Test inbound direction
	reqIn := &interceptor.Request{
		Payload:   []byte(`{"direction":"test"}`),
		Headers:   map[string]string{},
		Direction: interceptor.Inbound,
	}

	resultIn, err := ic.Process(context.Background(), reqIn)
	if err != nil {
		t.Fatalf("unexpected error for inbound: %v", err)
	}
	if resultIn.Direction != interceptor.Inbound {
		t.Errorf("expected inbound direction preserved")
	}

	// Verify the sent direction was "inbound"
	var sentIn requestPayload
	json.Unmarshal(mc.lastData, &sentIn)
	if sentIn.Direction != "inbound" {
		t.Errorf("expected sent direction 'inbound', got %q", sentIn.Direction)
	}
}

func TestInterceptor_CancelledContext(t *testing.T) {
	resp, _ := json.Marshal(responsePayload{
		Payload: json.RawMessage(`{"ok":true}`),
		Headers: map[string]string{},
	})
	mc := &mockClient{
		response: resp,
		err:      context.Canceled,
	}
	ic := New(mc, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := &interceptor.Request{
		Payload:   []byte(`{"test":true}`),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	_, err := ic.Process(ctx, req)
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}
	if !strings.Contains(err.Error(), "grpc interceptor call") {
		t.Errorf("expected grpc interceptor call error, got %q", err.Error())
	}
}

func TestInterceptor_SpecialCharactersInHeaders(t *testing.T) {
	resp, _ := json.Marshal(responsePayload{
		Payload: json.RawMessage(`{}`),
		Headers: map[string]string{
			"X-Special": "value-with-üñíçödé",
			"X-Quotes":  `value"with"quotes`,
		},
	})
	mc := &mockClient{response: resp}
	ic := New(mc, 5*time.Second)

	req := &interceptor.Request{
		Payload: []byte(`{}`),
		Headers: map[string]string{
			"X-Input": "input-with-spëçiål-chars",
		},
		Direction: interceptor.Inbound,
	}

	result, err := ic.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Headers["X-Special"] != "value-with-üñíçödé" {
		t.Errorf("special characters not preserved in headers")
	}
	if result.Headers["X-Quotes"] != `value"with"quotes` {
		t.Errorf("quotes not preserved in headers")
	}
}

func TestInterceptor_EmptyResponse(t *testing.T) {
	// Test response with empty payload and headers
	resp, _ := json.Marshal(responsePayload{
		Payload: json.RawMessage(`{}`),
		Headers: map[string]string{},
	})
	mc := &mockClient{response: resp}
	ic := New(mc, 5*time.Second)

	req := &interceptor.Request{
		Payload:   []byte(`{"data":"test"}`),
		Headers:   map[string]string{"X-Test": "value"},
		Direction: interceptor.Outbound,
	}

	result, err := ic.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if string(result.Payload) != `{}` {
		t.Errorf("expected empty object payload, got %s", result.Payload)
	}
}

func TestInterceptor_ComplexNestedJSON(t *testing.T) {
	nestedJSON := `{
		"level1": {
			"level2": {
				"level3": {
					"array": [1, 2, 3],
					"nested_array": [
						{"id": 1, "name": "test1"},
						{"id": 2, "name": "test2"}
					]
				}
			}
		}
	}`

	resp, _ := json.Marshal(responsePayload{
		Payload: json.RawMessage(`{"processed":true}`),
		Headers: map[string]string{},
	})
	mc := &mockClient{response: resp}
	ic := New(mc, 5*time.Second)

	req := &interceptor.Request{
		Payload:   []byte(nestedJSON),
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	result, err := ic.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error with nested JSON: %v", err)
	}

	// Verify that the nested JSON was properly sent
	var sent requestPayload
	json.Unmarshal(mc.lastData, &sent)

	// Verify the sent payload contains the nested structure
	var sentPayloadMap map[string]interface{}
	if err := json.Unmarshal(sent.Payload, &sentPayloadMap); err != nil {
		t.Fatalf("failed to unmarshal sent payload: %v", err)
	}
	if sentPayloadMap["level1"] == nil {
		t.Error("nested JSON structure not preserved")
	}

	if string(result.Payload) != `{"processed":true}` {
		t.Errorf("unexpected response payload: %s", result.Payload)
	}
}

func TestInterceptor_ResponseWithNullPayload(t *testing.T) {
	resp, _ := json.Marshal(responsePayload{
		Payload: json.RawMessage(`null`),
		Headers: map[string]string{"X-Null": "true"},
	})
	mc := &mockClient{response: resp}
	ic := New(mc, 5*time.Second)

	req := &interceptor.Request{
		Payload:   []byte(`{"input":true}`),
		Headers:   map[string]string{},
		Direction: interceptor.Inbound,
	}

	result, err := ic.Process(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Payload) != "null" {
		t.Errorf("expected null payload, got %s", result.Payload)
	}
	if result.Headers["X-Null"] != "true" {
		t.Error("expected X-Null header")
	}
}

func TestInterceptor_CloseWithError(t *testing.T) {
	mc := &mockClient{closeErr: errors.New("close failed")}
	ic := New(mc, 5*time.Second)

	err := ic.Close()
	if err == nil {
		t.Fatal("expected error from Close")
	}
	if !strings.Contains(err.Error(), "close failed") {
		t.Errorf("expected 'close failed' error, got %q", err.Error())
	}
	if !mc.closed {
		t.Error("expected client close to be called")
	}
}

func TestInterceptor_InvalidJSONInPayload(t *testing.T) {
	// Test with invalid JSON in payload
	// json.RawMessage validates JSON when marshaling, so invalid JSON will trigger an error
	mc := &mockClient{} // Won't be called since we expect early error
	ic := New(mc, 5*time.Second)

	// Create payload with invalid JSON (raw bytes that aren't valid JSON)
	invalidJSON := []byte{0xff, 0xfe, 0xfd}
	req := &interceptor.Request{
		Payload:   invalidJSON,
		Headers:   map[string]string{},
		Direction: interceptor.Outbound,
	}

	// This should fail during json.Marshal of requestPayload
	_, err := ic.Process(context.Background(), req)
	if err == nil {
		t.Fatal("expected error with invalid JSON in payload")
	}
	if !strings.Contains(err.Error(), "marshal request") {
		t.Errorf("expected 'marshal request' error, got %q", err.Error())
	}
}
