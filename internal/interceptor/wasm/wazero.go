package wasm

import (
	"context"

	wasmruntime "github.com/lsm/fiso/internal/wasm"
)

// WazeroRuntime delegates to the shared wazero runtime in internal/wasm.
type WazeroRuntime = wasmruntime.WazeroRuntime

// HostHTTPConfig configures the fiso.http_call host function (ADR 0006).
type HostHTTPConfig = wasmruntime.HostHTTPConfig

// Host call result codes (see internal/wasm/hosthttp.go).
const (
	HostErrInvalidRequest = wasmruntime.HostErrInvalidRequest
	HostErrTargetDenied   = wasmruntime.HostErrTargetDenied
	HostErrBufferSize     = wasmruntime.HostErrBufferSize
	HostErrUpstream       = wasmruntime.HostErrUpstream
)

// NewWazeroRuntime compiles a WASM module from raw bytes.
// The module must be a WASI binary (wasip1) that reads JSON from stdin and writes JSON to stdout.
func NewWazeroRuntime(ctx context.Context, wasmBytes []byte) (*WazeroRuntime, error) {
	return wasmruntime.NewWazeroRuntime(ctx, wasmBytes)
}
