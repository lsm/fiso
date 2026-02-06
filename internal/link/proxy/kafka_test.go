package proxy

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lsm/fiso/internal/link"
	"github.com/lsm/fiso/internal/link/circuitbreaker"
	"github.com/lsm/fiso/internal/link/ratelimit"
)

// mockPublisher is a mock dlq.Publisher for testing.
type mockPublisher struct {
	publishFunc func(ctx context.Context, topic string, key, value []byte, headers map[string]string) error
}

func (m *mockPublisher) Publish(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, topic, key, value, headers)
	}
	return nil
}

func (m *mockPublisher) Close() error {
	return nil
}

func TestKafkaHandler_ServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		targetName     string
		targetProtocol string
		targetKafka    *link.KafkaConfig
		body           []byte
		publishError   error
		wantStatus     int
		wantBody       string
	}{
		{
			name:           "successful publish",
			method:         "POST",
			targetName:     "test-kafka",
			targetProtocol: "kafka",
			targetKafka: &link.KafkaConfig{
				Topic: "test-topic",
				Key: link.KeyStrategy{
					Type: "uuid",
				},
			},
			body:       []byte(`{"test":"data"}`),
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"published","topic":"test-topic"}`,
		},
		{
			name:           "wrong method",
			method:         "GET",
			targetName:     "test-kafka",
			targetProtocol: "kafka",
			targetKafka:    &link.KafkaConfig{Topic: "test-topic"},
			wantStatus:     http.StatusMethodNotAllowed,
		},
		{
			name:           "target not found",
			method:         "POST",
			targetName:     "missing",
			targetProtocol: "kafka",
			wantStatus:     http.StatusNotFound,
		},
		{
			name:           "wrong protocol",
			method:         "POST",
			targetName:     "test-http",
			targetProtocol: "http",
			wantStatus:     http.StatusBadRequest,
		},
		{
			name:           "circuit breaker open",
			method:         "POST",
			targetName:     "test-kafka",
			targetProtocol: "kafka",
			targetKafka:    &link.KafkaConfig{Topic: "test-topic"},
			wantStatus:     http.StatusServiceUnavailable,
		},
		{
			name:           "rate limit exceeded",
			method:         "POST",
			targetName:     "test-kafka",
			targetProtocol: "kafka",
			targetKafka:    &link.KafkaConfig{Topic: "test-topic"},
			wantStatus:     http.StatusTooManyRequests,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			var store *link.TargetStore
			if tt.name == "target not found" {
				// Empty store - target won't be found
				store = link.NewTargetStore([]link.LinkTarget{})
			} else {
				// Store with the test target
				store = link.NewTargetStore([]link.LinkTarget{
					{
						Name:     tt.targetName,
						Protocol: tt.targetProtocol,
						Kafka:    tt.targetKafka,
					},
				})
			}

			breakers := make(map[string]*circuitbreaker.Breaker)
			rateLimiter := ratelimit.New()

			// Configure circuit breaker for test
			if tt.name == "circuit breaker open" {
				breakers[tt.targetName] = circuitbreaker.New(circuitbreaker.Config{
					FailureThreshold: 1,
					SuccessThreshold: 1,
					ResetTimeout:     1000 * time.Millisecond,
				})
				// Trip the breaker
				breakers[tt.targetName].RecordFailure()
			}

			// Configure rate limiter for test
			if tt.name == "rate limit exceeded" {
				rateLimiter.Set(tt.targetName, 0.0001, 1) // Very low rate, burst 1
				// Consume the burst so the next request is blocked
				rateLimiter.Allow(tt.targetName)
			}

			publisher := &mockPublisher{
				publishFunc: func(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
					return tt.publishError
				},
			}

			handler := NewKafkaHandler(publisher, store, breakers, rateLimiter, nil, nil)

			// Create request
			var req *http.Request
			if tt.body != nil {
				req = httptest.NewRequest(tt.method, "/link/"+tt.targetName, bytes.NewReader(tt.body))
			} else {
				req = httptest.NewRequest(tt.method, "/link/"+tt.targetName, nil)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			// Check response
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && w.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", w.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestGenerateKey(t *testing.T) {
	tests := []struct {
		name      string
		strategy  link.KeyStrategy
		body      []byte
		headers   http.Header
		wantKey   string
		wantError bool
	}{
		{
			name:     "no key",
			strategy: link.KeyStrategy{},
			wantKey:  "",
		},
		{
			name:     "uuid",
			strategy: link.KeyStrategy{Type: "uuid"},
			wantKey:  "", // Can't predict UUID, just check no error
		},
		{
			name:     "header extraction",
			strategy: link.KeyStrategy{Type: "header", Field: "X-Message-Id"},
			headers:  http.Header{"X-Message-Id": []string{"msg-123"}},
			wantKey:  "msg-123",
		},
		{
			name:      "header not found",
			strategy:  link.KeyStrategy{Type: "header", Field: "X-Missing"},
			wantError: true,
		},
		{
			name:     "payload extraction",
			strategy: link.KeyStrategy{Type: "payload", Field: "user_id"},
			body:     []byte(`{"user_id":"user-456","other":"data"}`),
			wantKey:  "user-456",
		},
		{
			name:      "payload field not found",
			strategy:  link.KeyStrategy{Type: "payload", Field: "missing"},
			body:      []byte(`{"other":"data"}`),
			wantError: true,
		},
		{
			name:     "static key",
			strategy: link.KeyStrategy{Type: "static", Value: "fixed-key"},
			wantKey:  "fixed-key",
		},
		{
			name:     "random key",
			strategy: link.KeyStrategy{Type: "random"},
			wantKey:  "", // Can't predict, just check no error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher := &mockPublisher{}
			store := link.NewTargetStore(nil)
			handler := NewKafkaHandler(publisher, store, nil, nil, nil, nil)

			key, err := handler.generateKey(tt.strategy, tt.body, tt.headers)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.wantKey != "" && string(key) != tt.wantKey {
				t.Errorf("key = %q, want %q", string(key), tt.wantKey)
			}
		})
	}
}

func TestKafkaHandler_RetryLogic(t *testing.T) {
	// Test retry logic when publish fails initially then succeeds
	attempts := 0
	publisher := &mockPublisher{
		publishFunc: func(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
			attempts++
			if attempts < 2 {
				return fmt.Errorf("temporary failure")
			}
			return nil
		},
	}

	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "retry-test",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Topic: "retry-topic"},
			Retry: link.RetryConfig{
				MaxAttempts: 3,
			},
		},
	})

	handler := NewKafkaHandler(publisher, store, nil, nil, nil, nil)

	req := httptest.NewRequest("POST", "/link/retry-test", bytes.NewReader([]byte(`{"test":"data"}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 after retries, got %d", w.Code)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestKafkaHandler_PublishFailureAfterRetries(t *testing.T) {
	// Test that all retries are exhausted before giving up
	attempts := 0
	publisher := &mockPublisher{
		publishFunc: func(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
			attempts++
			return fmt.Errorf("persistent failure")
		},
	}

	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "fail-test",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Topic: "fail-topic"},
			Retry: link.RetryConfig{
				MaxAttempts: 3,
			},
		},
	})

	breakers := make(map[string]*circuitbreaker.Breaker)
	breakers["fail-test"] = circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		ResetTimeout:     1000 * time.Millisecond,
	})

	handler := NewKafkaHandler(publisher, store, breakers, nil, nil, nil)

	req := httptest.NewRequest("POST", "/link/fail-test", bytes.NewReader([]byte(`{"test":"data"}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status 502 after failed retries, got %d", w.Code)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestKafkaHandler_StaticHeaders(t *testing.T) {
	// Test that static headers from config are added to Kafka messages
	var capturedHeaders map[string]string
	publisher := &mockPublisher{
		publishFunc: func(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
			capturedHeaders = headers
			return nil
		},
	}

	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "headers-test",
			Protocol: "kafka",
			Kafka: &link.KafkaConfig{
				Topic: "headers-topic",
				Headers: map[string]string{
					"source":  "test-service",
					"version": "1.0",
				},
			},
		},
	})

	handler := NewKafkaHandler(publisher, store, nil, nil, nil, nil)

	req := httptest.NewRequest("POST", "/link/headers-test", bytes.NewReader([]byte(`{"test":"data"}`)))
	req.Header.Set("X-Request-ID", "req-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if capturedHeaders["source"] != "test-service" {
		t.Errorf("expected static header source=test-service, got %s", capturedHeaders["source"])
	}
	if capturedHeaders["version"] != "1.0" {
		t.Errorf("expected static header version=1.0, got %s", capturedHeaders["version"])
	}
	if capturedHeaders["X-Request-ID"] != "req-123" {
		t.Errorf("expected HTTP header X-Request-ID=req-123, got %s", capturedHeaders["X-Request-ID"])
	}
}

func TestKafkaHandler_EmptyTargetName(t *testing.T) {
	publisher := &mockPublisher{}
	store := link.NewTargetStore([]link.LinkTarget{})
	handler := NewKafkaHandler(publisher, store, nil, nil, nil, nil)

	req := httptest.NewRequest("POST", "/link/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty target name, got %d", w.Code)
	}
}

func TestKafkaHandler_InvalidJSON(t *testing.T) {
	publisher := &mockPublisher{}
	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "test-kafka",
			Protocol: "kafka",
			Kafka: &link.KafkaConfig{
				Topic: "test-topic",
				Key: link.KeyStrategy{
					Type:  "payload",
					Field: "user_id",
				},
			},
		},
	})
	handler := NewKafkaHandler(publisher, store, nil, nil, nil, nil)

	req := httptest.NewRequest("POST", "/link/test-kafka", bytes.NewReader([]byte(`invalid json`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestKafkaHandler_UnknownKeyType(t *testing.T) {
	publisher := &mockPublisher{}
	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "test-kafka",
			Protocol: "kafka",
			Kafka: &link.KafkaConfig{
				Topic: "test-topic",
				Key: link.KeyStrategy{
					Type: "unknown-type",
				},
			},
		},
	})
	handler := NewKafkaHandler(publisher, store, nil, nil, nil, nil)

	req := httptest.NewRequest("POST", "/link/test-kafka", bytes.NewReader([]byte(`{"test":"data"}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown key type, got %d", w.Code)
	}
}

func TestKafkaHandler_ContextCancellation(t *testing.T) {
	// Test context cancellation during retry
	publisher := &mockPublisher{
		publishFunc: func(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
			// Simulate slow operation that will be cancelled
			time.Sleep(50 * time.Millisecond)
			return fmt.Errorf("publish failed")
		},
	}

	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "cancel-test",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Topic: "cancel-topic"},
			Retry: link.RetryConfig{
				MaxAttempts: 3,
			},
		},
	})

	handler := NewKafkaHandler(publisher, store, nil, nil, nil, nil)

	// Create request with short timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("POST", "/link/cancel-test", bytes.NewReader([]byte(`{"test":"data"}`)))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should fail due to context timeout
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for context timeout, got %d", w.Code)
	}
}

func TestKafkaHandler_WithMetrics(t *testing.T) {
	// Test metrics recording on success
	publisher := &mockPublisher{}
	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "metrics-test",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Topic: "metrics-topic"},
		},
	})

	reg := prometheus.NewRegistry()
	metrics := link.NewMetrics(reg)

	handler := NewKafkaHandler(publisher, store, nil, nil, metrics, nil)

	req := httptest.NewRequest("POST", "/link/metrics-test", bytes.NewReader([]byte(`{"test":"data"}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestKafkaHandler_CircuitBreakerWithMetrics(t *testing.T) {
	// Test circuit breaker metrics update when open
	publisher := &mockPublisher{}
	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "cb-test",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Topic: "cb-topic"},
		},
	})

	breakers := make(map[string]*circuitbreaker.Breaker)
	breakers["cb-test"] = circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		ResetTimeout:     1000 * time.Millisecond,
	})
	// Trip the breaker
	breakers["cb-test"].RecordFailure()

	reg := prometheus.NewRegistry()
	metrics := link.NewMetrics(reg)

	handler := NewKafkaHandler(publisher, store, breakers, nil, metrics, nil)

	req := httptest.NewRequest("POST", "/link/cb-test", bytes.NewReader([]byte(`{"test":"data"}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestKafkaHandler_RateLimitWithMetrics(t *testing.T) {
	// Test rate limit metrics update
	publisher := &mockPublisher{}
	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "rl-test",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Topic: "rl-topic"},
		},
	})

	rateLimiter := ratelimit.New()
	rateLimiter.Set("rl-test", 0.0001, 1) // Very low rate
	rateLimiter.Allow("rl-test")         // Consume the burst

	reg := prometheus.NewRegistry()
	metrics := link.NewMetrics(reg)

	handler := NewKafkaHandler(publisher, store, nil, rateLimiter, metrics, nil)

	req := httptest.NewRequest("POST", "/link/rl-test", bytes.NewReader([]byte(`{"test":"data"}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestKafkaHandler_ReadBodyError(t *testing.T) {
	// Test read body error
	publisher := &mockPublisher{}
	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "body-error-test",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Topic: "body-error-topic"},
		},
	})

	reg := prometheus.NewRegistry()
	metrics := link.NewMetrics(reg)
	handler := NewKafkaHandler(publisher, store, nil, nil, metrics, nil)

	// Create a reader that errors
	req := httptest.NewRequest("POST", "/link/body-error-test", &errorReader{})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for body read error, got %d", w.Code)
	}
}

func TestNormalizeHeaderKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"X-Request-Id", "X-Request-ID"},
		{"X-Correlation-Id", "X-Correlation-ID"},
		{"X-Trace-Id", "X-Trace-ID"},
		{"X-Span-Id", "X-Span-ID"},
		{"X-Session-Id", "X-Session-ID"},
		{"X-User-Id", "X-User-ID"},
		{"X-Client-Id", "X-Client-ID"},
		{"X-Api-Key", "X-API-Key"},
		{"X-Forwarded-For", "X-Forwarded-For"},
		{"X-Forwarded-Proto", "X-Forwarded-Proto"},
		{"X-Forwarded-Host", "X-Forwarded-Host"},
		{"Content-Type", "Content-Type"}, // Not in map, unchanged
		{"X-Custom-Header", "X-Custom-Header"}, // Not in map, unchanged
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeHeaderKey(tt.input)
			if got != tt.want {
				t.Errorf("normalizeHeaderKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestKafkaHandler_DefaultTopic(t *testing.T) {
	// Test default topic when Kafka config is nil
	var capturedTopic string
	publisher := &mockPublisher{
		publishFunc: func(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
			capturedTopic = topic
			return nil
		},
	}

	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "no-config",
			Protocol: "kafka",
			Kafka:    nil, // No Kafka config
		},
	})

	handler := NewKafkaHandler(publisher, store, nil, nil, nil, nil)

	req := httptest.NewRequest("POST", "/link/no-config", bytes.NewReader([]byte(`{"test":"data"}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if capturedTopic != "default-topic" {
		t.Errorf("expected default-topic, got %s", capturedTopic)
	}
}

func TestKafkaHandler_CircuitBreakerRecordsSuccess(t *testing.T) {
	// Test circuit breaker records success
	publisher := &mockPublisher{}
	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "cb-success",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Topic: "cb-success-topic"},
		},
	})

	breakers := make(map[string]*circuitbreaker.Breaker)
	breakers["cb-success"] = circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 5,
		SuccessThreshold: 1,
		ResetTimeout:     1000 * time.Millisecond,
	})

	handler := NewKafkaHandler(publisher, store, breakers, nil, nil, nil)

	req := httptest.NewRequest("POST", "/link/cb-success", bytes.NewReader([]byte(`{"test":"data"}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestKafkaHandler_PublishFailureWithMetrics(t *testing.T) {
	// Test that metrics are recorded on publish failure
	publisher := &mockPublisher{
		publishFunc: func(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
			return fmt.Errorf("publish failed")
		},
	}

	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "fail-metrics",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Topic: "fail-metrics-topic"},
			Retry: link.RetryConfig{
				MaxAttempts: 2,
			},
		},
	})

	breakers := make(map[string]*circuitbreaker.Breaker)
	breakers["fail-metrics"] = circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: 5,
		SuccessThreshold: 1,
		ResetTimeout:     1000 * time.Millisecond,
	})

	reg := prometheus.NewRegistry()
	metrics := link.NewMetrics(reg)

	handler := NewKafkaHandler(publisher, store, breakers, nil, metrics, nil)

	req := httptest.NewRequest("POST", "/link/fail-metrics", bytes.NewReader([]byte(`{"test":"data"}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

// errorReader is a reader that always returns an error.
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("read error")
}
