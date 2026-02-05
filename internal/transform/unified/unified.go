package unified

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/ext"
)

const (
	defaultTimeout        = 5 * time.Second
	defaultMaxOutputBytes = 1 << 20 // 1MB
)

// Option configures a Transformer.
type Option func(*Transformer)

// WithTimeout sets the maximum execution time for a single transform.
func WithTimeout(d time.Duration) Option {
	return func(t *Transformer) {
		t.timeout = d
	}
}

// WithMaxOutputBytes sets the maximum size of the transform output in bytes.
func WithMaxOutputBytes(n int) Option {
	return func(t *Transformer) {
		t.maxOutputBytes = n
	}
}

// Transformer applies a unified fields-based transform to JSON event payloads.
// The fields map defines output fields as CEL expressions, compiled into a single
// optimized CEL program for better performance than per-event goroutine evaluation.
type Transformer struct {
	program        cel.Program
	fields         map[string]string
	timeout        time.Duration
	maxOutputBytes int
}

// NewTransformer creates a unified transformer from a fields map.
// The fields map specifies output field names and their CEL expressions.
// For example: {"order_id": "data.legacy_id", "total": "data.price * data.quantity"}
func NewTransformer(fields map[string]string, opts ...Option) (*Transformer, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("fields cannot be empty")
	}

	// Build CEL expression from fields map
	expr, err := buildCelExpression(fields)
	if err != nil {
		return nil, fmt.Errorf("build cel expression: %w", err)
	}

	// Create CEL environment with standard extensions
	env, err := cel.NewEnv(
		cel.Variable("data", cel.DynType),
		cel.Variable("time", cel.DynType),
		cel.Variable("source", cel.DynType),
		cel.Variable("type", cel.DynType),
		cel.Variable("id", cel.DynType),
		cel.Variable("subject", cel.DynType),
		ext.Strings(),
		ext.Encoders(),
		ext.Math(),
	)
	if err != nil {
		return nil, fmt.Errorf("cel env: %w", err)
	}

	// Compile the expression
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("cel compile: %w", issues.Err())
	}

	// Create the program
	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("cel program: %w", err)
	}

	t := &Transformer{
		program:        prg,
		fields:         fields,
		timeout:        defaultTimeout,
		maxOutputBytes: defaultMaxOutputBytes,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

// buildCelExpression converts a fields map to a CEL object literal expression.
// For example: {"a": "data.x", "b": "42"} becomes {"a": data.x, "b": 42}
func buildCelExpression(fields map[string]string) (string, error) {
	if len(fields) == 0 {
		return "", fmt.Errorf("fields map is empty")
	}

	var builder strings.Builder
	builder.Grow(128) // Pre-allocate reasonable size

	builder.WriteByte('{')

	first := true
	for key, expr := range fields {
		if !first {
			builder.WriteByte(',')
		}
		first = false

		// Write key as JSON string (handles escaping)
		jsonKey, _ := json.Marshal(key)
		builder.Write(jsonKey)
		builder.WriteByte(':')
		builder.WriteString(expr)
	}

	builder.WriteByte('}')

	return builder.String(), nil
}

// Transform applies the unified transform to the input JSON payload.
// It uses direct CEL evaluation without per-event goroutines for better performance.
func (t *Transformer) Transform(ctx context.Context, input []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context error: %w", err)
	}

	// Parse input JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal input: %w", err)
	}

	// Build activation with all input fields
	activation := make(map[string]interface{}, len(parsed)+6)
	for k, v := range parsed {
		activation[k] = v
	}

	// Ensure all declared variables have a value to avoid runtime errors
	// for expressions that don't reference all variables.
	for _, name := range []string{"data", "time", "source", "type", "id", "subject"} {
		if _, ok := activation[name]; !ok {
			activation[name] = nil
		}
	}

	// Direct CEL evaluation without goroutine (key performance optimization)
	out, _, err := t.program.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("cel eval: %w", err)
	}

	// Convert CEL types to native Go types
	nativeVal := toNative(out)

	// Marshal output
	output, err := json.Marshal(nativeVal)
	if err != nil {
		return nil, fmt.Errorf("marshal output: %w", err)
	}

	// Check output size limit
	if len(output) > t.maxOutputBytes {
		return nil, fmt.Errorf("output size %d exceeds max %d bytes", len(output), t.maxOutputBytes)
	}

	return output, nil
}

// toNative recursively converts CEL ref.Val types to native Go types
// that json.Marshal can handle.
func toNative(val interface{}) interface{} {
	// Handle null values first - return Go nil
	if _, ok := val.(types.Null); ok {
		return nil
	}

	switch v := val.(type) {
	case traits.Mapper:
		it := v.Iterator()
		m := make(map[string]interface{})
		for it.HasNext() == types.True {
			key := it.Next()
			value := v.Get(key)
			m[fmt.Sprint(key.Value())] = toNative(value)
		}
		return m
	case traits.Lister:
		it := v.Iterator()
		var list []interface{}
		for it.HasNext() == types.True {
			elem := it.Next()
			list = append(list, toNative(elem))
		}
		return list
	default:
		if rv, ok := val.(types.Int); ok {
			return int64(rv)
		}
		if rv, ok := val.(types.Double); ok {
			return float64(rv)
		}
		if rv, ok := val.(types.String); ok {
			return string(rv)
		}
		if rv, ok := val.(types.Bool); ok {
			return bool(rv)
		}
		// Fall back to Value() for other ref.Val types
		if rv, ok := val.(interface{ Value() interface{} }); ok {
			return rv.Value()
		}
		return val
	}
}
