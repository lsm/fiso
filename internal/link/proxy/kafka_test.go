package proxy

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
