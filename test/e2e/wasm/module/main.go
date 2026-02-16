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

	if path, ok := stdinFileArg(os.Args); ok {
		input, err = os.ReadFile(path)
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

func stdinFileArg(args []string) (string, bool) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--stdin-file" {
			return args[i+1], true
		}
	}
	return "", false
}
