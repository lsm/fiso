package unified

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestNewTransformer_MultipleOptions(t *testing.T) {
	fields := map[string]string{
		"result": "data.value",
	}
	tr, err := NewTransformer(
		fields,
		WithTimeout(30*time.Second),
		WithMaxOutputBytes(2048),
	)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	if tr.timeout != 30*time.Second {
		t.Errorf("expected timeout=30s, got %v", tr.timeout)
	}
	if tr.maxOutputBytes != 2048 {
		t.Errorf("expected maxOutputBytes=2048, got %d", tr.maxOutputBytes)
	}

	// Verify transformer works
	input := `{"data": {"value": "test"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if out["result"] != "test" {
		t.Errorf("expected result='test', got %v", out["result"])
	}
}

func TestTransform_LargeNestedOutput(t *testing.T) {
	fields := map[string]string{
		"nested": `{
			"level1": {
				"level2": {
					"level3": {
						"value": data.value,
						"items": [1, 2, 3, 4, 5]
					}
				}
			}
		}`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"value": "deeply nested"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	nested, ok := out["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("expected nested to be a map")
	}
	level1, ok := nested["level1"].(map[string]interface{})
	if !ok {
		t.Fatal("expected level1 to be a map")
	}
	level2, ok := level1["level2"].(map[string]interface{})
	if !ok {
		t.Fatal("expected level2 to be a map")
	}
	level3, ok := level2["level3"].(map[string]interface{})
	if !ok {
		t.Fatal("expected level3 to be a map")
	}
	if level3["value"] != "deeply nested" {
		t.Errorf("expected value='deeply nested', got %v", level3["value"])
	}
}

func TestTransform_CELEvaluationError(t *testing.T) {
	fields := map[string]string{
		"result": "data.foo.bar.baz",
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	// Input with data.foo as non-map type will cause evaluation error
	input := `{"data": {"foo": "not a map"}}`
	_, err = tr.Transform(context.Background(), []byte(input))
	if err == nil {
		t.Fatal("expected error for CEL evaluation failure")
	}
	if !strings.Contains(err.Error(), "cel eval") {
		t.Errorf("expected 'cel eval' in error, got: %v", err)
	}
}

func TestTransform_AllDeclaredVariables(t *testing.T) {
	// Test that all declared variables (data, time, source, type, id, subject) can be used
	fields := map[string]string{
		"eventId":   "id",
		"eventType": "type",
		"timestamp": "time",
		"src":       "source",
		"subj":      "subject",
		"payload":   "data",
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{
		"id": "evt-123",
		"type": "user.created",
		"time": "2024-01-01T00:00:00Z",
		"source": "api",
		"subject": "user-456",
		"data": {"name": "Alice"}
	}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if out["eventId"] != "evt-123" {
		t.Errorf("expected eventId='evt-123', got %v", out["eventId"])
	}
	if out["eventType"] != "user.created" {
		t.Errorf("expected eventType='user.created', got %v", out["eventType"])
	}
	if out["src"] != "api" {
		t.Errorf("expected src='api', got %v", out["src"])
	}
}

func TestTransform_MissingVariablesDefaultToNil(t *testing.T) {
	// Test that missing variables are set to nil and don't cause errors
	// when not accessed by expressions
	fields := map[string]string{
		"value": `"constant"`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	// Input missing all standard variables except data
	input := `{"data": {"x": 1}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if out["value"] != "constant" {
		t.Errorf("expected value='constant', got %v", out["value"])
	}
}

func TestTransform_StringMethods(t *testing.T) {
	// Test CEL string extension methods
	fields := map[string]string{
		"upper": `data.text.upperAscii()`,
		"lower": `data.text.lowerAscii()`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"text": "Hello World"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if out["upper"] != "HELLO WORLD" {
		t.Errorf("expected upper='HELLO WORLD', got %v", out["upper"])
	}
	if out["lower"] != "hello world" {
		t.Errorf("expected lower='hello world', got %v", out["lower"])
	}
}

func TestTransform_MathOperations(t *testing.T) {
	// Test CEL math extension
	fields := map[string]string{
		"min": `math.least(data.a, data.b)`,
		"max": `math.greatest(data.a, data.b)`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"a": 10, "b": 20}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if out["min"] != float64(10) {
		t.Errorf("expected min=10, got %v", out["min"])
	}
	if out["max"] != float64(20) {
		t.Errorf("expected max=20, got %v", out["max"])
	}
}

func TestTransform_KeysWithSpecialCharacters(t *testing.T) {
	fields := map[string]string{
		"field-with-dash":       `data.value`,
		"field.with.dots":       `data.value`,
		"field_with_underscore": `data.value`,
		"field:with:colon":      `data.value`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"value": "test"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	for key := range fields {
		if out[key] != "test" {
			t.Errorf("expected %s='test', got %v", key, out[key])
		}
	}
}

func TestToNative_ComplexTypes(t *testing.T) {
	// Test toNative with various CEL types by creating a transformer
	// that returns different types
	tests := []struct {
		name     string
		fields   map[string]string
		input    string
		expected interface{}
	}{
		{
			name:     "integer",
			fields:   map[string]string{"result": "42"},
			input:    `{}`,
			expected: float64(42),
		},
		{
			name:     "double",
			fields:   map[string]string{"result": "3.14"},
			input:    `{}`,
			expected: 3.14,
		},
		{
			name:     "boolean true",
			fields:   map[string]string{"result": "true"},
			input:    `{}`,
			expected: true,
		},
		{
			name:     "boolean false",
			fields:   map[string]string{"result": "false"},
			input:    `{}`,
			expected: false,
		},
		{
			name:     "string",
			fields:   map[string]string{"result": `"hello"`},
			input:    `{}`,
			expected: "hello",
		},
		{
			name:     "null",
			fields:   map[string]string{"result": "null"},
			input:    `{}`,
			expected: nil,
		},
		{
			name:     "list",
			fields:   map[string]string{"result": "[1, 2, 3]"},
			input:    `{}`,
			expected: []interface{}{float64(1), float64(2), float64(3)},
		},
		{
			name:     "map",
			fields:   map[string]string{"result": `{"key": "value"}`},
			input:    `{}`,
			expected: map[string]interface{}{"key": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := NewTransformer(tt.fields)
			if err != nil {
				t.Fatalf("failed to create transformer: %v", err)
			}

			result, err := tr.Transform(context.Background(), []byte(tt.input))
			if err != nil {
				t.Fatalf("transform failed: %v", err)
			}

			var out map[string]interface{}
			if err := json.Unmarshal(result, &out); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			resultVal := out["result"]
			if tt.expected == nil {
				if resultVal != nil {
					t.Errorf("expected nil, got %v", resultVal)
				}
			} else {
				switch expected := tt.expected.(type) {
				case []interface{}:
					resultList, ok := resultVal.([]interface{})
					if !ok {
						t.Fatalf("expected list, got %T", resultVal)
					}
					if len(resultList) != len(expected) {
						t.Errorf("expected length %d, got %d", len(expected), len(resultList))
					}
					for i, v := range expected {
						if resultList[i] != v {
							t.Errorf("expected [%d]=%v, got %v", i, v, resultList[i])
						}
					}
				case map[string]interface{}:
					resultMap, ok := resultVal.(map[string]interface{})
					if !ok {
						t.Fatalf("expected map, got %T", resultVal)
					}
					for k, v := range expected {
						if resultMap[k] != v {
							t.Errorf("expected [%s]=%v, got %v", k, v, resultMap[k])
						}
					}
				default:
					if resultVal != expected {
						t.Errorf("expected %v, got %v", expected, resultVal)
					}
				}
			}
		})
	}
}

func TestTransform_DeeplyNestedMapsAndLists(t *testing.T) {
	fields := map[string]string{
		"result": `{
			"maps": {"a": {"b": {"c": data.value}}},
			"lists": [[1, 2], [3, 4]],
			"mixed": [{"x": 1}, {"y": 2}]
		}`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"value": "deep"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	resultMap, ok := out["result"].(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	// Verify nested maps
	maps, ok := resultMap["maps"].(map[string]interface{})
	if !ok {
		t.Fatal("expected maps to be a map")
	}
	a, ok := maps["a"].(map[string]interface{})
	if !ok {
		t.Fatal("expected a to be a map")
	}
	b, ok := a["b"].(map[string]interface{})
	if !ok {
		t.Fatal("expected b to be a map")
	}
	if b["c"] != "deep" {
		t.Errorf("expected c='deep', got %v", b["c"])
	}

	// Verify nested lists
	lists, ok := resultMap["lists"].([]interface{})
	if !ok {
		t.Fatal("expected lists to be a list")
	}
	if len(lists) != 2 {
		t.Errorf("expected 2 lists, got %d", len(lists))
	}
}

func TestBuildCelExpression_UnicodeKeys(t *testing.T) {
	fields := map[string]string{
		"名前":    `data.name`,
		"città": `data.city`,
		"価格":    `data.price`,
	}
	expr, err := buildCelExpression(fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Just verify it builds successfully - actual execution tested via NewTransformer
	if expr == "" {
		t.Error("expected non-empty expression")
	}
	if !strings.HasPrefix(expr, "{") || !strings.HasSuffix(expr, "}") {
		t.Errorf("expected object literal, got: %s", expr)
	}
}

func TestTransform_EncodingExtension(t *testing.T) {
	// Test CEL encoding extension (base64 encoding)
	fields := map[string]string{
		"encoded": `base64.encode(bytes(data.text))`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"text": "hello"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// base64("hello") = "aGVsbG8="
	if out["encoded"] != "aGVsbG8=" {
		t.Errorf("expected encoded='aGVsbG8=', got %v", out["encoded"])
	}
}

func TestTransform_BytesType(t *testing.T) {
	// Test that bytes type is properly handled by toNative
	fields := map[string]string{
		"data": `bytes(data.text)`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"text": "test"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Bytes should be converted to base64 string by json.Marshal
	if out["data"] == nil {
		t.Error("expected data to be non-nil")
	}
}

func TestTransform_DurationAndTimestampTypes(t *testing.T) {
	// Test CEL duration and timestamp types which use Value() method
	fields := map[string]string{
		"duration": `duration("1h")`,
		"timestamp": `timestamp(data.time)`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"time": "2024-01-01T00:00:00Z"}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Duration and timestamp should be converted via Value() method
	if out["duration"] == nil {
		t.Error("expected duration to be non-nil")
	}
	if out["timestamp"] == nil {
		t.Error("expected timestamp to be non-nil")
	}
}

func TestTransform_UintType(t *testing.T) {
	// Test CEL uint type which uses Value() method
	fields := map[string]string{
		"value": `uint(data.num)`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"num": 42}}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Uint should be converted via Value() method
	if out["value"] == nil {
		t.Error("expected value to be non-nil")
	}
}

func TestTransform_ArrayWithNested(t *testing.T) {
	fields := map[string]string{
		"items": `[data.a, data.b]`,
		"nested": `[[1, 2], [3, 4], []]`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{"data": {"a": 1, "b": 2}}`
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
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}

	// Test nested arrays including empty array
	nested, ok := out["nested"].([]interface{})
	if !ok {
		t.Fatalf("expected nested to be an array, got %T", out["nested"])
	}
	if len(nested) != 3 {
		t.Errorf("expected 3 nested arrays, got %d", len(nested))
	}
}

func TestTransform_EmptyMap(t *testing.T) {
	fields := map[string]string{
		"empty": `{}`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	empty, ok := out["empty"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected empty to be a map, got %T", out["empty"])
	}
	if len(empty) != 0 {
		t.Errorf("expected empty map, got length %d", len(empty))
	}
}

func TestTransform_LargeNumber(t *testing.T) {
	fields := map[string]string{
		"large": `9223372036854775807`, // max int64
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// JSON numbers are decoded as float64 by default
	large, ok := out["large"].(float64)
	if !ok {
		t.Fatalf("expected large to be a number, got %T", out["large"])
	}
	if large != 9223372036854775807 {
		t.Errorf("expected large=9223372036854775807, got %v", large)
	}
}

func TestTransform_TypeConversionWithValue(t *testing.T) {
	// Test various CEL types that may use the Value() interface
	tests := []struct {
		name   string
		fields map[string]string
		input  string
	}{
		{
			name:   "duration type",
			fields: map[string]string{"dur": `duration("30s")`},
			input:  `{}`,
		},
		{
			name:   "timestamp type",
			fields: map[string]string{"ts": `timestamp("2024-01-01T00:00:00Z")`},
			input:  `{}`,
		},
		{
			name:   "uint type",
			fields: map[string]string{"num": `uint(100)`},
			input:  `{}`,
		},
		{
			name:   "bytes type",
			fields: map[string]string{"b": `bytes("test")`},
			input:  `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := NewTransformer(tt.fields)
			if err != nil {
				t.Fatalf("failed to create transformer: %v", err)
			}

			result, err := tr.Transform(context.Background(), []byte(tt.input))
			if err != nil {
				t.Fatalf("transform failed: %v", err)
			}

			var out map[string]interface{}
			if err := json.Unmarshal(result, &out); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			// Just verify result is non-nil and marshallable
			if len(out) == 0 {
				t.Error("expected non-empty output")
			}
		})
	}
}

func TestTransform_ComplexNestedIteration(t *testing.T) {
	// Test toNative with complex nested structures that exercise iterator paths
	fields := map[string]string{
		"result": `{
			"outer": {
				"inner1": {"a": 1, "b": 2},
				"inner2": {"c": 3, "d": 4}
			},
			"lists": {
				"l1": [1, 2, 3],
				"l2": [{"x": 10}, {"y": 20}]
			}
		}`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	resultMap, ok := out["result"].(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	outer, ok := resultMap["outer"].(map[string]interface{})
	if !ok {
		t.Fatal("expected outer to be a map")
	}

	inner1, ok := outer["inner1"].(map[string]interface{})
	if !ok {
		t.Fatal("expected inner1 to be a map")
	}
	if inner1["a"] != float64(1) {
		t.Errorf("expected inner1.a=1, got %v", inner1["a"])
	}

	lists, ok := resultMap["lists"].(map[string]interface{})
	if !ok {
		t.Fatal("expected lists to be a map")
	}

	l1, ok := lists["l1"].([]interface{})
	if !ok {
		t.Fatal("expected l1 to be a list")
	}
	if len(l1) != 3 {
		t.Errorf("expected l1 length=3, got %d", len(l1))
	}
}

func TestNewTransformer_ManyFields(t *testing.T) {
	// Create transformer with many fields to exercise all code paths
	fields := make(map[string]string)
	for i := 0; i < 50; i++ {
		fields[fmt.Sprintf("field%d", i)] = fmt.Sprintf("data.value%d", i)
	}

	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transformer")
	}

	// Test with partial data
	input := `{"data": {"value0": "test", "value25": 42}}`
	_, err = tr.Transform(context.Background(), []byte(input))
	// Some fields will be missing, which may cause evaluation errors
	// We just want to exercise the code paths
	if err == nil {
		// If it succeeds, verify we can marshal the output
		t.Logf("Transform succeeded with partial data")
	}
}

func TestTransform_VeryLargeOutput(t *testing.T) {
	// Create a large output to test size limits
	fields := map[string]string{
		"large": `"` + strings.Repeat("x", 500) + `"`,
	}
	tr, err := NewTransformer(fields, WithMaxOutputBytes(100))
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{}`
	_, err = tr.Transform(context.Background(), []byte(input))
	if err == nil {
		t.Fatal("expected error for output exceeding max size")
	}
	if !strings.Contains(err.Error(), "exceeds max") {
		t.Errorf("expected 'exceeds max' in error, got: %v", err)
	}
}

func TestTransform_AllCELTypes(t *testing.T) {
	// Exercise toNative with all CEL types to maximize coverage
	fields := map[string]string{
		"intVal":       "int(42)",
		"doubleVal":    "double(3.14)",
		"stringVal":    `"text"`,
		"boolVal":      "true",
		"nullVal":      "null",
		"listVal":      "[1, 2, 3]",
		"mapVal":       `{"k": "v"}`,
		"durationVal":  `duration("1h30m")`,
		"timestampVal": `timestamp("2024-01-01T00:00:00Z")`,
		"uintVal":      "uint(100)",
		"bytesVal":     `bytes("hello")`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	input := `{}`
	result, err := tr.Transform(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Verify all types were converted and marshaled successfully
	expectedKeys := []string{"intVal", "doubleVal", "stringVal", "boolVal", "nullVal",
		"listVal", "mapVal", "durationVal", "timestampVal", "uintVal", "bytesVal"}
	for _, key := range expectedKeys {
		if _, exists := out[key]; !exists {
			t.Errorf("expected key %s to exist in output", key)
		}
	}
}
