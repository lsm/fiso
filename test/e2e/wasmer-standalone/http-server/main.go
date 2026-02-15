package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

type HelloResponse struct {
	Greeting string `json:"greeting"`
	Server   string `json:"server"`
	Version  string `json:"version"`
}

type EchoRequest struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
}

type EchoResponse struct {
	Received EchoRequest `json:"received"`
	Echoed   bool        `json:"echoed"`
	Server   string      `json:"server"`
}

type HealthResponse struct {
	Status string `json:"status"`
	Uptime string `json:"uptime"`
}

var startTime string

func main() {
	serverName := os.Getenv("SERVER_NAME")
	if serverName == "" {
		serverName = "fiso-wasmer"
	}
	startTime = "0s"

	log.Printf("HTTP server starting, name=%s", serverName)

	// GET /hello - greeting endpoint
	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		log.Printf("GET /hello from %s", r.RemoteAddr)

		resp := HelloResponse{
			Greeting: "Hello from WASIX!",
			Server:   serverName,
			Version:  "1.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// POST /echo - echo endpoint
	http.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}

		var req EchoRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		log.Printf("POST /echo: message=%s count=%d", req.Message, req.Count)

		resp := EchoResponse{
			Received: req,
			Echoed:   true,
			Server:   serverName,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// GET /health - health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		resp := HealthResponse{
			Status: "healthy",
			Uptime: startTime,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Default handler
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, "fiso-wasmer HTTP server\n")
	})

	addr := ":9000"
	log.Printf("server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
