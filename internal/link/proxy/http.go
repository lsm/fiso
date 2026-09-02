package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/lsm/fiso/internal/correlation"
	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/interceptor"
	"github.com/lsm/fiso/internal/kafka"
	"github.com/lsm/fiso/internal/link"
	"github.com/lsm/fiso/internal/link/auth"
	"github.com/lsm/fiso/internal/link/circuitbreaker"
	"github.com/lsm/fiso/internal/link/discovery"
	linkinterceptor "github.com/lsm/fiso/internal/link/interceptor"
	"github.com/lsm/fiso/internal/link/ratelimit"
	"github.com/lsm/fiso/internal/link/retry"
	"github.com/lsm/fiso/internal/tracing"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Handler is the HTTP forward proxy for Fiso-Link.
type Handler struct {
	targets      *link.TargetStore
	breakers     map[string]*circuitbreaker.Breaker
	rateLimiter  *ratelimit.Limiter
	auth         auth.Provider
	resolver     discovery.Resolver
	metrics      *link.Metrics
	client       *http.Client
	logger       *slog.Logger
	kafkaHandler *KafkaHandler // Optional: For Kafka targets
	tracer       trace.Tracer
	interceptors *linkinterceptor.Registry // Interceptor registry
}

// Config configures the proxy handler.
type Config struct {
	Targets        *link.TargetStore
	Breakers       map[string]*circuitbreaker.Breaker
	RateLimiter    *ratelimit.Limiter
	Auth           auth.Provider
	Resolver       discovery.Resolver
	Metrics        *link.Metrics
	Logger         *slog.Logger
	KafkaPublisher dlq.Publisher             // Deprecated: use KafkaPool instead
	KafkaRegistry  *kafka.Registry           // Named Kafka cluster registry
	KafkaPool      *kafka.PublisherPool      // Kafka publisher connection pool
	Interceptors   *linkinterceptor.Registry // Interceptor registry
}

// NewHandler creates a new HTTP proxy handler.
func NewHandler(cfg Config) *Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Resolver == nil {
		cfg.Resolver = &discovery.StaticResolver{}
	}
	if cfg.Auth == nil {
		cfg.Auth = &auth.NoopProvider{}
	}

	h := &Handler{
		targets:     cfg.Targets,
		breakers:    cfg.Breakers,
		rateLimiter: cfg.RateLimiter,
		auth:        cfg.Auth,
		resolver:    cfg.Resolver,
		metrics:     cfg.Metrics,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		logger:       cfg.Logger,
		tracer:       noop.NewTracerProvider().Tracer("proxy-handler"),
		interceptors: cfg.Interceptors,
	}

	// Initialize Kafka handler if pool or publisher provided
	if cfg.KafkaPool != nil {
		h.kafkaHandler = NewKafkaHandlerWithInterceptors(
			cfg.KafkaPool,
			cfg.Targets,
			cfg.Breakers,
			cfg.RateLimiter,
			cfg.Metrics,
			cfg.Logger,
			cfg.Interceptors,
		)
	} else if cfg.KafkaPublisher != nil {
		// Backwards compatibility: single publisher
		h.kafkaHandler = NewKafkaHandler(
			cfg.KafkaPublisher,
			cfg.Targets,
			cfg.Breakers,
			cfg.RateLimiter,
			cfg.Metrics,
			cfg.Logger,
		)
	}

	return h
}

// SetTracer sets the tracer for the handler.
func (h *Handler) SetTracer(tracer trace.Tracer) {
	h.tracer = tracer
}

// ServeHTTP handles proxy requests. Routes:
//   - /link/{targetName}/{path...}  — sync forward proxy
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Extract correlation ID from incoming request headers
	headers := make(map[string]string)
	for k, vv := range r.Header {
		if len(vv) > 0 {
			headers[k] = vv[0]
		}
	}
	corrID := correlation.ExtractOrGenerate(headers)

	// Extract trace context from incoming request
	ctx := correlation.ExtractTraceContext(r.Context(), headers)

	// Add correlation ID to response headers
	w.Header().Set(correlation.HeaderCorrelationID, corrID.Value)

	// Parse route: /link/{targetName}/{path...}
	trimmed := strings.TrimPrefix(r.URL.Path, "/link/")
	if trimmed == r.URL.Path {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	parts := strings.SplitN(trimmed, "/", 2)
	targetName := parts[0]
	proxyPath := "/"
	if len(parts) > 1 {
		proxyPath = "/" + parts[1]
	}

	target := h.targets.Get(targetName)
	if target == nil {
		http.Error(w, fmt.Sprintf("target %q not found", targetName), http.StatusNotFound)
		return
	}

	// Protocol-based routing: Kafka targets use special handler
	if target.Protocol == "kafka" {
		if h.kafkaHandler == nil {
			http.Error(w, "kafka targets not supported (no publisher configured)", http.StatusNotImplemented)
			return
		}
		h.kafkaHandler.ServeHTTP(w, r)
		return
	}

	// Start span for proxy request
	ctx, span := tracing.StartSpan(ctx, h.tracer, tracing.SpanProxyRequest,
		trace.WithAttributes(
			tracing.TargetNameAttr(targetName),
			tracing.HTTPMethodAttr(r.Method),
			tracing.CorrelationAttr(corrID.Value),
		),
	)
	defer span.End()

	// Check allowed paths
	if !h.isPathAllowed(target, proxyPath) {
		http.Error(w, "path not allowed", http.StatusForbidden)
		return
	}

	// Check circuit breaker
	if breaker, ok := h.breakers[targetName]; ok {
		if err := breaker.Allow(); err != nil {
			if h.metrics != nil {
				h.metrics.CircuitState.WithLabelValues(targetName).Set(float64(circuitbreaker.Open))
			}
			w.Header().Set("Retry-After", "30")
			http.Error(w, "service unavailable (circuit open)", http.StatusServiceUnavailable)
			return
		}
	}

	// Check rate limit
	if h.rateLimiter != nil && !h.rateLimiter.Allow(targetName) {
		if h.metrics != nil {
			h.metrics.RateLimitedTotal.WithLabelValues(targetName).Inc()
		}
		w.Header().Set("Retry-After", "1")
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Resolve host
	resolvedHost, err := h.resolver.Resolve(ctx, target.Host)
	if err != nil {
		tracing.SetSpanError(span, err)
		h.logger.Error("resolve error", "target", targetName, "error", err)
		http.Error(w, "failed to resolve host", http.StatusBadGateway)
		return
	}

	// Get auth credentials
	creds, err := h.auth.GetCredentials(ctx, targetName)
	if err != nil {
		tracing.SetSpanError(span, err)
		h.logger.Error("auth error", "target", targetName, "error", err)
		http.Error(w, "auth error", http.StatusInternalServerError)
		return
	}

	// Read request body for interceptor processing
	var requestBody []byte
	if r.Body != nil {
		requestBody, err = io.ReadAll(r.Body)
		if err != nil {
			h.logger.Error("read request body error", "target", targetName, "error", err)
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()
	}

	// Run outbound interceptors (before upstream request). Bodyless
	// requests run too — an authentication module must be able to refuse a
	// GET, and the envelope carries a null payload for empty bodies
	// (ADR 0007).
	if h.interceptors != nil {
		outboundHeaders := make(map[string]string)
		for k, vv := range r.Header {
			if len(vv) > 0 {
				outboundHeaders[k] = vv[0]
			}
		}

		icReq := &interceptor.Request{
			Payload:   requestBody,
			Headers:   outboundHeaders,
			Direction: interceptor.Outbound,
		}

		icResult, icErr := h.interceptors.ProcessOutbound(ctx, targetName, icReq)
		if icErr != nil {
			// A rejection answers with the guest-chosen status instead of a
			// blanket 500 (ADR 0007). Record it like any other request so
			// authentication verdicts appear on the status dashboards.
			if rej, ok := interceptor.AsRejection(icErr); ok {
				h.logger.Warn("request rejected by outbound interceptor",
					"target", targetName,
					"correlation_id", corrID.Value,
					"status", rej.Status,
					"reason", rej.Reason,
				)
				span.SetAttributes(tracing.HTTPStatusAttr(rej.Status))
				tracing.SetSpanError(span, rej)
				h.recordSyncRequest(targetName, r.Method, strconv.Itoa(rej.Status), time.Since(start).Seconds())
				// Emit the same completion record as the success path so
				// rejections join correlation-aware request logs.
				h.logger.Info("proxy request completed",
					"correlation_id", corrID.Value,
					"target", targetName,
					"method", r.Method,
					"status", rej.Status,
					"latency_ms", time.Since(start).Milliseconds(),
				)
				http.Error(w, rej.Reason, rej.Status)
				return
			}
			h.logger.Error("outbound interceptor error", "target", targetName, "error", icErr)
			http.Error(w, "interceptor error", http.StatusInternalServerError)
			return
		}

		requestBody = icResult.Payload
		// Update headers from interceptor result
		for k, v := range icResult.Headers {
			r.Header.Set(k, v)
		}
	}

	// Build upstream URL
	scheme := target.Protocol
	if scheme == "" {
		scheme = "https"
	}

	upstreamHost := resolvedHost
	if target.Port > 0 && !hasExplicitPort(resolvedHost) {
		upstreamHost = fmt.Sprintf("%s:%d", resolvedHost, target.Port)
	}

	upstreamPath := joinUpstreamPath(target.BasePath, proxyPath)
	upstreamURL := fmt.Sprintf("%s://%s%s", scheme, upstreamHost, upstreamPath)
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	span.SetAttributes(tracing.HTTPTargetAttr(upstreamURL))

	// Execute with retry
	var resp *http.Response
	retryCfg := buildRetryConfig(target)

	retryErr := retry.Do(ctx, retryCfg, func() error {
		// A retried attempt must not leak its predecessor's body; drain it
		// to EOF before closing so the keep-alive connection stays reusable,
		// then issue the next request. The final attempt's body stays open
		// so the error response can be forwarded — and seen by the inbound
		// interceptors (ADR 0007).
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			resp = nil
		}
		req, reqErr := http.NewRequestWithContext(ctx, r.Method, upstreamURL, bytes.NewReader(requestBody))
		if reqErr != nil {
			return retry.Permanent(reqErr)
		}

		// Copy original headers
		for k, vv := range r.Header {
			for _, v := range vv {
				req.Header.Add(k, v)
			}
		}

		// Add correlation ID to upstream request
		req.Header.Set(correlation.HeaderCorrelationID, corrID.Value)

		// Inject trace context into headers
		outboundHeaders := make(map[string]string)
		for k, vv := range req.Header {
			if len(vv) > 0 {
				outboundHeaders[k] = vv[0]
			}
		}
		correlation.InjectTraceContext(ctx, outboundHeaders)
		for k, v := range outboundHeaders {
			req.Header.Set(k, v)
		}

		// Inject auth headers
		if creds != nil {
			for k, v := range creds.Headers {
				req.Header.Set(k, v)
			}
		}

		var doErr error
		resp, doErr = h.client.Do(req)
		if doErr != nil {
			return doErr
		}

		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return fmt.Errorf("upstream returned %d", resp.StatusCode)
		}
		if resp.StatusCode >= 400 {
			return retry.Permanent(fmt.Errorf("upstream returned %d", resp.StatusCode))
		}
		return nil
	})

	// Record circuit breaker outcome
	if breaker, ok := h.breakers[targetName]; ok {
		if retryErr != nil {
			breaker.RecordFailure()
		} else {
			breaker.RecordSuccess()
		}
		if h.metrics != nil {
			h.metrics.CircuitState.WithLabelValues(targetName).Set(float64(breaker.State()))
		}
	}

	duration := time.Since(start).Seconds()

	if retryErr != nil {
		tracing.SetSpanError(span, retryErr)
		if resp != nil {
			// Forward the error response from upstream. When the target has
			// an inbound chain it routes through interception so a policy
			// module can also refuse error bodies (ADR 0007); without one,
			// stream the body through unbuffered rather than reading it all
			// into memory just to discover there is nothing to process.
			if h.hasInboundChain(targetName) {
				finalStatus := h.copyResponseWithInterceptors(ctx, w, resp, targetName)
				span.SetAttributes(tracing.HTTPStatusAttr(finalStatus))
				h.recordSyncRequest(targetName, r.Method, strconv.Itoa(finalStatus), duration)
				// The rejection verdict replaced the proxy-error record;
				// keep the correlation-aware completion trail.
				h.logger.Info("proxy request completed",
					"correlation_id", corrID.Value,
					"target", targetName,
					"method", r.Method,
					"status", finalStatus,
					"latency_ms", time.Since(start).Milliseconds(),
				)
				return
			}
			defer func() { _ = resp.Body.Close() }()
			for k, vv := range resp.Header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
			span.SetAttributes(tracing.HTTPStatusAttr(resp.StatusCode))
			h.recordSyncRequest(targetName, r.Method, strconv.Itoa(resp.StatusCode), duration)
			return
		}
		span.SetAttributes(tracing.HTTPStatusAttr(http.StatusBadGateway))
		// Label with the 502 the caller actually receives — the status
		// dashboards must count transport failures under the response code.
		h.recordSyncRequest(targetName, r.Method, strconv.Itoa(http.StatusBadGateway), duration)
		h.logger.Error("proxy error", "target", targetName, "correlation_id", corrID.Value, "error", retryErr)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	// The final caller-visible status is only known after inbound
	// interception: a guest can turn an upstream 200 into a 401 (ADR 0007),
	// and metrics/tracing must report what the caller saw.
	finalStatus := h.copyResponseWithInterceptors(ctx, w, resp, targetName)
	span.SetAttributes(tracing.HTTPStatusAttr(finalStatus))
	h.recordSyncRequest(targetName, r.Method, strconv.Itoa(finalStatus), duration)
	if finalStatus >= 400 {
		// A guest-refused or failed-to-forward response is not a successful
		// hop, even when the upstream call itself succeeded.
		tracing.SetSpanError(span, fmt.Errorf("final status %d", finalStatus))
	} else {
		tracing.SetSpanOK(span)
	}

	// Log successful proxy request
	h.logger.Info("proxy request completed",
		"correlation_id", corrID.Value,
		"target", targetName,
		"method", r.Method,
		"status", finalStatus,
		"latency_ms", time.Since(start).Milliseconds(),
	)
}

// recordSyncRequest records the request counter and duration for the
// synchronous HTTP path. status is the status label; pass "error" when no
// response exists.
func (h *Handler) recordSyncRequest(targetName, method, status string, durationSeconds float64) {
	if h.metrics == nil {
		return
	}
	h.metrics.RequestsTotal.WithLabelValues(targetName, method, status, "sync").Inc()
	h.metrics.RequestDuration.WithLabelValues(targetName, method).Observe(durationSeconds)
}

// hasInboundChain reports whether the target has inbound interceptors that
// response forwarding must route through.
func (h *Handler) hasInboundChain(targetName string) bool {
	if h.interceptors == nil {
		return false
	}
	chains := h.interceptors.GetChains(targetName)
	return chains != nil && chains.Inbound != nil && chains.Inbound.Len() > 0
}

// copyResponseWithInterceptors copies the response to the writer, optionally
// running inbound interceptors, and returns the final caller-visible status:
// the upstream's, or the guest's when an inbound interceptor rewrites it.
func (h *Handler) copyResponseWithInterceptors(ctx context.Context, w http.ResponseWriter, resp *http.Response, targetName string) int {
	defer func() { _ = resp.Body.Close() }()

	// Read response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.Error("read response body error", "target", targetName, "error", err)
		http.Error(w, "failed to read response", http.StatusInternalServerError)
		return http.StatusInternalServerError
	}

	// Run inbound interceptors (after upstream response)
	if h.interceptors != nil && len(responseBody) > 0 {
		inboundHeaders := make(map[string]string)
		for k, vv := range resp.Header {
			if len(vv) > 0 {
				inboundHeaders[k] = vv[0]
			}
		}

		icReq := &interceptor.Request{
			Payload:   responseBody,
			Headers:   inboundHeaders,
			Direction: interceptor.Inbound,
		}

		icResult, icErr := h.interceptors.ProcessInbound(ctx, targetName, icReq)
		if icErr != nil {
			// A rejection answers with the guest-chosen status instead of a
			// blanket 500 (ADR 0007).
			if rej, ok := interceptor.AsRejection(icErr); ok {
				h.logger.Warn("response rejected by inbound interceptor",
					"target", targetName,
					"status", rej.Status,
					"reason", rej.Reason,
				)
				http.Error(w, rej.Reason, rej.Status)
				return rej.Status
			}
			h.logger.Error("inbound interceptor error", "target", targetName, "error", icErr)
			http.Error(w, "interceptor error", http.StatusInternalServerError)
			return http.StatusInternalServerError
		}

		responseBody = icResult.Payload
		// Update headers from interceptor result
		for k, v := range icResult.Headers {
			resp.Header.Set(k, v)
		}
	}

	// Copy headers. The body's length is what we have now — interception
	// may have changed it — so recompute Content-Length instead of
	// forwarding the upstream's declaration (a mismatched length makes
	// net/http reject larger bodies as exceeding the declared length and
	// delivers smaller ones with an unexpected EOF).
	resp.Header.Set("Content-Length", strconv.Itoa(len(responseBody)))
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(responseBody)
	return resp.StatusCode
}

func (h *Handler) isPathAllowed(target *link.LinkTarget, reqPath string) bool {
	if len(target.AllowedPaths) == 0 {
		return true
	}
	for _, pattern := range target.AllowedPaths {
		matched, err := path.Match(pattern, reqPath)
		if err == nil && matched {
			return true
		}
		// Support ** suffix: /api/v2/** matches /api/v2/anything/nested
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if strings.HasPrefix(reqPath, prefix+"/") || reqPath == prefix {
				return true
			}
		}
	}
	return false
}

// buildRetryConfig converts target retry settings to the shared retry
// engine configuration. Package-level so the Kafka path shares it exactly.
func buildRetryConfig(target *link.LinkTarget) retry.Config {
	cfg := retry.DefaultConfig()
	if target.Retry.MaxAttempts > 0 {
		cfg.MaxAttempts = target.Retry.MaxAttempts
	}
	if target.Retry.Jitter > 0 {
		cfg.Jitter = target.Retry.Jitter
	}
	if d, err := time.ParseDuration(target.Retry.InitialInterval); err == nil {
		cfg.InitialInterval = d
	}
	if d, err := time.ParseDuration(target.Retry.MaxInterval); err == nil {
		cfg.MaxInterval = d
	}
	return cfg
}

func hasExplicitPort(host string) bool {
	if strings.HasPrefix(host, "[") {
		_, _, err := net.SplitHostPort(host)
		return err == nil
	}

	if strings.Count(host, ":") == 0 {
		return false
	}
	if strings.Count(host, ":") == 1 {
		_, _, err := net.SplitHostPort(host)
		return err == nil
	}

	// Raw IPv6 literal without brackets.
	return false
}

func joinUpstreamPath(basePath, proxyPath string) string {
	if proxyPath == "" {
		proxyPath = "/"
	}
	if !strings.HasPrefix(proxyPath, "/") {
		proxyPath = "/" + proxyPath
	}

	if basePath == "" || basePath == "/" {
		return proxyPath
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}

	if proxyPath == "/" {
		return strings.TrimRight(basePath, "/")
	}
	return strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(proxyPath, "/")
}
