package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"
)

type ProcessRequest struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"params"`
}

type ProcessResponse struct {
	Action    string      `json:"action"`
	Result    interface{} `json:"result"`
	RequestID string      `json:"request_id"`
	Timestamp string      `json:"timestamp"`
}

type StatusResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Uptime  string `json:"uptime"`
}

var startTime time.Time

func main() {
	startTime = time.Now()
	log.Println("backend-service starting on :8080")

	// POST /api/process - process request
	http.HandleFunc("/api/process", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Log all headers for verification
		log.Printf("received request: method=%s path=%s", r.Method, r.URL.Path)
		for key, values := range r.Header {
			for _, value := range values {
				log.Printf("header: %s=%s", key, value)
			}
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("body: %s", body)

		var req ProcessRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Process the action
		var result interface{}
		switch req.Action {
		case "calculate":
			params := req.Params
			if value, ok := params["value"].(float64); ok {
				if op, ok := params["operation"].(string); ok {
					switch op {
					case "double":
						result = value * 2
					case "triple":
						result = value * 3
					case "square":
						result = value * value
					default:
						result = value
					}
				}
			}
		case "echo":
			result = req.Params
		default:
			result = "unknown action"
		}

		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = "auto-" + time.Now().Format("20060102-150405")
		}

		resp := ProcessResponse{
			Action:    req.Action,
			Result:    result,
			RequestID: requestID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Proxy", "fiso-wasmer-link")
		json.NewEncoder(w).Encode(resp)
	})

	// GET /api/status - health check
	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		uptime := time.Since(startTime).String()
		resp := StatusResponse{
			Status:  "healthy",
			Version: "1.0.0",
			Uptime:  uptime,
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Proxy", "fiso-wasmer-link")
		json.NewEncoder(w).Encode(resp)
	})

	// Default handler
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("backend-service\n"))
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
