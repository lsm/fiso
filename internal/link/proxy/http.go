package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/link"
	"github.com/lsm/fiso/internal/link/auth"
	"github.com/lsm/fiso/internal/link/circuitbreaker"
	"github.com/lsm/fiso/internal/link/discovery"
	"github.com/lsm/fiso/internal/link/ratelimit"
	"github.com/lsm/fiso/internal/link/retry"
)

// Handler is the HTTP forward proxy for Fiso-Link.
type Handler struct {
	targets     *link.TargetStore
	breakers    map[string]*circuitbreaker.Breaker
	rateLimiter *ratelimit.Limiter
	auth        auth.Provider
	resolver    discovery.Resolver
	metrics     *link.Metrics
	client      *http.Client
	logger      *slog.Logger
	kafkaHandler *KafkaHandler // Optional: For Kafka targets
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
	KafkaPublisher dlq.Publisher // Optional: For Kafka targets
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
			Timeout: 30 * time.Second,
		},
		logger: cfg.Logger,
	}

	// Initialize Kafka handler if publisher provided
	if cfg.KafkaPublisher != nil {
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

// ServeHTTP handles proxy requests. Routes:
//   - /link/{targetName}/{path...}  — sync forward proxy
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

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
	resolvedHost, err := h.resolver.Resolve(r.Context(), target.Host)
	if err != nil {
		h.logger.Error("resolve error", "target", targetName, "error", err)
		http.Error(w, "failed to resolve host", http.StatusBadGateway)
		return
	}

	// Get auth credentials
	creds, err := h.auth.GetCredentials(r.Context(), targetName)
	if err != nil {
		h.logger.Error("auth error", "target", targetName, "error", err)
		http.Error(w, "auth error", http.StatusInternalServerError)
		return
	}

	// Build upstream URL
	scheme := target.Protocol
	if scheme == "" {
		scheme = "https"
	}
	upstreamURL := fmt.Sprintf("%s://%s%s", scheme, resolvedHost, proxyPath)
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	// Execute with retry
	var resp *http.Response
	retryCfg := h.buildRetryConfig(target)

	retryErr := retry.Do(r.Context(), retryCfg, func() error {
		req, reqErr := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
		if reqErr != nil {
			return retry.Permanent(reqErr)
		}

		// Copy original headers
		for k, vv := range r.Header {
			for _, v := range vv {
				req.Header.Add(k, v)
			}
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
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return fmt.Errorf("upstream returned %d", resp.StatusCode)
		}
		if resp.StatusCode >= 400 {
			return retry.Permanent(fmt.Errorf("upstream returned %d", resp.StatusCode))
		}
		return nil
	})

	// Record metrics
	duration := time.Since(start).Seconds()
	status := "error"
	if resp != nil {
		status = strconv.Itoa(resp.StatusCode)
	}
	if h.metrics != nil {
		h.metrics.RequestsTotal.WithLabelValues(targetName, r.Method, status, "sync").Inc()
		h.metrics.RequestDuration.WithLabelValues(targetName, r.Method).Observe(duration)
	}

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

	if retryErr != nil {
		if resp != nil {
			// Forward the error response from upstream
			h.copyResponse(w, resp)
			return
		}
		h.logger.Error("proxy error", "target", targetName, "error", retryErr)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	h.copyResponse(w, resp)
}

func (h *Handler) copyResponse(w http.ResponseWriter, resp *http.Response) {
	defer func() { _ = resp.Body.Close() }()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
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

func (h *Handler) buildRetryConfig(target *link.LinkTarget) retry.Config {
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
