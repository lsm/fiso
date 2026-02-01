package schema

import (
	"strings"
	"testing"
)

func TestJSONCodec_Decode_Valid(t *testing.T) {
	c, err := NewJSONCodec(`{"type":"object"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, err := c.Decode([]byte(`{"name":"test"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != `{"name":"test"}` {
		t.Errorf("unexpected output: %s", string(out))
	}
}

func TestJSONCodec_Decode_Invalid(t *testing.T) {
	c, _ := NewJSONCodec(`{"type":"object"}`)
	_, err := c.Decode([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestJSONCodec_Validate_RequiredFields(t *testing.T) {
	schema := `{
		"type": "object",
		"required": ["name", "age"],
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "number"}
		}
	}`
	c, err := NewJSONCodec(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Valid
	err = c.Validate([]byte(`{"name":"Alice","age":30}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Missing required field
	err = c.Validate([]byte(`{"name":"Alice"}`))
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	if !strings.Contains(err.Error(), "age") {
		t.Errorf("expected error about 'age', got %q", err.Error())
	}
}

func TestJSONCodec_Validate_TypeChecking(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"count": {"type": "integer"},
			"active": {"type": "boolean"}
		}
	}`
	c, _ := NewJSONCodec(schema)

	// Valid types
	err := c.Validate([]byte(`{"name":"test","count":5,"active":true}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wrong type for name
	err = c.Validate([]byte(`{"name":123}`))
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
	if !strings.Contains(err.Error(), "string") {
		t.Errorf("expected type error, got %q", err.Error())
	}

	// Float where integer expected
	err = c.Validate([]byte(`{"count":1.5}`))
	if err == nil {
		t.Fatal("expected error for float where integer expected")
	}
}

func TestJSONCodec_Validate_InvalidJSON(t *testing.T) {
	c, _ := NewJSONCodec(`{"type":"object"}`)
	err := c.Validate([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestJSONCodec_Validate_NoConstraints(t *testing.T) {
	c, _ := NewJSONCodec(`{"type":"object"}`)
	err := c.Validate([]byte(`{"anything":"goes"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewJSONCodec_InvalidSchema(t *testing.T) {
	_, err := NewJSONCodec("not json")
	if err == nil {
		t.Fatal("expected error for invalid schema JSON")
	}
}

func TestJSONCodec_Validate_ArrayType(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"items": {"type": "array"},
			"meta": {"type": "object"}
		}
	}`
	c, _ := NewJSONCodec(schema)

	err := c.Validate([]byte(`{"items":[1,2,3],"meta":{"key":"val"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = c.Validate([]byte(`{"items":"not-array"}`))
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestJSONCodec_Validate_BooleanType(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"active": {"type": "boolean"}
		}
	}`
	c, _ := NewJSONCodec(schema)

	err := c.Validate([]byte(`{"active":"yes"}`))
	if err == nil {
		t.Fatal("expected error for string instead of boolean")
	}
}

func TestJSONCodec_Validate_NumberType(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"price": {"type": "number"}
		}
	}`
	c, _ := NewJSONCodec(schema)

	// Valid number
	err := c.Validate([]byte(`{"price":19.99}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wrong type
	err = c.Validate([]byte(`{"price":"free"}`))
	if err == nil {
		t.Fatal("expected error for string instead of number")
	}
	if !strings.Contains(err.Error(), "number") {
		t.Errorf("expected number type error, got %q", err.Error())
	}
}

func TestJSONCodec_Validate_ObjectType(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"data": {"type": "object"}
		}
	}`
	c, _ := NewJSONCodec(schema)

	err := c.Validate([]byte(`{"data":"not-object"}`))
	if err == nil {
		t.Fatal("expected error for string instead of object")
	}
	if !strings.Contains(err.Error(), "object") {
		t.Errorf("expected object type error, got %q", err.Error())
	}
}

func TestJSONCodec_Validate_UnknownProperty(t *testing.T) {
	// Properties not in schema are silently allowed
	schema := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		}
	}`
	c, _ := NewJSONCodec(schema)

	err := c.Validate([]byte(`{"name":"test","extra":123}`))
	if err != nil {
		t.Fatalf("unexpected error for extra property: %v", err)
	}
}

func TestJSONCodec_Validate_PropertyNoType(t *testing.T) {
	// Property with no type constraint should pass
	schema := `{
		"type": "object",
		"properties": {
			"name": {"description": "a name field"}
		}
	}`
	c, _ := NewJSONCodec(schema)

	err := c.Validate([]byte(`{"name":123}`))
	if err != nil {
		t.Fatalf("unexpected error for property without type: %v", err)
	}
}

func TestJSONCodec_Validate_IntegerValid(t *testing.T) {
	schema := `{
		"type": "object",
		"properties": {
			"count": {"type": "integer"}
		}
	}`
	c, _ := NewJSONCodec(schema)

	// Integer value (represented as float64 in JSON)
	err := c.Validate([]byte(`{"count":42}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-number value
	err = c.Validate([]byte(`{"count":"42"}`))
	if err == nil {
		t.Fatal("expected error for string where integer expected")
	}
}
