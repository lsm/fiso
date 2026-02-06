package unified

import (
	"context"
	"testing"
)

// BenchmarkSimpleFieldMapping benchmarks a simple field mapping scenario.
// This should be significantly faster than the old CEL implementation due to
// removal of per-event goroutine overhead.
func BenchmarkSimpleFieldMapping(b *testing.B) {
	fields := map[string]string{
		"order_id": "data.legacy_id",
		"ts":       "data.timestamp",
		"status":   `"processed"`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte(`{
		"data": {
			"legacy_id": "order-12345",
			"timestamp": "2024-01-15T10:30:00Z",
			"customer_id": "cust-987",
			"items": ["item1", "item2"],
			"total": 149.99
		}
	}`)

	ctx := context.Background()

	// Warm up
	_, _ = tr.Transform(ctx, input)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = tr.Transform(ctx, input)
	}
}

// BenchmarkWithComputation benchmarks CEL with arithmetic and conditional logic.
func BenchmarkWithComputation(b *testing.B) {
	fields := map[string]string{
		"total":      `data.price * data.quantity`,
		"discounted": `data.price * data.quantity * (data.has_discount ? 0.9 : 1.0)`,
		"category":   `data.type == "premium" ? "gold" : "standard"`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte(`{
		"data": {
			"price": 99.99,
			"quantity": 3,
			"has_discount": true,
			"type": "premium"
		}
	}`)

	ctx := context.Background()

	// Warm up
	_, _ = tr.Transform(ctx, input)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = tr.Transform(ctx, input)
	}
}

// BenchmarkWithNestedStructure benchmarks CEL with nested output structure.
func BenchmarkWithNestedStructure(b *testing.B) {
	fields := map[string]string{
		"order":    `{"id": data.order_id, "total": data.subtotal + data.tax}`,
		"customer": `{"id": data.customer.id, "name": data.customer.name}`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte(`{
		"data": {
			"order_id": "ord-456",
			"subtotal": 100.0,
			"tax": 8.5,
			"customer": {
				"id": "cust-789",
				"name": "John Doe"
			}
		}
	}`)

	ctx := context.Background()

	// Warm up
	_, _ = tr.Transform(ctx, input)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = tr.Transform(ctx, input)
	}
}

// BenchmarkStringOperations benchmarks CEL with string manipulation.
func BenchmarkStringOperations(b *testing.B) {
	fields := map[string]string{
		"full_name": `data.first_name + " " + data.last_name`,
		"email":     `data.username + "@" + data.domain`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte(`{
		"data": {
			"first_name": "Jane",
			"last_name": "Smith",
			"username": "jane.smith",
			"domain": "example.com"
		}
	}`)

	ctx := context.Background()

	// Warm up
	_, _ = tr.Transform(ctx, input)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = tr.Transform(ctx, input)
	}
}

// BenchmarkComplexExpression benchmarks a complex CEL expression with multiple operations.
func BenchmarkComplexExpression(b *testing.B) {
	fields := map[string]string{
		"result": `{
			"score": data.base_score * data.multiplier + data.bonus,
			"tier": data.base_score > 1000 ? "platinum" : data.base_score > 500 ? "gold" : "silver",
			"eligible": data.age >= 18 && data.verified == true,
			"discount": data.member ? 0.15 : 0.0
		}`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte(`{
		"data": {
			"base_score": 1250,
			"multiplier": 1.5,
			"bonus": 100,
			"age": 28,
			"verified": true,
			"member": true
		}
	}`)

	ctx := context.Background()

	// Warm up
	_, _ = tr.Transform(ctx, input)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = tr.Transform(ctx, input)
	}
}

// BenchmarkLargePayload benchmarks CEL with a large realistic event payload.
func BenchmarkLargePayload(b *testing.B) {
	fields := map[string]string{
		"event_id":   "data.id",
		"event_type": "data.type",
		"timestamp":  "data.timestamp",
		"user":       `{"id": data.user_id, "segment": data.segment}`,
		"metrics":    `{"total": data.metrics.page_views + data.metrics.events, "unique": data.metrics.unique_visitors}`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte(`{
		"data": {
			"id": "evt-a1b2c3d4",
			"type": "page_view",
			"timestamp": "2024-01-15T10:30:00Z",
			"user_id": "user-12345",
			"segment": "premium",
			"metrics": {
				"page_views": 15420,
				"events": 8932,
				"unique_visitors": 3421,
				"bounce_rate": 0.32
			},
			"properties": {
				"page": "/checkout",
				"referrer": "https://google.com"
			},
			"tech": {
				"browser": "Chrome",
				"os": "macOS",
				"device": "desktop"
			}
		}
	}`)

	ctx := context.Background()

	// Warm up
	_, _ = tr.Transform(ctx, input)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = tr.Transform(ctx, input)
	}
}

// BenchmarkPassthrough benchmarks simple passthrough of the data field.
func BenchmarkPassthrough(b *testing.B) {
	fields := map[string]string{
		"result": "data",
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte(`{
		"data": {
			"id": "123",
			"name": "test",
			"value": 42
		}
	}`)

	ctx := context.Background()

	// Warm up
	_, _ = tr.Transform(ctx, input)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = tr.Transform(ctx, input)
	}
}

// BenchmarkListConstruction benchmarks creating a list from data.
func BenchmarkListConstruction(b *testing.B) {
	fields := map[string]string{
		"items": `[data.id, data.name, data.email, data.status]`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte(`{
		"data": {
			"id": "user-456",
			"name": "Alice Johnson",
			"email": "alice@example.com",
			"status": "active"
		}
	}`)

	ctx := context.Background()

	// Warm up
	_, _ = tr.Transform(ctx, input)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = tr.Transform(ctx, input)
	}
}

// BenchmarkMultipleFields benchmarks mapping with multiple field extractions.
func BenchmarkMultipleFields(b *testing.B) {
	fields := map[string]string{
		"id":         "id",
		"type":       "type",
		"timestamp":  "time",
		"user_id":    "data.user.id",
		"user_name":  "data.user.name",
		"user_email": "data.user.email",
		"status":     `"processed"`,
		"version":    `"1.0"`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte(`{
		"id": "evt-789",
		"type": "user_action",
		"time": "2024-01-15T10:30:00Z",
		"data": {
			"user": {
				"id": "user-123",
				"name": "John Doe",
				"email": "john@example.com"
			}
		}
	}`)

	ctx := context.Background()

	// Warm up
	_, _ = tr.Transform(ctx, input)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = tr.Transform(ctx, input)
	}
}

// BenchmarkDeepNesting benchmarks mapping with deeply nested structures.
func BenchmarkDeepNesting(b *testing.B) {
	fields := map[string]string{
		"level1": `{"level2": {"level3": {"value": data.deep.nested.value}}}`,
	}
	tr, err := NewTransformer(fields)
	if err != nil {
		b.Fatal(err)
	}

	input := []byte(`{
		"data": {
			"deep": {
				"nested": {
					"value": "found-it"
				}
			}
		}
	}`)

	ctx := context.Background()

	// Warm up
	_, _ = tr.Transform(ctx, input)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = tr.Transform(ctx, input)
	}
}
