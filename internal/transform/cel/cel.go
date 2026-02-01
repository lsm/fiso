package cel

import (
	"context"
	"encoding/json"
	"fmt"
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

// Transformer applies a CEL expression to JSON event payloads.
type Transformer struct {
	program        cel.Program
	timeout        time.Duration
	maxOutputBytes int
}

// NewTransformer compiles a CEL expression and returns a ready-to-use Transformer.
// The expression receives the parsed JSON input as top-level variables.
// For example, given input {"data": {"id": 1}}, the expression can reference data.id.
func NewTransformer(expression string, opts ...Option) (*Transformer, error) {
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

	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("cel compile: %w", issues.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("cel program: %w", err)
	}

	t := &Transformer{
		program:        prg,
		timeout:        defaultTimeout,
		maxOutputBytes: defaultMaxOutputBytes,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t, nil
}

// Transform applies the CEL expression to the input JSON payload.
func (t *Transformer) Transform(ctx context.Context, input []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context error: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	var parsed map[string]interface{}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal input: %w", err)
	}

	activation := make(map[string]interface{})
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

	type result struct {
		val interface{}
		err error
	}
	ch := make(chan result, 1)

	go func() {
		out, _, err := t.program.Eval(activation)
		if err != nil {
			ch <- result{err: fmt.Errorf("cel eval: %w", err)}
			return
		}
		ch <- result{val: toNative(out)}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("transform timeout: %w", ctx.Err())
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}

		output, err := json.Marshal(r.val)
		if err != nil {
			return nil, fmt.Errorf("marshal output: %w", err)
		}

		if len(output) > t.maxOutputBytes {
			return nil, fmt.Errorf("output size %d exceeds max %d bytes", len(output), t.maxOutputBytes)
		}

		return output, nil
	}
}

// toNative recursively converts CEL ref.Val types to native Go types
// that json.Marshal can handle.
func toNative(val interface{}) interface{} {
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
