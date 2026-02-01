package schema

import (
	"encoding/json"
	"fmt"
)

// Codec decodes wire-format messages and validates them against a schema.
type Codec interface {
	// Decode converts a wire-format message to JSON.
	Decode(data []byte) ([]byte, error)
	// Validate checks that data conforms to the schema.
	Validate(data []byte) error
}

// JSONCodec validates JSON data against a JSON Schema.
type JSONCodec struct {
	schema map[string]interface{}
}

// NewJSONCodec creates a JSON codec from a JSON Schema string.
func NewJSONCodec(schemaStr string) (*JSONCodec, error) {
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(schemaStr), &schema); err != nil {
		return nil, fmt.Errorf("parse JSON schema: %w", err)
	}
	return &JSONCodec{schema: schema}, nil
}

// Decode for JSON is a no-op passthrough — data is already JSON.
func (c *JSONCodec) Decode(data []byte) ([]byte, error) {
	if !json.Valid(data) {
		return nil, fmt.Errorf("invalid JSON data")
	}
	return data, nil
}

// Validate checks that the JSON data has the required fields defined in the schema.
func (c *JSONCodec) Validate(data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("invalid JSON data")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("unmarshal JSON for validation: %w", err)
	}

	// Check required fields if defined in schema
	if required, ok := c.schema["required"]; ok {
		if reqList, ok := required.([]interface{}); ok {
			for _, req := range reqList {
				fieldName, ok := req.(string)
				if !ok {
					continue
				}
				if _, exists := parsed[fieldName]; !exists {
					return fmt.Errorf("missing required field: %s", fieldName)
				}
			}
		}
	}

	// Check type constraints on properties
	properties, _ := c.schema["properties"].(map[string]interface{})
	if properties != nil {
		for key, val := range parsed {
			propSchema, ok := properties[key].(map[string]interface{})
			if !ok {
				continue
			}
			expectedType, _ := propSchema["type"].(string)
			if expectedType == "" {
				continue
			}
			if err := checkType(expectedType, val); err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
		}
	}

	return nil
}

func checkType(expected string, val interface{}) error {
	switch expected {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
	case "number":
		if _, ok := val.(float64); !ok {
			return fmt.Errorf("expected number, got %T", val)
		}
	case "integer":
		f, ok := val.(float64)
		if !ok {
			return fmt.Errorf("expected integer, got %T", val)
		}
		if f != float64(int64(f)) {
			return fmt.Errorf("expected integer, got float")
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", val)
		}
	case "object":
		if _, ok := val.(map[string]interface{}); !ok {
			return fmt.Errorf("expected object, got %T", val)
		}
	case "array":
		if _, ok := val.([]interface{}); !ok {
			return fmt.Errorf("expected array, got %T", val)
		}
	}
	return nil
}
