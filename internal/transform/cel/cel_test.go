package cel

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestNewTransformer_ValidExpression(t *testing.T) {
	tr, err := NewTransformer("data.id")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transformer")
	}
}

func TestNewTransformer_InvalidExpression(t *testing.T) {
	_, err := NewTransformer(">>>invalid<<<")
	if err == nil {
		t.Fatal("expected error for invalid expression")
	}
}

func TestTransform_SimpleFieldExtraction(t *testing.T) {
	tr, err := NewTransformer(`{"order_id": data.legacy_id, "ts": data.timestamp}`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"legacy_id": "ord-123", "timestamp": "2024-01-01T00:00:00Z", "extra": "dropped"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if out["order_id"] != "ord-123" {
		t.Errorf("expected order_id=ord-123, got %v", out["order_id"])
	}
	if out["ts"] != "2024-01-01T00:00:00Z" {
		t.Errorf("expected ts=2024-01-01T00:00:00Z, got %v", out["ts"])
	}
	if _, exists := out["extra"]; exists {
		t.Error("expected 'extra' field to be absent")
	}
}

func TestTransform_PassthroughExpression(t *testing.T) {
	tr, err := NewTransformer("data")
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"id": 42, "name": "test"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if out["name"] != "test" {
		t.Errorf("expected name=test, got %v", out["name"])
	}
}

func TestTransform_InvalidJSON(t *testing.T) {
	tr, err := NewTransformer("data.id")
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	_, err = tr.Transform(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON input")
	}
}

func TestTransform_MissingField(t *testing.T) {
	tr, err := NewTransformer("data.nonexistent")
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"id": 1}}`
	_, err = tr.Transform(context.Background(), []byte(input))
	if err == nil {
		t.Fatal("expected error for missing field access")
	}
}

func TestTransform_ContextCancelled(t *testing.T) {
	tr, err := NewTransformer("data")
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = tr.Transform(ctx, []byte(`{"data": "hello"}`))
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestTransform_WithTimeout(t *testing.T) {
	tr, err := NewTransformer("data", WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	result, err := tr.Transform(context.Background(), []byte(`{"data": "fast"}`))
	if err != nil {
		t.Fatalf("expected success for fast transform, got: %v", err)
	}

	var out interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
}

func TestTransform_MaxOutputSize(t *testing.T) {
	tr, err := NewTransformer("data", WithMaxOutputBytes(10))
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": "this string is definitely longer than ten bytes"}`
	_, err = tr.Transform(context.Background(), []byte(input))
	if err == nil {
		t.Fatal("expected error for output exceeding max size")
	}
}

func TestTransform_ConditionalExpression(t *testing.T) {
	tr, err := NewTransformer(`data.status == "active" ? "enabled" : "disabled"`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"status": "active"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out string
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if out != "enabled" {
		t.Errorf("expected 'enabled', got %q", out)
	}
}

func TestTransform_NumericComputation(t *testing.T) {
	tr, err := NewTransformer(`{"total": data.price * data.quantity}`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"price": 10.5, "quantity": 3}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	total, ok := out["total"].(float64)
	if !ok {
		t.Fatalf("expected float64 total, got %T", out["total"])
	}
	if total != 31.5 {
		t.Errorf("expected total=31.5, got %v", total)
	}
}

func TestTransform_ListOutput(t *testing.T) {
	tr, err := NewTransformer(`[data.a, data.b, data.c]`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}
	input := `{"data": {"a": 1, "b": 2, "c": 3}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}
	var out []interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(out) != 3 {
		t.Errorf("expected 3 elements, got %d", len(out))
	}
}

func TestTransform_BooleanOutput(t *testing.T) {
	tr, err := NewTransformer(`data.count > 0`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}
	result, err := tr.Transform(context.Background(), []byte(`{"data": {"count": 5}}`))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}
	var out bool
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out {
		t.Error("expected true")
	}
}

func TestTransform_IntegerOutput(t *testing.T) {
	tr, err := NewTransformer(`data.a + data.b`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}
	result, err := tr.Transform(context.Background(), []byte(`{"data": {"a": 3, "b": 7}}`))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}
	var out float64
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != 10 {
		t.Errorf("expected 10, got %v", out)
	}
}

func TestTransform_DoubleOutput(t *testing.T) {
	tr, err := NewTransformer(`data.price * 1.1`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}
	result, err := tr.Transform(context.Background(), []byte(`{"data": {"price": 100.0}}`))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}
	var out float64
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out < 109 || out > 111 {
		t.Errorf("expected ~110, got %v", out)
	}
}

func TestTransform_NestedMapOutput(t *testing.T) {
	tr, err := NewTransformer(`{"nested": {"id": data.id, "active": true}}`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}
	result, err := tr.Transform(context.Background(), []byte(`{"data": {"id": "abc"}}`))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nested, ok := out["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("expected nested map")
	}
	if nested["id"] != "abc" {
		t.Errorf("expected 'abc', got %v", nested["id"])
	}
}

func TestTransform_NullOutput(t *testing.T) {
	tr, err := NewTransformer(`data.missing`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}
	// data.missing will be null since we set it to nil in activation
	_, err = tr.Transform(context.Background(), []byte(`{"data": null}`))
	// CEL will error on null access
	if err == nil {
		t.Fatal("expected error for null field access")
	}
}

func TestNewTransformer_WithOptions(t *testing.T) {
	tr, err := NewTransformer("data",
		WithTimeout(10*time.Second),
		WithMaxOutputBytes(512),
	)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}
	if tr.timeout != 10*time.Second {
		t.Errorf("expected 10s timeout, got %v", tr.timeout)
	}
	if tr.maxOutputBytes != 512 {
		t.Errorf("expected 512 max bytes, got %d", tr.maxOutputBytes)
	}
}

func TestTransform_StringOutput(t *testing.T) {
	tr, err := NewTransformer(`"hello " + data.name`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}
	result, err := tr.Transform(context.Background(), []byte(`{"data": {"name": "world"}}`))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}
	var out string
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != "hello world" {
		t.Errorf("expected 'hello world', got %q", out)
	}
}

func TestTransform_TimestampOutput(t *testing.T) {
	tr, err := NewTransformer(`timestamp("2024-01-01T00:00:00Z")`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}
	result, err := tr.Transform(context.Background(), []byte(`{"data": {}}`))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	// The timestamp should be converted to a value via Value() fallback
	var out interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Timestamp converts to a time.Time which marshals as RFC3339 string
	if out == nil {
		t.Error("expected non-nil timestamp output")
	}
}

func TestTransform_BytesOutput(t *testing.T) {
	// CEL bytes literal
	tr, err := NewTransformer(`b"hello"`)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}
	result, err := tr.Transform(context.Background(), []byte(`{"data": {}}`))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	// Bytes should be converted via Value() fallback
	var out interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The output should be a string (base64 encoded bytes or similar)
	if out == nil {
		t.Error("expected non-nil bytes output")
	}
}
