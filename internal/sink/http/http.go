package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/lsm/fiso/internal/correlation"
	"github.com/lsm/fiso/internal/tracing"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// RetryConfig controls retry behavior for failed deliveries.
type RetryConfig struct {
	MaxAttempts     int
	InitialInterval time.Duration
	MaxInterval     time.Duration
}

// Config holds the configuration for an HTTP sink.
type Config struct {
	URL     string
	Method  string
	Headers map[string]string
	Retry   RetryConfig
}

// Sink delivers events to an HTTP endpoint.
type Sink struct {
	client *http.Client
	config Config
	logger *slog.Logger
	tracer trace.Tracer
}

// NewSink creates a new HTTP sink.
func NewSink(cfg Config) (*Sink, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if cfg.Method == "" {
		cfg.Method = "POST"
	}
	if cfg.Retry.MaxAttempts <= 0 {
		cfg.Retry.MaxAttempts = 1
	}
	if cfg.Retry.InitialInterval <= 0 {
		cfg.Retry.InitialInterval = 200 * time.Millisecond
	}
	if cfg.Retry.MaxInterval <= 0 {
		cfg.Retry.MaxInterval = 30 * time.Second
	}

	return &Sink{
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		config: cfg,
		logger: slog.Default(),
		tracer: noop.NewTracerProvider().Tracer("http-sink"),
	}, nil
}

// SetTracer sets the tracer for the sink.
func (s *Sink) SetTracer(tracer trace.Tracer) {
	s.tracer = tracer
}

// Deliver sends the event payload to the configured HTTP endpoint.
func (s *Sink) Deliver(ctx context.Context, event []byte, headers map[string]string) error {
	start := time.Now()

	// Extract correlation ID from headers
	corrID := correlation.ExtractOrGenerate(headers)

	// Start span for delivery
	ctx, span := tracing.StartSpan(ctx, s.tracer, tracing.SpanHTTPDeliver,
		trace.WithAttributes(
			tracing.HTTPTargetAttr(s.config.URL),
			tracing.HTTPMethodAttr(s.config.Method),
			tracing.CorrelationAttr(corrID.Value),
		),
	)
	defer span.End()

	var lastErr error

	for attempt := 0; attempt < s.config.Retry.MaxAttempts; attempt++ {
		if attempt > 0 {
			delay := s.backoff(attempt)
			select {
			case <-ctx.Done():
				tracing.SetSpanError(span, ctx.Err())
				return fmt.Errorf("context cancelled during retry backoff: %w", ctx.Err())
			case <-time.After(delay):
			}
		}

		err := s.doRequest(ctx, event, headers)
		if err == nil {
			tracing.SetSpanOK(span)
			s.logger.Info("event delivered",
				"correlation_id", corrID.Value,
				"target", s.config.URL,
				"latency_ms", time.Since(start).Milliseconds(),
			)
			return nil
		}
		lastErr = err

		// Don't retry on permanent errors (4xx except 429)
		if isPermanent(err) {
			tracing.SetSpanError(span, err)
			s.logger.Error("delivery failed",
				"correlation_id", corrID.Value,
				"target", s.config.URL,
				"error", err,
			)
			return err
		}
	}

	tracing.SetSpanError(span, lastErr)
	s.logger.Error("delivery failed after retries",
		"correlation_id", corrID.Value,
		"target", s.config.URL,
		"attempts", s.config.Retry.MaxAttempts,
		"error", lastErr,
	)
	return fmt.Errorf("delivery failed after %d attempts: %w", s.config.Retry.MaxAttempts, lastErr)
}

// Close releases resources held by the sink.
func (s *Sink) Close() error {
	s.client.CloseIdleConnections()
	return nil
}

func (s *Sink) doRequest(ctx context.Context, event []byte, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, s.config.Method, s.config.URL, bytes.NewReader(event))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// Apply static headers from config first
	for k, v := range s.config.Headers {
		req.Header.Set(k, v)
	}
	// Apply per-event headers (can override static)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Inject trace context into headers for propagation
	correlation.InjectTraceContext(ctx, headers)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	return &StatusError{Code: resp.StatusCode}
}

func (s *Sink) backoff(attempt int) time.Duration {
	base := float64(s.config.Retry.InitialInterval) * math.Pow(2, float64(attempt-1))
	if base > float64(s.config.Retry.MaxInterval) {
		base = float64(s.config.Retry.MaxInterval)
	}
	// Add ±20% jitter
	jitter := base * 0.2 * (2*rand.Float64() - 1)
	return time.Duration(base + jitter)
}

// StatusError represents an HTTP response with a non-2xx status code.
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("http status %d", e.Code)
}

// isPermanent returns true for client errors (4xx) except 429 Too Many Requests.
func isPermanent(err error) bool {
	se, ok := err.(*StatusError)
	if !ok {
		return false
	}
	return se.Code >= 400 && se.Code < 500 && se.Code != 429
}
