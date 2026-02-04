package main

import (
	"encoding/json"
	"io"
	"os"
)

type wasmInput struct {
	Payload   json.RawMessage   `json:"payload"`
	Headers   map[string]string `json:"headers"`
	Direction string            `json:"direction"`
}

type wasmOutput struct {
	Payload interface{}       `json:"payload"`
	Headers map[string]string `json:"headers"`
}

func main() {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(1)
	}

	var req wasmInput
	if err := json.Unmarshal(input, &req); err != nil {
		os.Exit(1)
	}

	// Parse the payload
	var data map[string]interface{}
	if err := json.Unmarshal(req.Payload, &data); err != nil {
		os.Exit(1)
	}

	// Return empty output (no stdout)
	// This simulates a WASM module that produces no output
}
