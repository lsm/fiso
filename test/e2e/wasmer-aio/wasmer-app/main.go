package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

type ProcessResponse struct {
	Processor  string `json:"processor"`
	RequestID  string `json:"request_id"`
	Count      int64  `json:"count"`
	Timestamp  string `json:"timestamp"`
	Health     string `json:"health"`
	AppVersion string `json:"app_version"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	Uptime    string `json:"uptime"`
	Processed int64  `json:"processed"`
}

var (
	requestCount atomic.Int64
	startTime    time.Time
	appName      string
)

func main() {
	appName = os.Getenv("APP_NAME")
	if appName == "" {
		appName = "processor"
	}
	startTime = time.Now()

	log.Printf("Wasmer processor app starting: %s", appName)

	// GET /process - main processing endpoint
	http.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		count := requestCount.Add(1)
		log.Printf("/process request #%d", count)

		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = fmt.Sprintf("req-%d", count)
		}

		resp := ProcessResponse{
			Processor:  appName,
			RequestID:  requestID,
			Count:      count,
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Health:     "ok",
			AppVersion: "1.0.0-aio",
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Processor-Name", appName)
		json.NewEncoder(w).Encode(resp)
	})

	// GET /health - health check
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		uptime := time.Since(startTime).String()
		resp := HealthResponse{
			Status:    "healthy",
			Uptime:    uptime,
			Processed: requestCount.Load(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// GET /metrics - simple metrics
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		fmt.Fprintf(w, "# HELP processor_requests_total Total requests processed\n")
		fmt.Fprintf(w, "# TYPE processor_requests_total counter\n")
		fmt.Fprintf(w, "processor_requests_total{app=\"%s\"} %d\n", appName, requestCount.Load())
	})

	// Default handler
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, "fiso-wasmer-aio processor\n")
	})

	addr := ":9000"
	log.Printf("processor listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
