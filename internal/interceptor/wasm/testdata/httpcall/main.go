package main

import (
	"encoding/base64"
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
	BodyB64 string            `json:"bodyB64,omitempty"`
}

type callResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	BodyB64 string            `json:"bodyB64,omitempty"`
}

var badPtr bool

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
		BodyB64: base64.StdEncoding.EncodeToString(env.Payload),
	})
	reqBuf := []byte(req)
	respBuf := make([]byte, 64*1024)

	// Test modes (set via FISO_TEST_MODE) exercise the host error paths.
	mode := os.Getenv("FISO_TEST_MODE")
	switch mode {
	case "badreq":
		reqBuf = []byte("not-json")
	case "smallbuf":
		respBuf = respBuf[:8]
	case "traversal":
		cr := callRequest{Target: "enrich-api", Path: "/../secret"}
		b, _ := json.Marshal(cr)
		reqBuf = b
	case "badresp":
		cr := callRequest{Target: "enrich-api", Path: "/x"}
		b, _ := json.Marshal(cr)
		reqBuf = b
		badPtr = true
	case "emptytarget":
		cr := callRequest{}
		b, _ := json.Marshal(cr)
		reqBuf = b
	case "plaintext":
		// Body that is not JSON: base64 carries it verbatim.
		cr := callRequest{Target: "enrich-api", Path: "/", Method: "POST", BodyB64: base64.StdEncoding.EncodeToString([]byte("hello=world"))}
		b, _ := json.Marshal(cr)
		reqBuf = b
	}

	respPtr := uintptr(unsafe.Pointer(&respBuf[0]))
	respCap := uint32(len(respBuf))
	if badPtr {
		respPtr = 0xFFFF0000 // far out of linear memory
	}
	n := http_call(
		uint32(uintptr(unsafe.Pointer(&reqBuf[0]))),
		uint32(len(reqBuf)),
		uint32(respPtr),
		respCap,
	)
	if n < 0 {
		// Surface the host error code through the output headers so the
		// test can pin the denial path.
		env.Headers["X-Host-Error"] = fmt.Sprintf("%d", n)
	} else {
		var cr callResponse
		if json.Unmarshal(respBuf[:n], &cr) == nil {
			env.Headers["X-Api-Status"] = fmt.Sprintf("%d", cr.Status)
			if raw, err := base64.StdEncoding.DecodeString(cr.BodyB64); err == nil {
				env.Payload = raw
			}
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
