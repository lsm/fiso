package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"unsafe"
)

//go:wasmimport fiso http_call
func http_call(reqPtr, reqLen, respPtr, respCap uint32) int32

type envelope struct {
	Payload   json.RawMessage   `json:"payload"`
	Headers   map[string]string `json:"headers"`
	Direction string            `json:"direction"`
}

type callRequest struct {
	Target  string            `json:"target"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

type callResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

func main() {
	input, _ := readAllStdin()
	var env envelope
	if err := json.Unmarshal(input, &env); err != nil {
		os.Exit(1)
	}

	// Call the allowed target through the host function.
	req, _ := json.Marshal(callRequest{
		Target:  "enrich-api",
		Method:  "POST",
		Path:    "/lookup",
		Headers: map[string]string{"X-Caller": "wasm"},
		Body:    env.Payload,
	})
	reqBuf := []byte(req)
	respBuf := make([]byte, 64*1024)

	n := http_call(
		uint32(uintptr(unsafe.Pointer(&reqBuf[0]))),
		uint32(len(reqBuf)),
		uint32(uintptr(unsafe.Pointer(&respBuf[0]))),
		uint32(len(respBuf)),
	)
	if n < 0 {
		// Surface the host error code through the output headers so the
		// test can pin the denial path.
		env.Headers["X-Host-Error"] = fmt.Sprintf("%d", n)
	} else {
		var cr callResponse
		if json.Unmarshal(respBuf[:n], &cr) == nil {
			env.Headers["X-Api-Status"] = fmt.Sprintf("%d", cr.Status)
			env.Payload = cr.Body
		}
	}

	out, _ := json.Marshal(envelope{Payload: env.Payload, Headers: env.Headers})
	fmt.Println(string(out))
}

func readAllStdin() ([]byte, error) {
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}

var _ = binary.LittleEndian
