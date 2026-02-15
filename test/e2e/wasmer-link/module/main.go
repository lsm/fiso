package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
)

type InterceptRequest struct {
	Payload   json.RawMessage   `json:"payload"`
	Headers   map[string]string `json:"headers"`
	Direction string            `json:"direction"`
	Target    string            `json:"target"`
	Path      string            `json:"path"`
	Method    string            `json:"method"`
}

type InterceptResponse struct {
	Payload   interface{}       `json:"payload"`
	Headers   map[string]string `json:"headers"`
	Modified  bool              `json:"modified"`
	Timestamp string            `json:"timestamp"`
}

func main() {
	mode := os.Getenv("INTERCEPT_MODE")
	if mode == "" {
		mode = "request"
	}

	var input []byte
	var err error

	if len(os.Args) > 2 && os.Args[1] == "--stdin-file" {
		input, err = os.ReadFile(os.Args[2])
	} else {
		input, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		log.Printf("error reading input: %v", err)
		os.Exit(1)
	}

	var req InterceptRequest
	if err := json.Unmarshal(input, &req); err != nil {
		log.Printf("error parsing input: %v", err)
		os.Exit(1)
	}

	log.Printf("intercept: direction=%s target=%s path=%s method=%s",
		req.Direction, req.Target, req.Path, req.Method)

	// Parse the payload
	var data map[string]interface{}
	if err := json.Unmarshal(req.Payload, &data); err != nil {
		// Non-JSON payload, pass through
		data = map[string]interface{}{
			"raw": string(req.Payload),
		}
	}

	// Add interception metadata
	data["intercepted"] = true
	data["intercept_mode"] = mode
	data["direction"] = req.Direction

	// Add/modify headers
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	req.Headers["X-Intercepted-By"] = "wasmer-link"
	req.Headers["X-Intercept-Direction"] = req.Direction

	// Mark as modified
	modified := true

	output := InterceptResponse{
		Payload:   data,
		Headers:   req.Headers,
		Modified:  modified,
		Timestamp: "now",
	}

	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		log.Printf("error encoding output: %v", err)
		os.Exit(1)
	}
}
