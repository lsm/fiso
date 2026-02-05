package unified

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestNewTransformer_ValidFields(t *testing.T) {
	fields := map[string]string{
		"order_id": "data.legacy_id",
		"ts":       "data.timestamp",
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transformer")
	}
}

func TestNewTransformer_EmptyFields(t *testing.T) {
	_, err := NewTransformer(map[string]string{})
	if err == nil {
		t.Fatal("expected error for empty fields")
	}
}

func TestNewTransformer_NilFields(t *testing.T) {
	_, err := NewTransformer(nil)
	if err == nil {
		t.Fatal("expected error for nil fields")
	}
}

func TestNewTransformer_InvalidExpression(t *testing.T) {
	fields := map[string]string{
		"result": ">>>invalid<<<",
	}
	_, err := NewTransformer(fields)
	if err == nil {
		t.Fatal("expected error for invalid expression")
	}
}

func TestTransform_SimpleFieldMapping(t *testing.T) {
	fields := map[string]string{
		"order_id": "data.legacy_id",
		"ts":       "data.timestamp",
		"status":   `"processed"`,
	}
	tr, err := NewTransformer(fields)
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
	if out["status"] != "processed" {
		t.Errorf("expected status=processed, got %v", out["status"])
	}
	if _, exists := out["extra"]; exists {
		t.Error("expected 'extra' field to be absent")
	}
}

func TestTransform_StaticLiterals(t *testing.T) {
	fields := map[string]string{
		"status":  `"processed"`,
		"count":   `42`,
		"price":   `99.99`,
		"active":  `true`,
		"enabled": `false`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	result, err := tr.Transform(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if out["status"] != "processed" {
		t.Errorf("expected status='processed', got %v", out["status"])
	}
	if out["count"] != float64(42) {
		t.Errorf("expected count=42, got %v", out["count"])
	}
	if out["price"] != 99.99 {
		t.Errorf("expected price=99.99, got %v", out["price"])
	}
	if out["active"] != true {
		t.Errorf("expected active=true, got %v", out["active"])
	}
	if out["enabled"] != false {
		t.Errorf("expected enabled=false, got %v", out["enabled"])
	}
}

func TestTransform_ArithmeticComputation(t *testing.T) {
	fields := map[string]string{
		"total":      `data.price * data.quantity`,
		"discounted": `data.price * data.quantity * 0.9`,
		"sum":        `data.a + data.b`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"price": 10.5, "quantity": 3, "a": 5, "b": 7}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	total, ok := out["total"].(float64)
	if !ok {
		t.Fatalf("expected float64 total, got %T", out["total"])
	}
	if total != 31.5 {
		t.Errorf("expected total=31.5, got %v", total)
	}

	sum, ok := out["sum"].(float64)
	if !ok {
		t.Fatalf("expected float64 sum, got %T", out["sum"])
	}
	if sum != 12 {
		t.Errorf("expected sum=12, got %v", sum)
	}
}

func TestTransform_ConditionalExpression(t *testing.T) {
	fields := map[string]string{
		"category": `data.type == "premium" ? "gold" : "standard"`,
		"status":   `data.status == "active" ? "enabled" : "disabled"`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"type": "premium", "status": "active"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if out["category"] != "gold" {
		t.Errorf("expected category='gold', got %v", out["category"])
	}
	if out["status"] != "enabled" {
		t.Errorf("expected status='enabled', got %v", out["status"])
	}
}

func TestTransform_NestedStructures(t *testing.T) {
	fields := map[string]string{
		"customer": `{"id": data.customer_id, "name": data.customer_name, "tier": "premium"}`,
		"order":    `{"id": data.order_id, "total": data.subtotal + data.tax}`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"order_id": "ord-123", "customer_id": "cust-456", "customer_name": "Alice", "subtotal": 100.0, "tax": 8.5}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	customer, ok := out["customer"].(map[string]interface{})
	if !ok {
		t.Fatal("expected customer to be a map")
	}
	if customer["id"] != "cust-456" {
		t.Errorf("expected customer.id='cust-456', got %v", customer["id"])
	}
	if customer["name"] != "Alice" {
		t.Errorf("expected customer.name='Alice', got %v", customer["name"])
	}
	if customer["tier"] != "premium" {
		t.Errorf("expected customer.tier='premium', got %v", customer["tier"])
	}

	order, ok := out["order"].(map[string]interface{})
	if !ok {
		t.Fatal("expected order to be a map")
	}
	if order["total"] != 108.5 {
		t.Errorf("expected order.total=108.5, got %v", order["total"])
	}
}

func TestTransform_InvalidJSON(t *testing.T) {
	fields := map[string]string{
		"result": "data.id",
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	_, err = tr.Transform(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON input")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("expected 'unmarshal' in error, got %q", err)
	}
}

func TestTransform_MissingFields(t *testing.T) {
	fields := map[string]string{
		"result": "data.nonexistent",
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"id": 1}}`
	_, err = tr.Transform(context.Background(), []byte(input))
	// CEL will error on missing field access
	if err == nil {
		t.Fatal("expected error for missing field access")
	}
}

func TestTransform_ContextCancelled(t *testing.T) {
	fields := map[string]string{
		"result": "data",
	}
	tr, err := NewTransformer(fields)
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
	fields := map[string]string{
		"result": "data",
	}
	tr, err := NewTransformer(fields, WithTimeout(10*time.Second))
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
	fields := map[string]string{
		"result": "data",
	}
	tr, err := NewTransformer(fields, WithMaxOutputBytes(10))
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": "this string is definitely longer than ten bytes"}`
	_, err = tr.Transform(context.Background(), []byte(input))
	if err == nil {
		t.Fatal("expected error for output exceeding max size")
	}
}

func TestTransform_TopLevelFields(t *testing.T) {
	fields := map[string]string{
		"id":        "id",
		"eventType": "type",
		"timestamp": "time",
		"subject":   "subject",
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"id": "evt-123", "type": "order.created", "time": "2024-01-01T00:00:00Z", "subject": "order-456", "source": "web"}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if out["id"] != "evt-123" {
		t.Errorf("expected id='evt-123', got %v", out["id"])
	}
	if out["eventType"] != "order.created" {
		t.Errorf("expected eventType='order.created', got %v", out["eventType"])
	}
}

func TestTransform_StringConcatenation(t *testing.T) {
	fields := map[string]string{
		"fullName": `data.first_name + " " + data.last_name`,
		"email":    `data.username + "@" + data.domain`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"first_name": "John", "last_name": "Doe", "username": "john.doe", "domain": "example.com"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if out["fullName"] != "John Doe" {
		t.Errorf("expected fullName='John Doe', got %v", out["fullName"])
	}
	if out["email"] != "john.doe@example.com" {
		t.Errorf("expected email='john.doe@example.com', got %v", out["email"])
	}
}

func TestTransform_ComplexBooleanLogic(t *testing.T) {
	fields := map[string]string{
		"eligible": `data.age >= 18 && data.verified == true`,
		"premium":  `data.member == true && data.level >= 3`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"age": 25, "verified": true, "member": true, "level": 4}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if out["eligible"] != true {
		t.Errorf("expected eligible=true, got %v", out["eligible"])
	}
	if out["premium"] != true {
		t.Errorf("expected premium=true, got %v", out["premium"])
	}
}

func TestTransform_NullValueHandling(t *testing.T) {
	fields := map[string]string{
		"value": "data.nullable",
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"nullable": null}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Check if the field exists and is null
	val, exists := out["value"]
	if !exists {
		t.Error("expected 'value' field to exist")
	}
	// Null values in JSON unmarshal to nil in Go
	if val != nil {
		t.Errorf("expected nil (null), got %v (type: %T)", val, val)
	}
}

func TestBuildCelExpression(t *testing.T) {
	tests := []struct {
		name     string
		fields   map[string]string
		expected string
	}{
		{
			name:     "simple fields",
			fields:   map[string]string{"a": "data.x", "b": "data.y"},
			expected: `{"a":data.x,"b":data.y}`,
		},
		{
			name:     "static literals",
			fields:   map[string]string{"status": `"processed"`, "count": `42`},
			expected: `{"status":"processed","count":42}`,
		},
		{
			name:     "expressions",
			fields:   map[string]string{"total": "data.price * data.quantity"},
			expected: `{"total":data.price * data.quantity}`,
		},
		{
			name:     "special characters in keys",
			fields:   map[string]string{"field-name": "data.value"},
			expected: `{"field-name":data.value}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildCelExpression(tt.fields)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// For CEL expressions, we can't parse as JSON since values are expressions, not JSON literals.
			// Instead, normalize by sorting the key-value pairs (both should be well-formed object literals).
			if !celExprEqual(tt.expected, result) {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// celExprEqual compares two CEL object literal expressions for semantic equality,
// ignoring key order differences. Both expressions must be well-formed object literals.
func celExprEqual(a, b string) bool {
	// Quick check for exact match
	if a == b {
		return true
	}
	// Parse as {"key": value, ...} and compare key-value pairs after sorting
	if !strings.HasPrefix(a, "{") || !strings.HasPrefix(b, "{") ||
		!strings.HasSuffix(a, "}") || !strings.HasSuffix(b, "}") {
		return false
	}
	// Extract and sort the key-value pairs
	aPairs := parseObjPairs(a[1 : len(a)-1])
	bPairs := parseObjPairs(b[1 : len(b)-1])
	if len(aPairs) != len(bPairs) {
		return false
	}
	for i := range aPairs {
		if aPairs[i] != bPairs[i] {
			return false
		}
	}
	return true
}

// parseObjPairs splits "key1: val1, key2: val2" into sorted ["key1: val1", "key2: val2"]
func parseObjPairs(s string) []string {
	pairs := strings.Split(s, ",")
	for i := range pairs {
		pairs[i] = strings.TrimSpace(pairs[i])
	}
	// Sort by key (the part before the first colon)
	sort.Slice(pairs, func(i, j int) bool {
		iKey := pairs[i]
		jKey := pairs[j]
		if idx := strings.Index(iKey, ":"); idx >= 0 {
			iKey = strings.TrimSpace(iKey[:idx])
		}
		if idx := strings.Index(jKey, ":"); idx >= 0 {
			jKey = strings.TrimSpace(jKey[:idx])
		}
		return iKey < jKey
	})
	return pairs
}

func TestBuildCelExpression_Empty(t *testing.T) {
	_, err := buildCelExpression(map[string]string{})
	if err == nil {
		t.Fatal("expected error for empty fields")
	}
}

func TestNewTransformer_WithOptions(t *testing.T) {
	tr, err := NewTransformer(
		map[string]string{"result": "data"},
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

func TestTransform_ArrayConstruction(t *testing.T) {
	fields := map[string]string{
		"items": `[data.a, data.b, data.c]`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"a": 1, "b": 2, "c": 3}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	items, ok := out["items"].([]interface{})
	if !ok {
		t.Fatalf("expected items to be an array, got %T", out["items"])
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestTransform_MixedLiteralAndExpressions(t *testing.T) {
	fields := map[string]string{
		"id":        "data.id",
		"type":      `"order.created"`,
		"timestamp": "data.time",
		"version":   `"1.0"`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"id": "ord-123", "time": "2024-01-01T00:00:00Z"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if out["id"] != "ord-123" {
		t.Errorf("expected id='ord-123', got %v", out["id"])
	}
	if out["type"] != "order.created" {
		t.Errorf("expected type='order.created', got %v", out["type"])
	}
	if out["version"] != "1.0" {
		t.Errorf("expected version='1.0', got %v", out["version"])
	}
}
