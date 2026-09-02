package wasm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/lsm/fiso/internal/interceptor"
)

// wrapMode records how a non-JSON payload was packaged into the envelope so
// the module's output can be unwrapped symmetrically (ADR 0007).
type wrapMode int

const (
	wrapNone wrapMode = iota
	wrapString
	wrapBase64
)

// wasmB64Payload is the lossless carrier for binary payloads: JSON strings
// cannot hold invalid UTF-8 without corruption, so arbitrary bytes travel
// base64-encoded inside a {"fisoB64": "..."} object instead.
type wasmB64Payload struct {
	B64 string `json:"fisoB64"`
}

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

// wasmRejection is the guest's refusal verdict in the output ABI (ADR 0007).
type wasmRejection struct {
	Status int    `json:"status"`
	Reason string `json:"reason"`
}

type wasmOutput struct {
	Payload json.RawMessage   `json:"payload"`
	Headers map[string]string `json:"headers"`
	Reject  *wasmRejection    `json:"reject,omitempty"`
}

// Process invokes the WASM module to process the request.
func (i *Interceptor) Process(ctx context.Context, req *interceptor.Request) (*interceptor.Request, error) {
	payload := req.Payload
	wrapMode := wrapNone
	if len(payload) == 0 {
		// A bodyless request (e.g. a GET through Link) arrives with an
		// empty payload; an empty json.RawMessage does not marshal, so the
		// envelope carries an explicit null instead (ADR 0007).
		payload = json.RawMessage("null")
	} else if !json.Valid(payload) {
		// Non-JSON bodies travel losslessly: valid UTF-8 text as a JSON
		// string, arbitrary bytes base64-encoded inside a {"fisoB64":...}
		// object (JSON strings cannot carry invalid UTF-8 without
		// corruption). A module returning the wrapper unchanged restores
		// the original bytes (ADR 0007).
		if utf8.Valid(payload) {
			wrapMode = wrapString
			if b, err := json.Marshal(string(payload)); err == nil {
				payload = b
			}
		} else {
			wrapMode = wrapBase64
			b, err := json.Marshal(wasmB64Payload{B64: base64.StdEncoding.EncodeToString(payload)})
			if err == nil {
				payload = b
			}
		}
	}
	input := wasmInput{
		Payload:   payload,
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

	// A rejection is a deliberate refusal (ADR 0007): surface it as the
	// typed error so the pipeline and proxies answer with the guest-chosen
	// status instead of treating the event as failed. Only caller-facing
	// error statuses (400–599) are valid refusals; anything else is a
	// contract violation and follows the failure path.
	if output.Reject != nil {
		if output.Reject.Status < 400 || output.Reject.Status > 599 {
			return nil, fmt.Errorf("wasm module %s: reject.status %d is outside 400-599",
				i.moduleName, output.Reject.Status)
		}
		return nil, &interceptor.RejectedError{
			Status: output.Reject.Status,
			Reason: output.Reject.Reason,
		}
	}

	// A null payload round-trips as an empty one: a guest that passes the
	// (bodyless request's) null payload through unchanged must not turn the
	// request into a literal four-byte "null" body (ADR 0007).
	if string(output.Payload) == "null" {
		output.Payload = nil
	}
	// Symmetrically unwrap: a guest returning the wrapper unchanged
	// restores the original non-JSON bytes, losslessly for binary too.
	switch wrapMode {
	case wrapString:
		var s string
		if err := json.Unmarshal(output.Payload, &s); err == nil {
			output.Payload = []byte(s)
		}
	case wrapBase64:
		// Field presence (not non-emptiness) selects the unwrap: a guest
		// that deliberately empties a binary payload returns
		// {"fisoB64":""}, which decodes to zero bytes.
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(output.Payload, &probe); err == nil {
			if rawField, present := probe["fisoB64"]; present {
				var b64 string
				if err := json.Unmarshal(rawField, &b64); err == nil {
					if raw, err := base64.StdEncoding.DecodeString(b64); err == nil {
						output.Payload = raw
					}
				}
			}
		}
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
