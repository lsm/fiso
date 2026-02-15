package link

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewMetrics_RegistersWithoutPanic(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	if m.RequestsTotal == nil {
		t.Error("RequestsTotal not initialized")
	}
	if m.RequestDuration == nil {
		t.Error("RequestDuration not initialized")
	}
	if m.CircuitState == nil {
		t.Error("CircuitState not initialized")
	}
	if m.RetriesTotal == nil {
		t.Error("RetriesTotal not initialized")
	}
	if m.AuthRefreshTotal == nil {
		t.Error("AuthRefreshTotal not initialized")
	}
	if m.RateLimitedTotal == nil {
		t.Error("RateLimitedTotal not initialized")
	}
}

func TestMetrics_IncrementCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RequestsTotal.WithLabelValues("crm", "GET", "200", "sync").Inc()
	m.RetriesTotal.WithLabelValues("crm", "1").Inc()
	m.AuthRefreshTotal.WithLabelValues("crm", "success").Inc()
	m.RateLimitedTotal.WithLabelValues("crm").Inc()
	m.CircuitState.WithLabelValues("crm").Set(0)
	m.RequestDuration.WithLabelValues("crm", "GET").Observe(0.5)

	// Gather to verify
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mfs) != 6 {
		t.Errorf("expected 6 metric families, got %d", len(mfs))
	}
}

func TestMetrics_RecordInterceptorInvocation_Success(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Record successful invocation
	m.RecordInterceptorInvocation("test-target", "test.wasm", "outbound", true, 0.123)

	// Gather to verify metrics were recorded
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have interceptor metrics registered
	foundInvocations := false
	for _, mf := range mfs {
		if mf.GetName() == "fiso_link_interceptor_invocations_total" {
			foundInvocations = true
			break
		}
	}
	if !foundInvocations {
		t.Error("expected interceptor invocations metric to be registered")
	}
}

func TestMetrics_RecordInterceptorInvocation_Error(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Record failed invocation
	m.RecordInterceptorInvocation("test-target", "test.wasm", "inbound", false, 0.056)

	// Gather to verify metrics were recorded
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have interceptor error metrics
	foundErrors := false
	for _, mf := range mfs {
		if mf.GetName() == "fiso_link_interceptor_errors_total" {
			foundErrors = true
			break
		}
	}
	if !foundErrors {
		t.Error("expected interceptor errors metric to be registered")
	}
}

func TestMetrics_RecordInterceptorInvocation_NilMetrics(t *testing.T) {
	// Test that nil metrics doesn't panic
	var m *Metrics
	m.RecordInterceptorInvocation("test", "test.wasm", "outbound", true, 0.1)
	// Should not panic
}

func TestMetrics_InterceptorMetricsNotNil(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	if m.InterceptorInvocations == nil {
		t.Error("InterceptorInvocations not initialized")
	}
	if m.InterceptorDuration == nil {
		t.Error("InterceptorDuration not initialized")
	}
	if m.InterceptorErrors == nil {
		t.Error("InterceptorErrors not initialized")
	}
}
