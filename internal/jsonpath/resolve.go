package jsonpath

import (
	"fmt"
	"strings"
)

// Resolve resolves a dot-notation path against parsed JSON data.
// The path should not include the "$." prefix (pass "field.nested", not "$.field.nested").
// Returns the resolved value or an error if the path cannot be traversed.
func Resolve(data map[string]interface{}, path string) (interface{}, error) {
	parts := strings.Split(path, ".")
	var current interface{} = data
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("path %q: cannot traverse into non-object at %q", path, part)
		}
		current, ok = m[part]
		if !ok {
			return nil, fmt.Errorf("path %q: field %q not found", path, part)
		}
	}
	return current, nil
}

// ResolveString resolves an expression against parsed JSON data.
// If the expression starts with "$.", it is treated as a JSONPath and resolved.
// Otherwise, the expression is returned as a literal string.
// On resolution errors, the raw expression is returned as a fallback.
func ResolveString(data map[string]interface{}, expr string) string {
	if !strings.HasPrefix(expr, "$.") {
		return expr
	}
	val, err := Resolve(data, expr[2:])
	if err != nil {
		return expr
	}
	return fmt.Sprintf("%v", val)
}

// ResolveValue resolves a value from a mapping definition against input data.
// If the value is a string starting with "$.", it resolves via JSONPath.
// If the value is a nested map, it recursively resolves all entries.
// Otherwise, the value is returned as-is (literal).
func ResolveValue(input map[string]interface{}, value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		if strings.HasPrefix(v, "$.") {
			return Resolve(input, v[2:])
		}
		return v, nil
	case map[string]interface{}:
		return ResolveMap(input, v)
	default:
		return v, nil
	}
}

// ResolveMap resolves all values in a mapping definition against input data.
func ResolveMap(input map[string]interface{}, mapping map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{}, len(mapping))
	for key, value := range mapping {
		resolved, err := ResolveValue(input, value)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", key, err)
		}
		result[key] = resolved
	}
	return result, nil
}
