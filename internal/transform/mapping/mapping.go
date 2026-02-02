package mapping

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lsm/fiso/internal/jsonpath"
)

// Transformer applies a YAML mapping definition to JSON event payloads.
// Values starting with "$." are resolved as JSONPath expressions against the input.
// Nested maps produce nested output objects. All other values are treated as literals.
type Transformer struct {
	mapping map[string]interface{}
}

// NewTransformer creates a mapping transformer from a mapping definition.
// Returns an error if the mapping is empty.
func NewTransformer(mapping map[string]interface{}) (*Transformer, error) {
	if len(mapping) == 0 {
		return nil, fmt.Errorf("mapping cannot be empty")
	}
	return &Transformer{mapping: mapping}, nil
}

// Transform applies the mapping to the input JSON payload.
func (t *Transformer) Transform(ctx context.Context, input []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context error: %w", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal input: %w", err)
	}

	result, err := jsonpath.ResolveMap(parsed, t.mapping)
	if err != nil {
		return nil, fmt.Errorf("mapping transform: %w", err)
	}

	output, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal output: %w", err)
	}
	return output, nil
}
