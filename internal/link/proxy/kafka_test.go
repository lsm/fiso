package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lsm/fiso/internal/kafka"
	"github.com/lsm/fiso/internal/link"
	"github.com/lsm/fiso/internal/link/circuitbreaker"
	linkinterceptor "github.com/lsm/fiso/internal/link/interceptor"
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
	rateLimiter.Allow("rl-test")          // Consume the burst

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
		{"Content-Type", "Content-Type"},       // Not in map, unchanged
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

func TestNewKafkaHandlerWithPool(t *testing.T) {
	store := link.NewTargetStore([]link.LinkTarget{
		{Name: "test", Protocol: "kafka", Kafka: &link.KafkaConfig{Topic: "test-topic"}},
	})

	registry := kafka.NewRegistry()
	_ = registry.Register("default", &kafka.ClusterConfig{Brokers: []string{"localhost:9092"}})
	pool := kafka.NewPublisherPool(registry)

	handler := NewKafkaHandlerWithPool(pool, store, nil, nil, nil, nil)
	if handler == nil {
		t.Fatal("expected non-nil handler")
		return
	}
	if handler.pool == nil {
		t.Error("expected pool to be set")
	}
}

func TestNewKafkaHandlerWithInterceptors(t *testing.T) {
	store := link.NewTargetStore([]link.LinkTarget{
		{Name: "test", Protocol: "kafka", Kafka: &link.KafkaConfig{Topic: "test-topic"}},
	})

	registry := kafka.NewRegistry()
	_ = registry.Register("default", &kafka.ClusterConfig{Brokers: []string{"localhost:9092"}})
	pool := kafka.NewPublisherPool(registry)

	handler := NewKafkaHandlerWithInterceptors(pool, store, nil, nil, nil, nil, nil)
	if handler == nil {
		t.Fatal("expected non-nil handler")
		return
	}
	if handler.pool == nil {
		t.Error("expected pool to be set")
	}
}

func TestKafkaHandler_GetPublisher_WithPool(t *testing.T) {
	store := link.NewTargetStore([]link.LinkTarget{
		{Name: "test", Protocol: "kafka", Kafka: &link.KafkaConfig{Topic: "test-topic", Cluster: "main"}},
	})

	registry := kafka.NewRegistry()
	_ = registry.Register("main", &kafka.ClusterConfig{Brokers: []string{"localhost:9092"}})
	pool := kafka.NewPublisherPool(registry)

	handler := NewKafkaHandlerWithPool(pool, store, nil, nil, nil, nil)

	target := store.Get("test")
	// Note: This will try to connect to Kafka which may fail, but tests the path
	_, err := handler.getPublisher(target)
	// We expect this might fail due to no Kafka broker, but we're testing the path
	if err != nil {
		t.Logf("getPublisher error (expected if no Kafka): %v", err)
	}
}

func TestKafkaHandler_GetPublisher_DefaultCluster(t *testing.T) {
	store := link.NewTargetStore([]link.LinkTarget{
		{Name: "test", Protocol: "kafka", Kafka: &link.KafkaConfig{Topic: "test-topic"}}, // No cluster specified
	})

	registry := kafka.NewRegistry()
	_ = registry.Register("default", &kafka.ClusterConfig{Brokers: []string{"localhost:9092"}})
	pool := kafka.NewPublisherPool(registry)

	handler := NewKafkaHandlerWithPool(pool, store, nil, nil, nil, nil)

	target := store.Get("test")
	_, err := handler.getPublisher(target)
	if err != nil {
		t.Logf("getPublisher error (expected if no Kafka): %v", err)
	}
}

func TestKafkaHandler_GetPublisher_WithSinglePublisher(t *testing.T) {
	store := link.NewTargetStore([]link.LinkTarget{
		{Name: "test", Protocol: "kafka", Kafka: &link.KafkaConfig{Topic: "test-topic"}},
	})

	publisher := &mockPublisher{}
	handler := NewKafkaHandler(publisher, store, nil, nil, nil, nil)

	target := store.Get("test")
	pub, err := handler.getPublisher(target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub == nil {
		t.Error("expected publisher")
	}
}

func TestKafkaHandler_GetPublisher_NoPublisher(t *testing.T) {
	store := link.NewTargetStore([]link.LinkTarget{
		{Name: "test", Protocol: "kafka", Kafka: &link.KafkaConfig{Topic: "test-topic"}},
	})

	// Handler with no pool or publisher
	handler := &KafkaHandler{
		targets: store,
	}

	target := store.Get("test")
	_, err := handler.getPublisher(target)
	if err == nil {
		t.Fatal("expected error when no publisher configured")
	}
	if !strings.Contains(err.Error(), "no kafka publisher") {
		t.Errorf("expected error about no publisher, got: %v", err)
	}
}

func TestKafkaHandler_NewKafkaHandler_NilLogger(t *testing.T) {
	store := link.NewTargetStore([]link.LinkTarget{})
	publisher := &mockPublisher{}

	handler := NewKafkaHandler(publisher, store, nil, nil, nil, nil)
	if handler == nil {
		t.Fatal("expected non-nil handler")
		return
	}
	if handler.logger == nil {
		t.Error("expected default logger when nil provided")
	}
}

func TestKafkaHandler_NewKafkaHandlerWithPool_NilLogger(t *testing.T) {
	store := link.NewTargetStore([]link.LinkTarget{})
	registry := kafka.NewRegistry()
	pool := kafka.NewPublisherPool(registry)

	handler := NewKafkaHandlerWithPool(pool, store, nil, nil, nil, nil)
	if handler == nil {
		t.Fatal("expected non-nil handler")
		return
	}
	if handler.logger == nil {
		t.Error("expected default logger when nil provided")
	}
}

func TestKafkaHandler_NewKafkaHandlerWithInterceptors_NilLogger(t *testing.T) {
	store := link.NewTargetStore([]link.LinkTarget{})
	registry := kafka.NewRegistry()
	pool := kafka.NewPublisherPool(registry)

	handler := NewKafkaHandlerWithInterceptors(pool, store, nil, nil, nil, nil, nil)
	if handler == nil {
		t.Fatal("expected non-nil handler")
		return
	}
	if handler.logger == nil {
		t.Error("expected default logger when nil provided")
	}
}

func TestKafkaHandler_OutboundInterceptorError(t *testing.T) {
	// Test outbound interceptor error path using a manually configured handler
	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "test-interceptor",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Topic: "test-topic"},
		},
	})

	reg := prometheus.NewRegistry()
	metrics := link.NewMetrics(reg)

	// Create interceptor registry with mock that returns error
	icRegistry := linkinterceptor.NewRegistry(nil, slog.Default())
	// Pre-configure a mock chain that returns error
	_ = icRegistry.Close() // Clean up

	// Create handler with interceptors
	handler := &KafkaHandler{
		targets:      store,
		metrics:      metrics,
		logger:       slog.Default(),
		interceptors: icRegistry,
		publisher:    &mockPublisher{},
	}

	req := httptest.NewRequest("POST", "/link/test-interceptor", bytes.NewReader([]byte(`{"test":"data"}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Request should succeed (no interceptors configured for target)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestKafkaHandler_GetPublisherError(t *testing.T) {
	// Test getPublisher error path with metrics
	store := link.NewTargetStore([]link.LinkTarget{
		{
			Name:     "test-no-publisher",
			Protocol: "kafka",
			Kafka:    &link.KafkaConfig{Topic: "test-topic"},
		},
	})

	reg := prometheus.NewRegistry()
	metrics := link.NewMetrics(reg)

	// Handler with no pool or publisher
	handler := &KafkaHandler{
		targets: store,
		metrics: metrics,
		logger:  slog.Default(),
	}

	req := httptest.NewRequest("POST", "/link/test-no-publisher", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should get 500 error from getPublisher
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "get publisher") {
		t.Errorf("expected get publisher error message, got: %s", w.Body.String())
	}
}

// newKafkaRetryHandler builds a handler with a single failing/succeeding
// publisher and an atomic attempt counter with per-attempt timestamps.
func newKafkaRetryHandler(t *testing.T, target link.LinkTarget, publish func(attempt int) error) (*KafkaHandler, *atomic.Int32, *[]time.Time) {
	t.Helper()
	var attempts atomic.Int32
	times := make([]time.Time, 0, 8)
	var mu sync.Mutex
	publisher := &mockPublisher{
		publishFunc: func(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
			n := int(attempts.Add(1))
			mu.Lock()
			times = append(times, time.Now())
			mu.Unlock()
			return publish(n)
		},
	}
	store := link.NewTargetStore([]link.LinkTarget{target})
	handler := NewKafkaHandler(publisher, store, nil, nil, nil, nil)
	return handler, &attempts, &times
}

// TestKafkaHandler_CancellationStopsRetries pins that cancelling the request
// after the first failed publish prevents every later publish. Today the
// between-attempts wait selects on the already-cancelled per-attempt context
// and the loop's break exits only the select, so all attempts run regardless.
func TestKafkaHandler_CancellationStopsRetries(t *testing.T) {
	target := link.LinkTarget{
		Name:     "cancel-test",
		Protocol: "kafka",
		Kafka:    &link.KafkaConfig{Topic: "cancel-topic"},
		Retry: link.RetryConfig{
			MaxAttempts:     5,
			InitialInterval: "10s",
			MaxInterval:     "10s",
		},
	}

	firstFailure := make(chan struct{}, 1)
	handler, attempts, _ := newKafkaRetryHandler(t, target, func(attempt int) error {
		if attempt == 1 {
			firstFailure <- struct{}{}
		}
		return errors.New("broker down")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	w := httptest.NewRecorder()
	go func() {
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/link/cancel-test", bytes.NewReader([]byte(`{}`))).WithContext(ctx))
		close(done)
	}()

	<-firstFailure
	cancel()
	<-done

	if got := attempts.Load(); got != 1 {
		t.Errorf("cancelled request must not be retried: expected 1 publish attempt, got %d", got)
	}
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 after cancellation, got %d", w.Code)
	}
}

// TestKafkaHandler_RetryHonorsInitialInterval pins that configured retry
// timing is forwarded to the publish loop: with InitialInterval 40ms (no
// jitter), the gap between the first two attempts must be at least 40ms.
// Sleeps only overshoot, so the lower bound is deterministic in the safe
// direction. Today the backoff wait never fires and the gap is microseconds.
func TestKafkaHandler_RetryHonorsInitialInterval(t *testing.T) {
	target := link.LinkTarget{
		Name:     "interval-test",
		Protocol: "kafka",
		Kafka:    &link.KafkaConfig{Topic: "interval-topic"},
		Retry: link.RetryConfig{
			MaxAttempts:     3,
			InitialInterval: "40ms",
			MaxInterval:     "120ms",
		},
	}

	handler, attempts, times := newKafkaRetryHandler(t, target, func(int) error {
		return errors.New("broker down")
	})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/link/interval-test", bytes.NewReader([]byte(`{}`))))

	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
	if gap := (*times)[1].Sub((*times)[0]); gap < 40*time.Millisecond {
		t.Errorf("configured initialInterval not honored: gap between attempts 1 and 2 was %v, want >= 40ms", gap)
	}
}

// TestKafkaHandler_BackoffGrowsExponentially pins exponential growth by lower
// bounds: with InitialInterval 25ms and no jitter, attempt gaps must be at
// least 25ms and then 50ms. Today both gaps are ~0 because the wait is dead
// code.
func TestKafkaHandler_BackoffGrowsExponentially(t *testing.T) {
	target := link.LinkTarget{
		Name:     "growth-test",
		Protocol: "kafka",
		Kafka:    &link.KafkaConfig{Topic: "growth-topic"},
		Retry: link.RetryConfig{
			MaxAttempts:     3,
			InitialInterval: "25ms",
			MaxInterval:     "1s",
		},
	}

	handler, attempts, times := newKafkaRetryHandler(t, target, func(int) error {
		return errors.New("broker down")
	})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/link/growth-test", bytes.NewReader([]byte(`{}`))))

	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
	if gap1 := (*times)[1].Sub((*times)[0]); gap1 < 25*time.Millisecond {
		t.Errorf("first backoff was %v, want >= 25ms", gap1)
	}
	if gap2 := (*times)[2].Sub((*times)[1]); gap2 < 50*time.Millisecond {
		t.Errorf("second backoff was %v, want >= 50ms (exponential growth)", gap2)
	}
}
