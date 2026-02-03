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
	"github.com/lsm/fiso/internal/interceptor"
	"github.com/lsm/fiso/internal/interceptor/wasm"
	"github.com/lsm/fiso/internal/observability"
	"github.com/lsm/fiso/internal/pipeline"
	httpsink "github.com/lsm/fiso/internal/sink/http"
	temporalsink "github.com/lsm/fiso/internal/sink/temporal"
	"github.com/lsm/fiso/internal/source"
	grpcsource "github.com/lsm/fiso/internal/source/grpc"
	httpsource "github.com/lsm/fiso/internal/source/http"
	"github.com/lsm/fiso/internal/source/kafka"
	"github.com/lsm/fiso/internal/transform"
	celxform "github.com/lsm/fiso/internal/transform/cel"
	mappingxform "github.com/lsm/fiso/internal/transform/mapping"
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

	if err := p.Shutdown(shutdownCtx); err != nil {
		logger.Error("pipeline shutdown error", "error", err)
	}
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}

	logger.Info("shutdown complete")
	return pipelineErr
}

func buildPipeline(flowDef *config.FlowDefinition, logger *slog.Logger) (*pipeline.Pipeline, error) {
	// Build source
	var src source.Source
	var propagateErrors bool

	switch flowDef.Source.Type {
	case "kafka":
		brokers, err := getStringSlice(flowDef.Source.Config, "brokers")
		if err != nil {
			return nil, fmt.Errorf("source config: %w", err)
		}
		topic, _ := flowDef.Source.Config["topic"].(string)
		consumerGroup, _ := flowDef.Source.Config["consumerGroup"].(string)
		startOffset, _ := flowDef.Source.Config["startOffset"].(string)

		s, err := kafka.NewSource(kafka.Config{
			Brokers:       brokers,
			Topic:         topic,
			ConsumerGroup: consumerGroup,
			StartOffset:   startOffset,
		}, logger)
		if err != nil {
			return nil, fmt.Errorf("kafka source: %w", err)
		}
		src = s

	case "grpc":
		listenAddr, _ := flowDef.Source.Config["listenAddr"].(string)
		s, err := grpcsource.NewSource(grpcsource.Config{ListenAddr: listenAddr}, logger)
		if err != nil {
			return nil, fmt.Errorf("grpc source: %w", err)
		}
		src = s
		propagateErrors = true

	case "http":
		listenAddr, _ := flowDef.Source.Config["listenAddr"].(string)
		path, _ := flowDef.Source.Config["path"].(string)
		s, err := httpsource.NewSource(httpsource.Config{ListenAddr: listenAddr, Path: path}, logger)
		if err != nil {
			return nil, fmt.Errorf("http source: %w", err)
		}
		src = s
		propagateErrors = true

	default:
		return nil, fmt.Errorf("unsupported source type: %s", flowDef.Source.Type)
	}

	// Build transformer (optional)
	// Use the interface type so a nil value stays nil (avoids typed-nil gotcha).
	var transformer transform.Transformer
	var err error
	if flowDef.Transform != nil {
		switch {
		case flowDef.Transform.CEL != "":
			transformer, err = celxform.NewTransformer(flowDef.Transform.CEL)
			if err != nil {
				return nil, fmt.Errorf("cel transformer: %w", err)
			}
		case len(flowDef.Transform.Mapping) > 0:
			transformer, err = mappingxform.NewTransformer(flowDef.Transform.Mapping)
			if err != nil {
				return nil, fmt.Errorf("mapping transformer: %w", err)
			}
		}
	}

	// Build sink
	var sk interface {
		Deliver(context.Context, []byte, map[string]string) error
		Close() error
	}

	switch flowDef.Sink.Type {
	case "http":
		sinkURL, _ := flowDef.Sink.Config["url"].(string)
		sinkMethod, _ := flowDef.Sink.Config["method"].(string)

		httpSink, err := httpsink.NewSink(httpsink.Config{
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
		sk = httpSink

	case "temporal":
		tcfg := temporalsink.Config{
			TaskQueue:    getString(flowDef.Sink.Config, "taskQueue"),
			WorkflowType: getString(flowDef.Sink.Config, "workflowType"),
		}
		if v := getString(flowDef.Sink.Config, "hostPort"); v != "" {
			tcfg.HostPort = v
		}
		if v := getString(flowDef.Sink.Config, "namespace"); v != "" {
			tcfg.Namespace = v
		}
		if v := getString(flowDef.Sink.Config, "workflowIdExpr"); v != "" {
			tcfg.WorkflowIDExpr = v
		}
		if v := getString(flowDef.Sink.Config, "signalName"); v != "" {
			tcfg.SignalName = v
		}
		if v := getString(flowDef.Sink.Config, "mode"); v != "" {
			tcfg.Mode = temporalsink.Mode(v)
		}

		client, err := newTemporalSDKClient(tcfg)
		if err != nil {
			return nil, fmt.Errorf("temporal client: %w", err)
		}
		tSink, err := temporalsink.NewSink(client, tcfg)
		if err != nil {
			return nil, fmt.Errorf("temporal sink: %w", err)
		}
		sk = tSink

	default:
		return nil, fmt.Errorf("unsupported sink type: %s", flowDef.Sink.Type)
	}

	// Build DLQ handler — use Kafka publisher when brokers available, noop otherwise
	var dlqHandler *dlq.Handler
	if flowDef.Source.Type == "kafka" {
		brokers, _ := getStringSlice(flowDef.Source.Config, "brokers")
		pub, err := kafka.NewPublisher(brokers)
		if err != nil {
			return nil, fmt.Errorf("dlq publisher: %w", err)
		}
		dlqHandler = dlq.NewHandler(pub)
		if flowDef.ErrorHandling.DeadLetterTopic != "" {
			dlqHandler = dlq.NewHandler(pub, dlq.WithTopicFunc(func(_ string) string {
				return flowDef.ErrorHandling.DeadLetterTopic
			}))
		}
	} else {
		dlqHandler = dlq.NewHandler(&dlq.NoopPublisher{})
	}

	cfg := pipeline.Config{
		FlowName:        flowDef.Name,
		PropagateErrors: propagateErrors,
	}

	// Apply CloudEvents overrides from config
	if flowDef.CloudEvents != nil {
		cfg.CloudEvents = &pipeline.CloudEventsOverrides{
			Type:    flowDef.CloudEvents.Type,
			Source:  flowDef.CloudEvents.Source,
			Subject: flowDef.CloudEvents.Subject,
		}
	}

	// Build interceptor chain (optional)
	var chain *interceptor.Chain
	if len(flowDef.Interceptors) > 0 {
		var interceptors []interceptor.Interceptor
		for _, ic := range flowDef.Interceptors {
			switch ic.Type {
			case "wasm":
				modulePath := getString(ic.Config, "module")
				wasmBytes, err := os.ReadFile(modulePath)
				if err != nil {
					return nil, fmt.Errorf("read wasm module %s: %w", modulePath, err)
				}
				rt, err := wasm.NewWazeroRuntime(context.Background(), wasmBytes)
				if err != nil {
					return nil, fmt.Errorf("wasm runtime for %s: %w", modulePath, err)
				}
				interceptors = append(interceptors, wasm.New(rt, modulePath))
				logger.Info("loaded wasm interceptor", "module", modulePath)
			default:
				return nil, fmt.Errorf("unsupported interceptor type: %s", ic.Type)
			}
		}
		chain = interceptor.NewChain(interceptors...)
	}

	return pipeline.New(cfg, src, transformer, sk, dlqHandler, chain), nil
}

func getString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
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
