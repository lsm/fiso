package main

import (
	"encoding/json"
	"io"
	"os"
	"time"
)

type WasmInput struct {
	Payload   json.RawMessage   `json:"payload"`
	Headers   map[string]string `json:"headers"`
	Direction string            `json:"direction"`
}

type WasmOutput struct {
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

	var req WasmInput
	if err := json.Unmarshal(input, &req); err != nil {
		os.Exit(1)
	}

	// Parse the original payload
	var data map[string]interface{}
	if err := json.Unmarshal(req.Payload, &data); err != nil {
		os.Exit(1)
	}

	// Enrich with Wasmer-specific markers
	data["wasmer_enriched"] = true
	data["runtime"] = "wasmer"
	data["processed_at"] = time.Now().UTC().Format(time.RFC3339)

	// Add computed field: order_value category
	if amount, ok := data["amount"].(float64); ok {
		var category string
		switch {
		case amount >= 500:
			category = "high"
		case amount >= 100:
			category = "medium"
		default:
			category = "low"
		}
		data["value_category"] = category
	}

	// Add headers
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	req.Headers["X-Wasmer-Processed"] = "true"
	req.Headers["X-Processing-Runtime"] = "wasmer"
	req.Headers["X-Request-Direction"] = req.Direction

	output := WasmOutput{
		Payload: data,
		Headers: req.Headers,
	}

	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		os.Exit(1)
	}
}
