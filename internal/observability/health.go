package observability

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// HealthServer exposes /healthz and /readyz endpoints.
type HealthServer struct {
	ready atomic.Bool
}

// NewHealthServer creates a new health server.
func NewHealthServer() *HealthServer {
	return &HealthServer{}
}

// SetReady marks the server as ready to receive traffic.
func (h *HealthServer) SetReady(ready bool) {
	h.ready.Store(ready)
}

// Handler returns an http.Handler with health and readiness endpoints.
func (h *HealthServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleHealth)
	mux.HandleFunc("GET /readyz", h.handleReady)
	return mux
}

func (h *HealthServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *HealthServer) handleReady(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.ready.Load() {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "not ready"})
	}
}
