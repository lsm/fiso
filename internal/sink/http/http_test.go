package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeliver_Success(t *testing.T) {
	var receivedBody []byte
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s, err := NewSink(Config{URL: server.URL, Method: "POST"})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	headers := map[string]string{
		"Content-Type": "application/cloudevents+json",
		"X-Custom":     "test-value",
	}
	payload := []byte(`{"id":"evt-1","data":"hello"}`)

	err = s.Deliver(context.Background(), payload, headers)
	if err != nil {
		t.Fatalf("deliver failed: %v", err)
	}

	if string(receivedBody) != string(payload) {
		t.Errorf("body mismatch: got %s, want %s", receivedBody, payload)
	}
	if receivedHeaders.Get("Content-Type") != "application/cloudevents+json" {
		t.Errorf("Content-Type mismatch: got %s", receivedHeaders.Get("Content-Type"))
	}
	if receivedHeaders.Get("X-Custom") != "test-value" {
		t.Errorf("X-Custom mismatch: got %s", receivedHeaders.Get("X-Custom"))
	}
}

func TestDeliver_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	s, err := NewSink(Config{URL: server.URL, Method: "POST"})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.Deliver(context.Background(), []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestDeliver_ClientError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	s, err := NewSink(Config{URL: server.URL, Method: "POST"})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.Deliver(context.Background(), []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestDeliver_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s, err := NewSink(Config{URL: server.URL, Method: "POST"})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = s.Deliver(ctx, []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestDeliver_InvalidURL(t *testing.T) {
	s, err := NewSink(Config{URL: "http://localhost:1", Method: "POST"})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.Deliver(context.Background(), []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

func TestDeliver_DefaultMethod(t *testing.T) {
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s, err := NewSink(Config{URL: server.URL})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.Deliver(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("deliver failed: %v", err)
	}

	if receivedMethod != "POST" {
		t.Errorf("expected POST, got %s", receivedMethod)
	}
}

func TestDeliver_NilHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s, err := NewSink(Config{URL: server.URL, Method: "POST"})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.Deliver(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("deliver failed: %v", err)
	}
}

func TestDeliver_StaticHeaders(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s, err := NewSink(Config{
		URL:    server.URL,
		Method: "POST",
		Headers: map[string]string{
			"X-Static": "configured",
		},
	})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.Deliver(context.Background(), []byte(`{}`), map[string]string{
		"X-Dynamic": "per-event",
	})
	if err != nil {
		t.Fatalf("deliver failed: %v", err)
	}

	if receivedHeaders.Get("X-Static") != "configured" {
		t.Errorf("missing static header, got: %s", receivedHeaders.Get("X-Static"))
	}
	if receivedHeaders.Get("X-Dynamic") != "per-event" {
		t.Errorf("missing dynamic header, got: %s", receivedHeaders.Get("X-Dynamic"))
	}
}

func TestDeliver_RetriesOnServerError(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s, err := NewSink(Config{
		URL:    server.URL,
		Method: "POST",
		Retry: RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 10 * time.Millisecond,
			MaxInterval:     50 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.Deliver(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestNewSink_MissingURL(t *testing.T) {
	_, err := NewSink(Config{})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestDeliver_BackoffCapped(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s, err := NewSink(Config{
		URL: server.URL,
		Retry: RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.Deliver(context.Background(), []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDeliver_429Retries(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s, err := NewSink(Config{
		URL: server.URL,
		Retry: RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.Deliver(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("expected success after retry on 429, got: %v", err)
	}
}

func TestDeliver_RetryContextCancelled(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s, err := NewSink(Config{
		URL: server.URL,
		Retry: RetryConfig{
			MaxAttempts:     10,
			InitialInterval: 1 * time.Second,
			MaxInterval:     5 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = s.Deliver(ctx, []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error for cancelled context during retry")
	}
}

func TestIsPermanent(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"400", &StatusError{Code: 400}, true},
		{"404", &StatusError{Code: 404}, true},
		{"429", &StatusError{Code: 429}, false},
		{"500", &StatusError{Code: 500}, false},
		{"non-status", io.EOF, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPermanent(tt.err); got != tt.expected {
				t.Errorf("isPermanent(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestStatusError_Error(t *testing.T) {
	se := &StatusError{Code: 503}
	if se.Error() != "http status 503" {
		t.Errorf("expected 'http status 503', got %q", se.Error())
	}
}

func TestBackoff(t *testing.T) {
	s, err := NewSink(Config{
		URL: "http://localhost:8080",
		Retry: RetryConfig{
			MaxAttempts:     5,
			InitialInterval: 100 * time.Millisecond,
			MaxInterval:     1 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("failed to create sink: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Test attempt 0 should be 0 (no backoff before first attempt)
	// Test attempt 1: base = 100ms * 2^0 = 100ms
	delay1 := s.backoff(1)
	if delay1 < 80*time.Millisecond || delay1 > 120*time.Millisecond {
		t.Errorf("backoff(1) = %v, expected ~100ms ±20%%", delay1)
	}

	// Test attempt 2: base = 100ms * 2^1 = 200ms
	delay2 := s.backoff(2)
	if delay2 < 160*time.Millisecond || delay2 > 240*time.Millisecond {
		t.Errorf("backoff(2) = %v, expected ~200ms ±20%%", delay2)
	}

	// Test capped at MaxInterval
	delay10 := s.backoff(10)
	if delay10 > 1200*time.Millisecond {
		t.Errorf("backoff(10) = %v, expected <= 1200ms (MaxInterval 1s + 20%% jitter)", delay10)
	}
	if delay10 < 800*time.Millisecond {
		t.Errorf("backoff(10) = %v, expected >= 800ms (MaxInterval 1s - 20%% jitter)", delay10)
	}
}
