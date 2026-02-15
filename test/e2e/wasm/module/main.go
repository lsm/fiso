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
	var input []byte
	var err error

	if len(os.Args) > 2 && os.Args[1] == "--stdin-file" {
		input, err = os.ReadFile(os.Args[2])
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

	// Enrich: add wasm_enriched field to payload
	var data map[string]interface{}
	if err := json.Unmarshal(req.Payload, &data); err != nil {
		os.Exit(1)
	}
	data["wasm_enriched"] = true

	// Add header
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	req.Headers["X-WASM-Processed"] = "true"

	output := wasmOutput{
		Payload: data,
		Headers: req.Headers,
	}

	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		os.Exit(1)
	}
}
