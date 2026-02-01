package observability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz_AlwaysOK(t *testing.T) {
	hs := NewHealthServer()
	handler := hs.Handler()

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %s", body["status"])
	}
}

func TestReadyz_NotReadyByDefault(t *testing.T) {
	hs := NewHealthServer()
	handler := hs.Handler()

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestReadyz_ReadyAfterSet(t *testing.T) {
	hs := NewHealthServer()
	hs.SetReady(true)
	handler := hs.Handler()

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("expected status ready, got %s", body["status"])
	}
}

func TestReadyz_BackToNotReady(t *testing.T) {
	hs := NewHealthServer()
	hs.SetReady(true)
	hs.SetReady(false)
	handler := hs.Handler()

	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}
