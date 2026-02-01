package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/lsm/fiso/internal/config"
	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/observability"
	"github.com/lsm/fiso/internal/pipeline"
	"github.com/lsm/fiso/internal/source/kafka"
	httpsink "github.com/lsm/fiso/internal/sink/http"
	celxform "github.com/lsm/fiso/internal/transform/cel"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := observability.NewLogger("fiso-flow", slog.LevelInfo)
	slog.SetDefault(logger)

	configDir := os.Getenv("FISO_CONFIG_DIR")
	if configDir == "" {
		configDir = "/etc/fiso/flows"
	}

	metricsAddr := os.Getenv("FISO_METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = ":9090"
	}

	// Load configuration
	loader := config.NewLoader(configDir, logger)
	flows, err := loader.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(flows) == 0 {
		return fmt.Errorf("no flow definitions found in %s", configDir)
	}

	// Setup metrics
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(collectors.NewGoCollector())
	_ = observability.NewMetrics(reg)

	// Health server
	health := observability.NewHealthServer()

	// Start metrics + health HTTP server
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.Handle("GET /healthz", health.Handler())
	mux.Handle("GET /readyz", health.Handler())

	httpServer := &http.Server{Addr: metricsAddr, Handler: mux}
	go func() {
		logger.Info("metrics server starting", "addr", metricsAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server error", "error", err)
		}
	}()

	// Context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start config watcher
	watchDone := make(chan struct{})
	go func() {
		if err := loader.Watch(watchDone); err != nil {
			logger.Error("config watcher error", "error", err)
		}
	}()

	// Build and start pipeline for the first flow (single-flow for Phase 1)
	var flowName string
	var flowDef *config.FlowDefinition
	for name, def := range flows {
		flowName = name
		flowDef = def
		break
	}

	logger.Info("starting flow", "name", flowName)

	p, err := buildPipeline(flowDef, logger)
	if err != nil {
		return fmt.Errorf("build pipeline %s: %w", flowName, err)
	}

	health.SetReady(true)

	// Run pipeline until shutdown
	pipelineErr := p.Run(ctx)

	// Graceful shutdown
	health.SetReady(false)
	close(watchDone)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}

	logger.Info("shutdown complete")
	return pipelineErr
}

func buildPipeline(flowDef *config.FlowDefinition, logger *slog.Logger) (*pipeline.Pipeline, error) {
	// Build source
	if flowDef.Source.Type != "kafka" {
		return nil, fmt.Errorf("unsupported source type: %s (only 'kafka' supported in Phase 1)", flowDef.Source.Type)
	}

	brokers, err := getStringSlice(flowDef.Source.Config, "brokers")
	if err != nil {
		return nil, fmt.Errorf("source config: %w", err)
	}
	topic, _ := flowDef.Source.Config["topic"].(string)
	consumerGroup, _ := flowDef.Source.Config["consumerGroup"].(string)
	startOffset, _ := flowDef.Source.Config["startOffset"].(string)

	src, err := kafka.NewSource(kafka.Config{
		Brokers:       brokers,
		Topic:         topic,
		ConsumerGroup: consumerGroup,
		StartOffset:   startOffset,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("kafka source: %w", err)
	}

	// Build transformer (optional)
	var transformer *celxform.Transformer
	if flowDef.Transform != nil && flowDef.Transform.CEL != "" {
		transformer, err = celxform.NewTransformer(flowDef.Transform.CEL)
		if err != nil {
			return nil, fmt.Errorf("cel transformer: %w", err)
		}
	}

	// Build sink
	if flowDef.Sink.Type != "http" {
		return nil, fmt.Errorf("unsupported sink type: %s (only 'http' supported in Phase 1)", flowDef.Sink.Type)
	}

	sinkURL, _ := flowDef.Sink.Config["url"].(string)
	sinkMethod, _ := flowDef.Sink.Config["method"].(string)

	sk, err := httpsink.NewSink(httpsink.Config{
		URL:    sinkURL,
		Method: sinkMethod,
		Retry: httpsink.RetryConfig{
			MaxAttempts:     flowDef.ErrorHandling.MaxRetries,
			InitialInterval: 200 * time.Millisecond,
			MaxInterval:     30 * time.Second,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("http sink: %w", err)
	}

	// Build DLQ handler
	pub, err := kafka.NewPublisher(brokers)
	if err != nil {
		return nil, fmt.Errorf("dlq publisher: %w", err)
	}

	dlqHandler := dlq.NewHandler(pub)
	if flowDef.ErrorHandling.DeadLetterTopic != "" {
		dlqHandler = dlq.NewHandler(pub, dlq.WithTopicFunc(func(_ string) string {
			return flowDef.ErrorHandling.DeadLetterTopic
		}))
	}

	cfg := pipeline.Config{
		FlowName: flowDef.Name,
	}

	return pipeline.New(cfg, src, transformer, sk, dlqHandler), nil
}

func getStringSlice(m map[string]interface{}, key string) ([]string, error) {
	val, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("missing key %q", key)
	}

	switch v := val.(type) {
	case []interface{}:
		result := make([]string, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("key %q: element %d is not a string", key, i)
			}
			result[i] = s
		}
		return result, nil
	case []string:
		return v, nil
	default:
		return nil, fmt.Errorf("key %q: expected string slice, got %T", key, val)
	}
}
