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
}

func TestMetrics_IncrementCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RequestsTotal.WithLabelValues("crm", "GET", "200", "sync").Inc()
	m.RetriesTotal.WithLabelValues("crm", "1").Inc()
	m.AuthRefreshTotal.WithLabelValues("crm", "success").Inc()
	m.CircuitState.WithLabelValues("crm").Set(0)
	m.RequestDuration.WithLabelValues("crm", "GET").Observe(0.5)

	// Gather to verify
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mfs) != 5 {
		t.Errorf("expected 5 metric families, got %d", len(mfs))
	}
}
