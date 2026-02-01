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
