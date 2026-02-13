package tracing

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Config holds tracing configuration.
type Config struct {
	Enabled     bool
	Endpoint    string
	ServiceName string
}

// GetConfig reads tracing configuration from environment variables.
// FISO_OTEL_ENABLED must be "true" to enable tracing.
// OTEL_EXPORTER_OTLP_ENDPOINT defaults to "localhost:4317".
func GetConfig(serviceName string) Config {
	enabled := strings.ToLower(os.Getenv("FISO_OTEL_ENABLED")) == "true"
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:4317"
	}
	return Config{
		Enabled:     enabled,
		Endpoint:    endpoint,
		ServiceName: serviceName,
	}
}

// Initialize sets up OpenTelemetry tracing.
// When disabled, returns a no-op tracer.
// Returns the tracer, shutdown function, and error.
func Initialize(cfg Config, logger *slog.Logger) (trace.Tracer, func(context.Context) error, error) {
	if !cfg.Enabled {
		logger.Info("tracing disabled, using no-op tracer")
		return noop.NewTracerProvider().Tracer(cfg.ServiceName), func(ctx context.Context) error { return nil }, nil
	}

	logger.Info("initializing tracing", "endpoint", cfg.Endpoint, "service", cfg.ServiceName)

	// Create OTLP exporter
	exporter, err := otlptracegrpc.New(context.Background(),
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	// Create resource with service name
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create resource: %w", err)
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Set global tracer provider and propagator
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.Info("tracing initialized successfully")

	tracer := tp.Tracer(cfg.ServiceName)
	shutdown := func(ctx context.Context) error {
		logger.Info("shutting down tracer provider")
		return tp.Shutdown(ctx)
	}

	return tracer, shutdown, nil
}

// Propagator returns the global text map propagator for trace context.
func Propagator() propagation.TextMapPropagator {
	return otel.GetTextMapPropagator()
}
