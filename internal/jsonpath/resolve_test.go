package jsonpath

import (
	"strings"
	"testing"
)

func TestResolve_SimpleField(t *testing.T) {
	data := map[string]interface{}{"name": "Alice"}
	val, err := Resolve(data, "name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "Alice" {
		t.Errorf("expected 'Alice', got %v", val)
	}
}

func TestResolve_NestedField(t *testing.T) {
	data := map[string]interface{}{
		"customer": map[string]interface{}{
			"address": map[string]interface{}{
				"city": "NYC",
			},
		},
	}
	val, err := Resolve(data, "customer.address.city")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "NYC" {
		t.Errorf("expected 'NYC', got %v", val)
	}
}

func TestResolve_MissingField(t *testing.T) {
	data := map[string]interface{}{"name": "Alice"}
	_, err := Resolve(data, "age")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err)
	}
}

func TestResolve_IntermediateNonObject(t *testing.T) {
	data := map[string]interface{}{"name": "Alice"}
	_, err := Resolve(data, "name.first")
	if err == nil {
		t.Fatal("expected error for non-object intermediate")
	}
	if !strings.Contains(err.Error(), "non-object") {
		t.Errorf("expected 'non-object' in error, got %q", err)
	}
}

func TestResolve_NullValue(t *testing.T) {
	data := map[string]interface{}{"value": nil}
	val, err := Resolve(data, "value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil, got %v", val)
	}
}

func TestResolve_ArrayPassthrough(t *testing.T) {
	data := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}
	val, err := Resolve(data, "items")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := val.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", val)
	}
	if len(arr) != 3 {
		t.Errorf("expected 3 items, got %d", len(arr))
	}
}

func TestResolve_NumericValue(t *testing.T) {
	data := map[string]interface{}{"count": float64(42)}
	val, err := Resolve(data, "count")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != float64(42) {
		t.Errorf("expected 42, got %v", val)
	}
}

func TestResolve_BoolValue(t *testing.T) {
	data := map[string]interface{}{"active": true}
	val, err := Resolve(data, "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != true {
		t.Errorf("expected true, got %v", val)
	}
}

func TestResolveString_JSONPath(t *testing.T) {
	data := map[string]interface{}{"order_id": "abc-123"}
	result := ResolveString(data, "$.order_id")
	if result != "abc-123" {
		t.Errorf("expected 'abc-123', got %q", result)
	}
}

func TestResolveString_Literal(t *testing.T) {
	data := map[string]interface{}{"order_id": "abc-123"}
	result := ResolveString(data, "order.created")
	if result != "order.created" {
		t.Errorf("expected 'order.created', got %q", result)
	}
}

func TestResolveString_MissingField_FallsBack(t *testing.T) {
	data := map[string]interface{}{}
	result := ResolveString(data, "$.nonexistent")
	if result != "$.nonexistent" {
		t.Errorf("expected raw expr '$.nonexistent', got %q", result)
	}
}

func TestResolveString_NestedPath(t *testing.T) {
	data := map[string]interface{}{
		"customer": map[string]interface{}{"id": "cust-99"},
	}
	result := ResolveString(data, "$.customer.id")
	if result != "cust-99" {
		t.Errorf("expected 'cust-99', got %q", result)
	}
}

func TestResolveString_NumericConversion(t *testing.T) {
	data := map[string]interface{}{"count": float64(42)}
	result := ResolveString(data, "$.count")
	if result != "42" {
		t.Errorf("expected '42', got %q", result)
	}
}

func TestResolveValue_String_JSONPath(t *testing.T) {
	data := map[string]interface{}{"name": "Alice"}
	val, err := ResolveValue(data, "$.name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "Alice" {
		t.Errorf("expected 'Alice', got %v", val)
	}
}

func TestResolveValue_String_Literal(t *testing.T) {
	data := map[string]interface{}{}
	val, err := ResolveValue(data, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "hello" {
		t.Errorf("expected 'hello', got %v", val)
	}
}

func TestResolveValue_NestedMap(t *testing.T) {
	data := map[string]interface{}{"x": "val"}
	nested := map[string]interface{}{"a": "$.x", "b": "literal"}
	val, err := ResolveValue(data, nested)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := val.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}
	if m["a"] != "val" {
		t.Errorf("expected a='val', got %v", m["a"])
	}
	if m["b"] != "literal" {
		t.Errorf("expected b='literal', got %v", m["b"])
	}
}

func TestResolveValue_NonString(t *testing.T) {
	data := map[string]interface{}{}
	val, err := ResolveValue(data, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42 {
		t.Errorf("expected 42, got %v", val)
	}
}

func TestResolveValue_MissingPath_Error(t *testing.T) {
	data := map[string]interface{}{}
	_, err := ResolveValue(data, "$.missing")
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestResolveMap_MixedValues(t *testing.T) {
	data := map[string]interface{}{
		"legacy_id":     "order-42",
		"customer_name": "Alice",
	}
	mapping := map[string]interface{}{
		"order_id": "$.legacy_id",
		"customer": map[string]interface{}{
			"name": "$.customer_name",
		},
		"status": "processed",
	}

	result, err := ResolveMap(data, mapping)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["order_id"] != "order-42" {
		t.Errorf("expected order_id='order-42', got %v", result["order_id"])
	}
	if result["status"] != "processed" {
		t.Errorf("expected status='processed', got %v", result["status"])
	}
	customer, ok := result["customer"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected customer to be map, got %T", result["customer"])
	}
	if customer["name"] != "Alice" {
		t.Errorf("expected customer.name='Alice', got %v", customer["name"])
	}
}

func TestResolveMap_Error_Propagated(t *testing.T) {
	data := map[string]interface{}{}
	mapping := map[string]interface{}{"a": "$.missing"}
	_, err := ResolveMap(data, mapping)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if !strings.Contains(err.Error(), "key \"a\"") {
		t.Errorf("expected key context in error, got %q", err)
	}
}
