package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewMetrics_RegistersWithoutPanic(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	if m.EventsTotal == nil {
		t.Error("EventsTotal is nil")
	}
	if m.EventDuration == nil {
		t.Error("EventDuration is nil")
	}
	if m.ConsumerLag == nil {
		t.Error("ConsumerLag is nil")
	}
	if m.TransformErrors == nil {
		t.Error("TransformErrors is nil")
	}
	if m.DLQTotal == nil {
		t.Error("DLQTotal is nil")
	}
	if m.SinkDeliveryErrors == nil {
		t.Error("SinkDeliveryErrors is nil")
	}
}

func TestMetrics_IncrementCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.EventsTotal.WithLabelValues("test-flow", "success").Inc()
	m.EventsTotal.WithLabelValues("test-flow", "error").Inc()
	m.TransformErrors.WithLabelValues("test-flow", "TRANSFORM_FAILED").Inc()
	m.DLQTotal.WithLabelValues("test-flow").Inc()
	m.SinkDeliveryErrors.WithLabelValues("test-flow").Inc()

	// Gather and verify metrics are present
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	expected := []string{
		"fiso_flow_events_total",
		"fiso_flow_transform_errors_total",
		"fiso_flow_dlq_total",
		"fiso_flow_sink_delivery_errors_total",
	}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected metric %s not found", name)
		}
	}
}

func TestMetrics_ObserveHistogram(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.EventDuration.WithLabelValues("test-flow", "transform").Observe(0.05)
	m.EventDuration.WithLabelValues("test-flow", "deliver").Observe(0.12)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "fiso_flow_event_duration_seconds" {
			found = true
			break
		}
	}
	if !found {
		t.Error("histogram metric not found")
	}
}
