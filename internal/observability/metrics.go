package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Fiso-Flow Prometheus metrics.
type Metrics struct {
	EventsTotal        *prometheus.CounterVec
	EventDuration      *prometheus.HistogramVec
	ConsumerLag        *prometheus.GaugeVec
	TransformErrors    *prometheus.CounterVec
	DLQTotal           *prometheus.CounterVec
	SinkDeliveryErrors *prometheus.CounterVec
}

// NewMetrics creates and registers all Fiso-Flow metrics.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	factory := promauto.With(reg)

	return &Metrics{
		EventsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "fiso_flow_events_total",
			Help: "Total events processed by Fiso-Flow.",
		}, []string{"flow", "status"}),

		EventDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "fiso_flow_event_duration_seconds",
			Help:    "Processing time per event phase.",
			Buckets: prometheus.DefBuckets,
		}, []string{"flow", "phase"}),

		ConsumerLag: factory.NewGaugeVec(prometheus.GaugeOpts{
			Name: "fiso_flow_consumer_lag",
			Help: "Consumer lag per partition.",
		}, []string{"flow", "partition"}),

		TransformErrors: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "fiso_flow_transform_errors_total",
			Help: "Transform failures by flow and error type.",
		}, []string{"flow", "error_type"}),

		DLQTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "fiso_flow_dlq_total",
			Help: "Events sent to DLQ.",
		}, []string{"flow"}),

		SinkDeliveryErrors: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "fiso_flow_sink_delivery_errors_total",
			Help: "Sink delivery failures.",
		}, []string{"flow"}),
	}
}
