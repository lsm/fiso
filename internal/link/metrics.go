package link

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds Fiso-Link Prometheus metrics.
type Metrics struct {
	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	CircuitState     *prometheus.GaugeVec
	RetriesTotal     *prometheus.CounterVec
	AuthRefreshTotal *prometheus.CounterVec
	RateLimitedTotal *prometheus.CounterVec
	// Interceptor metrics
	InterceptorInvocations *prometheus.CounterVec
	InterceptorDuration    *prometheus.HistogramVec
	InterceptorErrors      *prometheus.CounterVec
}

// NewMetrics registers and returns Fiso-Link metrics.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	f := promauto.With(reg)
	return &Metrics{
		RequestsTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "fiso_link_requests_total",
			Help: "Total requests proxied by Fiso-Link.",
		}, []string{"target", "method", "status", "mode"}),
		RequestDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "fiso_link_request_duration_seconds",
			Help:    "Request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"target", "method"}),
		CircuitState: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "fiso_link_circuit_state",
			Help: "Circuit breaker state (0=closed, 1=half-open, 2=open).",
		}, []string{"target"}),
		RetriesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "fiso_link_retries_total",
			Help: "Total retries per target.",
		}, []string{"target", "attempt"}),
		AuthRefreshTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "fiso_link_auth_refresh_total",
			Help: "Total auth credential refreshes.",
		}, []string{"target", "status"}),
		RateLimitedTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "fiso_link_rate_limited_total",
			Help: "Total requests rejected by rate limiting.",
		}, []string{"target"}),
		// Interceptor metrics
		InterceptorInvocations: f.NewCounterVec(prometheus.CounterOpts{
			Name: "fiso_link_interceptor_invocations_total",
			Help: "Total interceptor invocations.",
		}, []string{"target", "module", "phase", "success"}),
		InterceptorDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "fiso_link_interceptor_duration_seconds",
			Help:    "Interceptor invocation duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"target", "module", "phase"}),
		InterceptorErrors: f.NewCounterVec(prometheus.CounterOpts{
			Name: "fiso_link_interceptor_errors_total",
			Help: "Total interceptor errors.",
		}, []string{"target", "module", "phase"}),
	}
}

// RecordInterceptorInvocation records metrics for an interceptor invocation.
func (m *Metrics) RecordInterceptorInvocation(target, module, phase string, success bool, durationSeconds float64) {
	if m == nil {
		return
	}
	m.InterceptorInvocations.WithLabelValues(target, module, phase, boolString(success)).Inc()
	m.InterceptorDuration.WithLabelValues(target, module, phase).Observe(durationSeconds)
	if !success {
		m.InterceptorErrors.WithLabelValues(target, module, phase).Inc()
	}
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
