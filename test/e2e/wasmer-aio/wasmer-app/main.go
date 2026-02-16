package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"
)

type AppRequest struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

type AppResponse struct {
	Status   int               `json:"status"`
	Headers  map[string]string `json:"headers,omitempty"`
	Body     interface{}       `json:"body,omitempty"`
	BodyText string            `json:"bodyText,omitempty"`
}

var count atomic.Int64

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

	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = "processor"
	}

	resp := AppResponse{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}}

	switch {
	case req.Path == "/process" && (req.Method == "GET" || req.Method == "POST"):
		n := count.Add(1)
		resp.Body = map[string]interface{}{
			"processor":   appName,
			"request_id":  fmt.Sprintf("req-%d", n),
			"count":       n,
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
			"health":      "ok",
			"app_version": "2.0.0-aio",
		}
	case req.Path == "/health" && req.Method == "GET":
		resp.Body = map[string]interface{}{"status": "healthy", "processed": count.Load()}
	case req.Path == "/metrics" && req.Method == "GET":
		resp.Headers["Content-Type"] = "text/plain; version=0.0.4"
		resp.BodyText = fmt.Sprintf("# HELP processor_requests_total Total requests processed\n# TYPE processor_requests_total counter\nprocessor_requests_total{app=\"%s\"} %d\n", appName, count.Load())
	case req.Path == "/" && req.Method == "GET":
		resp.Headers["Content-Type"] = "text/plain"
		resp.BodyText = "fiso-wasmer-aio processor\n"
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
