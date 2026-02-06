package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/link"
	"github.com/lsm/fiso/internal/link/circuitbreaker"
	"github.com/lsm/fiso/internal/link/ratelimit"
)

// KafkaHandler handles Kafka target publishing.
type KafkaHandler struct {
	publisher   dlq.Publisher
	targets     *link.TargetStore
	breakers    map[string]*circuitbreaker.Breaker
	rateLimiter *ratelimit.Limiter
	metrics     *link.Metrics
	logger      *slog.Logger
}

// NewKafkaHandler creates a new Kafka handler.
func NewKafkaHandler(publisher dlq.Publisher, targets *link.TargetStore,
	breakers map[string]*circuitbreaker.Breaker, rateLimiter *ratelimit.Limiter,
	metrics *link.Metrics, logger *slog.Logger) *KafkaHandler {

	if logger == nil {
		logger = slog.Default()
	}

	return &KafkaHandler{
		publisher:   publisher,
		targets:     targets,
		breakers:    breakers,
		rateLimiter: rateLimiter,
		metrics:     metrics,
		logger:      logger,
	}
}

// ServeHTTP handles Kafka publish requests.
// Route: POST /link/{targetName}
// Body: JSON payload to publish to Kafka
func (h *KafkaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse target name from URL: /link/{targetName}
	targetName := r.URL.Path[len("/link/"):]
	if targetName == "" {
		http.Error(w, "target name required", http.StatusBadRequest)
		return
	}

	target := h.targets.Get(targetName)
	if target == nil {
		http.Error(w, fmt.Sprintf("target %q not found", targetName), http.StatusNotFound)
		return
	}

	// Verify protocol is kafka
	if target.Protocol != "kafka" {
		http.Error(w, fmt.Sprintf("target %q is not a kafka target", targetName), http.StatusBadRequest)
		return
	}

	// Check circuit breaker
	if breaker, ok := h.breakers[target.Name]; ok {
		if err := breaker.Allow(); err != nil {
			if h.metrics != nil {
				h.metrics.CircuitState.WithLabelValues(target.Name).Set(float64(circuitbreaker.Open))
			}
			http.Error(w, "circuit breaker open", http.StatusServiceUnavailable)
			return
		}
	}

	// Check rate limit
	if h.rateLimiter != nil {
		if !h.rateLimiter.Allow(target.Name) {
			if h.metrics != nil {
				h.metrics.RateLimitedTotal.WithLabelValues(target.Name).Inc()
			}
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if h.metrics != nil {
			h.metrics.RequestsTotal.WithLabelValues(target.Name, "POST", "400", "kafka").Inc()
		}
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	// Build Kafka headers from HTTP headers + static headers
	kafkaHeaders := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			// Normalize well-known header names to their conventional casing
			// since http.Header canonicalizes them (e.g., X-Request-Id instead of X-Request-ID)
			normalizedKey := normalizeHeaderKey(k)
			kafkaHeaders[normalizedKey] = v[0]
		}
	}
	// Add static headers from config
	if target.Kafka != nil {
		for k, v := range target.Kafka.Headers {
			kafkaHeaders[k] = v
		}
	}

	// Generate key
	keyStrategy := link.KeyStrategy{}
	if target.Kafka != nil {
		keyStrategy = target.Kafka.Key
	}
	key, err := h.generateKey(keyStrategy, body, r.Header)
	if err != nil {
		if h.metrics != nil {
			h.metrics.RequestsTotal.WithLabelValues(target.Name, "POST", "400", "kafka").Inc()
		}
		http.Error(w, fmt.Sprintf("key generation: %v", err), http.StatusBadRequest)
		return
	}

	// Publish to Kafka with retry
	var publishErr error
	maxRetries := 3
	if target.Retry.MaxAttempts > 0 {
		maxRetries = target.Retry.MaxAttempts
	}

	// Get topic
	topic := "default-topic"
	if target.Kafka != nil {
		topic = target.Kafka.Topic
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Use request context with timeout for safety
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		publishErr = h.publisher.Publish(ctx, topic, key, body, kafkaHeaders)
		cancel()
		if publishErr == nil {
			// Success
			if breaker, ok := h.breakers[target.Name]; ok {
				breaker.RecordSuccess()
			}
			if h.metrics != nil {
				h.metrics.RequestsTotal.WithLabelValues(target.Name, "POST", "200", "kafka").Inc()
			}
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"status":"published","topic":"%s"}`, topic)
			return
		}

		// Retry on error
		if attempt < maxRetries-1 {
			backoff := time.Duration(attempt+1) * 100 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				publishErr = ctx.Err()
				break
			}
		}
	}

	// All retries failed
	if breaker, ok := h.breakers[target.Name]; ok {
		breaker.RecordFailure()
	}
	if h.metrics != nil {
		h.metrics.RequestsTotal.WithLabelValues(target.Name, "POST", "502", "kafka").Inc()
	}
	http.Error(w, fmt.Sprintf("kafka publish: %v", publishErr), http.StatusBadGateway)
}

// generateKey generates a Kafka message key based on strategy.
func (h *KafkaHandler) generateKey(strategy link.KeyStrategy, body []byte, headers http.Header) ([]byte, error) {
	// Default: no key
	if strategy.Type == "" {
		return nil, nil
	}

	switch strategy.Type {
	case "uuid":
		return []byte(uuid.New().String()), nil

	case "header":
		values := headers.Values(strategy.Field)
		if len(values) == 0 {
			return nil, fmt.Errorf("header %q not found", strategy.Field)
		}
		return []byte(values[0]), nil

	case "payload":
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("parse payload: %w", err)
		}
		val, ok := payload[strategy.Field]
		if !ok {
			return nil, fmt.Errorf("field %q not found in payload", strategy.Field)
		}
		return []byte(fmt.Sprintf("%v", val)), nil

	case "static":
		return []byte(strategy.Value), nil

	case "random":
		return []byte(fmt.Sprintf("%d", time.Now().UnixNano())), nil

	default:
		return nil, fmt.Errorf("unknown key type: %s", strategy.Type)
	}
}

// normalizeHeaderKey normalizes HTTP canonical header keys back to their conventional form.
// Go's http.Header canonicalizes header names (e.g., "X-Request-ID" becomes "X-Request-Id"),
// but for Kafka headers we want to preserve the conventional casing that users expect.
func normalizeHeaderKey(key string) string {
	// Map of canonical forms to conventional forms for well-known headers
	wellKnownHeaders := map[string]string{
		"X-Request-Id":      "X-Request-ID",
		"X-Correlation-Id":  "X-Correlation-ID",
		"X-Trace-Id":        "X-Trace-ID",
		"X-Span-Id":         "X-Span-ID",
		"X-Session-Id":      "X-Session-ID",
		"X-User-Id":         "X-User-ID",
		"X-Client-Id":       "X-Client-ID",
		"X-Api-Key":         "X-API-Key",
		"X-Forwarded-For":   "X-Forwarded-For",
		"X-Forwarded-Proto": "X-Forwarded-Proto",
		"X-Forwarded-Host":  "X-Forwarded-Host",
	}

	if normalized, ok := wellKnownHeaders[key]; ok {
		return normalized
	}
	return key
}
