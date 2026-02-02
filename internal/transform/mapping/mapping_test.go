package mapping

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewTransformer_Valid(t *testing.T) {
	tr, err := NewTransformer(map[string]interface{}{"a": "$.b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transformer")
	}
}

func TestNewTransformer_EmptyMapping(t *testing.T) {
	_, err := NewTransformer(map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for empty mapping")
	}
}

func TestNewTransformer_NilMapping(t *testing.T) {
	_, err := NewTransformer(nil)
	if err == nil {
		t.Fatal("expected error for nil mapping")
	}
}

func TestTransform_SimpleFieldExtraction(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{
		"order_id": "$.legacy_id",
	})
	out, err := tr.Transform(context.Background(), []byte(`{"legacy_id": "order-42"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(out, &result)
	if result["order_id"] != "order-42" {
		t.Errorf("expected order_id='order-42', got %v", result["order_id"])
	}
}

func TestTransform_NestedFieldExtraction(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{
		"city": "$.address.city",
	})
	out, err := tr.Transform(context.Background(), []byte(`{"address": {"city": "NYC"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(out, &result)
	if result["city"] != "NYC" {
		t.Errorf("expected city='NYC', got %v", result["city"])
	}
}

func TestTransform_LiteralValues(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{
		"status": "processed",
		"count":  42,
	})
	out, err := tr.Transform(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(out, &result)
	if result["status"] != "processed" {
		t.Errorf("expected status='processed', got %v", result["status"])
	}
	if result["count"] != float64(42) {
		t.Errorf("expected count=42, got %v", result["count"])
	}
}

func TestTransform_NestedOutputStructure(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{
		"customer": map[string]interface{}{
			"name":  "$.customer_name",
			"email": "$.contact_email",
		},
	})
	out, err := tr.Transform(context.Background(), []byte(`{"customer_name": "Alice", "contact_email": "alice@example.com"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(out, &result)
	customer, ok := result["customer"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected customer to be map, got %T", result["customer"])
	}
	if customer["name"] != "Alice" {
		t.Errorf("expected name='Alice', got %v", customer["name"])
	}
	if customer["email"] != "alice@example.com" {
		t.Errorf("expected email='alice@example.com', got %v", customer["email"])
	}
}

func TestTransform_MixedLiteralAndPath(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{
		"id":     "$.order_id",
		"type":   "order.created",
		"amount": "$.total",
	})
	out, err := tr.Transform(context.Background(), []byte(`{"order_id": "123", "total": 99.99}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(out, &result)
	if result["id"] != "123" {
		t.Errorf("expected id='123', got %v", result["id"])
	}
	if result["type"] != "order.created" {
		t.Errorf("expected type='order.created', got %v", result["type"])
	}
	if result["amount"] != 99.99 {
		t.Errorf("expected amount=99.99, got %v", result["amount"])
	}
}

func TestTransform_MissingField_Error(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{
		"name": "$.nonexistent",
	})
	_, err := tr.Transform(context.Background(), []byte(`{"other": "val"}`))
	if err == nil {
		t.Fatal("expected error for missing field")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err)
	}
}

func TestTransform_InvalidJSON(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{"a": "$.b"})
	_, err := tr.Transform(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("expected 'unmarshal' in error, got %q", err)
	}
}

func TestTransform_ContextCancelled(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{"a": "$.b"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tr.Transform(ctx, []byte(`{"b": "val"}`))
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestTransform_IntermediateNonObject(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{
		"val": "$.name.first",
	})
	_, err := tr.Transform(context.Background(), []byte(`{"name": "Alice"}`))
	if err == nil {
		t.Fatal("expected error for non-object intermediate")
	}
}

func TestTransform_NullValue(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{
		"val": "$.nullable",
	})
	out, err := tr.Transform(context.Background(), []byte(`{"nullable": null}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(out, &result)
	if result["val"] != nil {
		t.Errorf("expected nil, got %v", result["val"])
	}
}

func TestTransform_ArrayPassthrough(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{
		"items": "$.tags",
	})
	out, err := tr.Transform(context.Background(), []byte(`{"tags": ["a", "b", "c"]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(out, &result)
	items, ok := result["items"].([]interface{})
	if !ok {
		t.Fatalf("expected array, got %T", result["items"])
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestTransform_DeepNestedOutput(t *testing.T) {
	tr, _ := NewTransformer(map[string]interface{}{
		"level1": map[string]interface{}{
			"level2": map[string]interface{}{
				"value": "$.deep_val",
			},
		},
	})
	out, err := tr.Transform(context.Background(), []byte(`{"deep_val": "found"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(out, &result)
	l1, _ := result["level1"].(map[string]interface{})
	l2, _ := l1["level2"].(map[string]interface{})
	if l2["value"] != "found" {
		t.Errorf("expected 'found', got %v", l2["value"])
	}
}
