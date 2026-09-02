package wasm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lsm/fiso/internal/interceptor"
)

// Runtime abstracts the WASM runtime for testability.
// In production this would be backed by wazero or similar.
type Runtime interface {
	// Call invokes the WASM module's process function with JSON input, returns JSON output.
	Call(ctx context.Context, input []byte) ([]byte, error)
	Close() error
}

// Interceptor runs a WASM module to process requests.
// Uses a JSON-in/JSON-out ABI: the module receives and returns JSON with
// {payload, headers, direction} structure.
type Interceptor struct {
	runtime    Runtime
	moduleName string
}

// New creates a new WASM interceptor.
func New(runtime Runtime, moduleName string) *Interceptor {
	return &Interceptor{runtime: runtime, moduleName: moduleName}
}

type wasmInput struct {
	Payload   json.RawMessage   `json:"payload"`
	Headers   map[string]string `json:"headers"`
	Direction string            `json:"direction"`
}

type wasmOutput struct {
	Payload json.RawMessage   `json:"payload"`
	Headers map[string]string `json:"headers"`
}

// Process invokes the WASM module to process the request.
// ProcessWithEnv is Process with environment variables set for the guest
// invocation (used by tests to select guest code paths).
func (i *Interceptor) ProcessWithEnv(ctx context.Context, req *interceptor.Request, env map[string]string) (*interceptor.Request, error) {
	runtime, ok := i.runtime.(interface {
		CallWithEnv(context.Context, []byte, map[string]string) ([]byte, error)
	})
	if !ok {
		resp, err := i.Process(ctx, req)
		return resp, err
	}
	input := wasmInput{Payload: req.Payload, Headers: req.Headers, Direction: string(req.Direction)}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("wasm marshal input: %w", err)
	}
	result, err := runtime.CallWithEnv(ctx, data, env)
	if err != nil {
		return nil, fmt.Errorf("wasm module %s: %w", i.moduleName, err)
	}
	var output wasmOutput
	if err := json.Unmarshal(result, &output); err != nil {
		return nil, fmt.Errorf("wasm unmarshal output from %s: %w", i.moduleName, err)
	}
	return &interceptor.Request{Payload: output.Payload, Headers: output.Headers, Direction: req.Direction}, nil
}

func (i *Interceptor) Process(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
	input := wasmInput{
		Payload:   req.Payload,
		Headers:   req.Headers,
		Direction: string(req.Direction),
	}

	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("wasm marshal input: %w", err)
	}

	result, err := i.runtime.Call(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("wasm module %s: %w", i.moduleName, err)
	}

	var output wasmOutput
	if err := json.Unmarshal(result, &output); err != nil {
		return nil, fmt.Errorf("wasm unmarshal output from %s: %w", i.moduleName, err)
	}

	return &interceptor.Request{
		Payload:   output.Payload,
		Headers:   output.Headers,
		Direction: req.Direction,
	}, nil
}

// Close releases the WASM runtime resources.
func (i *Interceptor) Close() error {
	return i.runtime.Close()
}
