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
		// Fall back to pass-through envelope to avoid hard-failing on non-object payloads
		data = map[string]interface{}{"raw": string(req.Payload)}
	}

	// AIO-specific enrichment
	data["aio_transformed"] = true
	data["aio_timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	data["aio_runtime"] = "wasmer"

	// Add processing metadata
	if _, ok := data["order_id"]; ok {
		data["aio_type"] = "order"
		// Calculate total if items present
		if items, ok := data["items"].([]interface{}); ok {
			var total float64
			for _, item := range items {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if qty, ok := itemMap["qty"].(float64); ok {
						if price, ok := itemMap["price"].(float64); ok {
							total += qty * price
						}
					}
				}
			}
			data["aio_calculated_total"] = total
		}
	} else if _, ok := data["type"]; ok {
		data["aio_type"] = "notification"
		data["aio_routed"] = true
	}

	// Add headers
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	req.Headers["X-AIO-Processed"] = "true"
	req.Headers["X-AIO-Transform"] = "wasmer"

	output := WasmOutput{
		Payload: data,
		Headers: req.Headers,
	}

	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		os.Exit(1)
	}
}
