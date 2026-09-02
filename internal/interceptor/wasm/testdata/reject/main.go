package main

import (
	"encoding/json"
	"io"
	"os"
)

// Test module for the rejection ABI (ADR 0007): it refuses any request whose
// headers carry no Authorization, answering with the reject object; an
// authorized request passes through enriched. This is the shape of a real
// guest-side authentication module.

type wasmInput struct {
	Payload   json.RawMessage   `json:"payload"`
	Headers   map[string]string `json:"headers"`
	Direction string            `json:"direction"`
}

type wasmOutput struct {
	Payload interface{}       `json:"payload"`
	Headers map[string]string `json:"headers"`
	Reject  *wasmRejection    `json:"reject,omitempty"`
}

type wasmRejection struct {
	Status int    `json:"status"`
	Reason string `json:"reason"`
}

func main() {
	// The two engines deliver input differently: wazero pipes JSON to
	// stdin; wasmer writes it to a file passed as --stdin-file. Support
	// both so the same module runs under either engine. Args are parsed
	// manually: the flag package exits(2) on any unexpected argument,
	// which engines may pass.
	var (
		input    []byte
		err      error
		stdinArg string
	)
	for i := 1; i+1 < len(os.Args); i++ {
		if os.Args[i] == "--stdin-file" {
			stdinArg = os.Args[i+1]
		}
	}
	if stdinArg != "" {
		input, err = os.ReadFile(stdinArg)
	} else {
		input, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		os.Exit(1)
	}

	var req wasmInput
	if err := json.Unmarshal(input, &req); err != nil {
		os.Exit(1)
	}

	auth, authorized := req.Headers["Authorization"]
	if !authorized || auth == "" {
		_ = json.NewEncoder(os.Stdout).Encode(wasmOutput{
			Reject: &wasmRejection{Status: 401, Reason: "missing credentials"},
		})
		return
	}

	var data map[string]interface{}
	if err := json.Unmarshal(req.Payload, &data); err != nil {
		os.Exit(1)
	}
	// A bodyless request arrives as a null payload; tolerate it.
	if data == nil {
		data = map[string]interface{}{}
	}
	data["authenticated"] = true

	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	req.Headers["X-Authenticated"] = "true"

	_ = json.NewEncoder(os.Stdout).Encode(wasmOutput{
		Payload: data,
		Headers: req.Headers,
	})
}
