package main

import (
	"encoding/json"
	"io"
	"os"
)

type AppRequest struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

type AppResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    interface{}       `json:"body,omitempty"`
}

type EchoRequest struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
}

func main() {
	var in []byte
	var err error

	if path, ok := stdinFileArg(os.Args); ok {
		in, err = os.ReadFile(path)
	} else {
		in, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		os.Exit(1)
	}

	var req AppRequest
	if err := json.Unmarshal(in, &req); err != nil {
		os.Exit(1)
	}

	serverName := os.Getenv("SERVER_NAME")
	if serverName == "" {
		serverName = "fiso-wasmer-e2e"
	}

	resp := AppResponse{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}}

	switch {
	case req.Method == "GET" && req.Path == "/hello":
		resp.Body = map[string]interface{}{
			"greeting": "Hello from Wasmer app-mode!",
			"server":   serverName,
			"version":  "2.0.0",
		}
	case req.Method == "POST" && req.Path == "/echo":
		var er EchoRequest
		_ = json.Unmarshal(req.Body, &er)
		resp.Body = map[string]interface{}{
			"received": er,
			"echoed":   true,
			"server":   serverName,
		}
	case req.Method == "GET" && req.Path == "/health":
		resp.Body = map[string]interface{}{"status": "healthy", "uptime": "ok"}
	default:
		resp.Status = 404
		resp.Body = map[string]interface{}{"error": "not found", "path": req.Path}
	}

	if err := json.NewEncoder(os.Stdout).Encode(resp); err != nil {
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
