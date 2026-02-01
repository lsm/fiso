package schema

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConfluentRegistry_GetByID(t *testing.T) {
	schema := Schema{ID: 1, Schema: `{"type":"string"}`, Type: "JSON"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/schemas/ids/1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(schema)
	}))
	defer srv.Close()

	reg, err := NewConfluentRegistry(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s, err := reg.GetByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ID != 1 {
		t.Errorf("expected ID 1, got %d", s.ID)
	}
	if s.Schema != `{"type":"string"}` {
		t.Errorf("unexpected schema: %s", s.Schema)
	}
}

func TestConfluentRegistry_GetLatest(t *testing.T) {
	schema := Schema{ID: 5, Subject: "orders-value", Version: 3, Schema: `{"type":"record"}`, Type: "AVRO"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subjects/orders-value/versions/latest" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(schema)
	}))
	defer srv.Close()

	reg, err := NewConfluentRegistry(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s, err := reg.GetLatest(context.Background(), "orders-value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Subject != "orders-value" {
		t.Errorf("expected subject 'orders-value', got %q", s.Subject)
	}
	if s.Version != 3 {
		t.Errorf("expected version 3, got %d", s.Version)
	}
}

func TestConfluentRegistry_CachesResults(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		json.NewEncoder(w).Encode(Schema{ID: 1, Schema: `{}`})
	}))
	defer srv.Close()

	reg, _ := NewConfluentRegistry(srv.URL)

	// First call hits server
	reg.GetByID(context.Background(), 1)
	if calls != 1 {
		t.Fatalf("expected 1 server call, got %d", calls)
	}

	// Second call uses cache
	reg.GetByID(context.Background(), 1)
	if calls != 1 {
		t.Errorf("expected still 1 server call (cached), got %d", calls)
	}
}

func TestConfluentRegistry_CacheExpires(t *testing.T) {
	now := time.Now()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		json.NewEncoder(w).Encode(Schema{ID: 1, Schema: `{}`})
	}))
	defer srv.Close()

	reg, _ := NewConfluentRegistry(srv.URL,
		WithCacheTTL(1*time.Minute),
		WithRegistryClock(func() time.Time { return now }),
	)

	reg.GetByID(context.Background(), 1)
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}

	// Advance clock past TTL
	now = now.Add(2 * time.Minute)
	reg.GetByID(context.Background(), 1)
	if calls != 2 {
		t.Errorf("expected 2 calls after TTL expiry, got %d", calls)
	}
}

func TestConfluentRegistry_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	reg, _ := NewConfluentRegistry(srv.URL)
	_, err := reg.GetByID(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestConfluentRegistry_EmptyURL(t *testing.T) {
	_, err := NewConfluentRegistry("")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 10 * time.Second}
	reg, err := NewConfluentRegistry("http://localhost:8081", WithHTTPClient(custom))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.client != custom {
		t.Error("expected custom HTTP client to be set")
	}
}

func TestConfluentRegistry_GetLatest_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	reg, _ := NewConfluentRegistry(srv.URL)
	_, err := reg.GetLatest(context.Background(), "unknown-subject")
	if err == nil {
		t.Fatal("expected error on 404 response")
	}
}

func TestConfluentRegistry_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	reg, _ := NewConfluentRegistry(srv.URL)
	_, err := reg.GetByID(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestConfluentRegistry_AcceptHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("Accept")
		if accept != "application/vnd.schemaregistry.v1+json" {
			t.Errorf("unexpected Accept header: %s", accept)
		}
		json.NewEncoder(w).Encode(Schema{ID: 1})
	}))
	defer srv.Close()

	reg, _ := NewConfluentRegistry(srv.URL)
	reg.GetByID(context.Background(), 1)
}
