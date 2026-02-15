package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"
)

type OrderRequest struct {
	OrderID            string                 `json:"order_id"`
	Items              []map[string]interface{} `json:"items"`
	Customer           string                 `json:"customer"`
	AIOTransformed     bool                   `json:"aio_transformed"`
	AIOCalculatedTotal float64                `json:"aio_calculated_total"`
}

type OrderResponse struct {
	Status          string  `json:"status"`
	OrderID         string  `json:"order_id"`
	ProcessedAt     string  `json:"processed_at"`
	TransformedByAIO bool   `json:"transformed_by_aio"`
	Total           float64 `json:"total,omitempty"`
}

type NotificationRequest struct {
	Type           string `json:"type"`
	Recipient      string `json:"recipient"`
	Message        string `json:"message"`
	AIOTransformed bool   `json:"aio_transformed"`
	AIOType        string `json:"aio_type"`
}

type NotificationResponse struct {
	Status          string `json:"status"`
	Type            string `json:"type"`
	Recipient       string `json:"recipient"`
	ProcessedAt     string `json:"processed_at"`
	TransformedByAIO bool  `json:"transformed_by_aio"`
}

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Service string `json:"service"`
}

var startTime time.Time

func main() {
	startTime = time.Now()
	log.Println("backend-service starting on :8080")

	// POST /orders - receive orders from flow
	http.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}

		log.Printf("received order: %s", body)

		var req OrderRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		resp := OrderResponse{
			Status:          "received",
			OrderID:         req.OrderID,
			ProcessedAt:     time.Now().UTC().Format(time.RFC3339),
			TransformedByAIO: req.AIOTransformed,
			Total:           req.AIOCalculatedTotal,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// POST /notifications - receive notifications from flow
	http.HandleFunc("/notifications", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}

		log.Printf("received notification: %s", body)

		var req NotificationRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		resp := NotificationResponse{
			Status:          "queued",
			Type:            req.Type,
			Recipient:       req.Recipient,
			ProcessedAt:     time.Now().UTC().Format(time.RFC3339),
			TransformedByAIO: req.AIOTransformed,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// GET /api/health - health check for link proxy
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		resp := HealthResponse{
			Status:  "healthy",
			Version: "1.0.0",
			Service: "backend-service",
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
		w.Write([]byte("backend-service\n"))
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
