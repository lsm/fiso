package main

import (
	"context"
	"flag"
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
	"go.opentelemetry.io/otel/trace"

	"github.com/lsm/fiso/internal/config"
	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/interceptor"
	"github.com/lsm/fiso/internal/interceptor/wasm"
	"github.com/lsm/fiso/internal/observability"
	"github.com/lsm/fiso/internal/pipeline"
	httpsink "github.com/lsm/fiso/internal/sink/http"
	kafkasink "github.com/lsm/fiso/internal/sink/kafka"
	temporalsink "github.com/lsm/fiso/internal/sink/temporal"
	"github.com/lsm/fiso/internal/source"
	grpcsource "github.com/lsm/fiso/internal/source/grpc"
	httpsource "github.com/lsm/fiso/internal/source/http"
	"github.com/lsm/fiso/internal/source/kafka"
	"github.com/lsm/fiso/internal/tracing"
	"github.com/lsm/fiso/internal/transform"
	unifiedxform "github.com/lsm/fiso/internal/transform/unified"
)

var logLevel string

func init() {
	flag.StringVar(&logLevel, "log-level", "", "Log level (debug, info, warn, error). Can also be set via FISO_LOG_LEVEL env var.")
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	level := observability.GetLogLevel(logLevel)
	logger := observability.NewLogger("fiso-flow", level)
	slog.SetDefault(logger)

	slog.Debug("starting fiso-flow", "log_level", level.String())

	// Initialize tracing
	tracerCfg := tracing.GetConfig("fiso-flow")
	tracer, tracerShutdown, err := tracing.Initialize(tracerCfg, logger)
	if err != nil {
		return fmt.Errorf("initialize tracing: %w", err)
	}
	defer func() {
		if err := tracerShutdown(context.Background()); err != nil {
			logger.Error("tracer shutdown error", "error", err)
		}
	}()

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

	// Create shared HTTP server pool for path-based routing
	// Multiple HTTP flows can share the same port with different paths
	httpPool := httpsource.NewServerPool(logger)

	// Build and start all flows concurrently (router model)
	// Each flow runs independently - one flow failure doesn't stop others
	type flowRunner struct {
		name     string
		pipeline *pipeline.Pipeline
	}

	runners := make([]*flowRunner, 0, len(flows))
	for name, def := range flows {
		logger.Info("building flow", "name", name)
		p, err := buildPipeline(def, logger, httpPool, tracer)
		if err != nil {
			return fmt.Errorf("build pipeline %s: %w", name, err)
		}
		runners = append(runners, &flowRunner{
			name:     name,
			pipeline: p,
		})
	}

	if len(runners) == 0 {
		return fmt.Errorf("no flows to run")
	}

	logger.Info("starting flows", "count", len(runners))
	health.SetReady(true)

	// Start the shared HTTP pool (handles all HTTP sources)
	go func() {
		if err := httpPool.Start(ctx); err != nil && err != context.Canceled {
			logger.Error("http pool error", "error", err)
		}
	}()

	// Start each flow in its own goroutine (independent execution)
	for _, runner := range runners {
		go func(r *flowRunner) {
			logger.Info("flow started", "name", r.name)
			if err := r.pipeline.Run(ctx); err != nil {
				// Log error but don't stop other flows (router model)
				logger.Error("flow stopped with error", "name", r.name, "error", err)
			} else {
				logger.Info("flow stopped", "name", r.name)
			}
		}(runner)
	}

	// Wait for shutdown signal
	<-ctx.Done()

	// Graceful shutdown
	logger.Info("shutdown initiated")
	health.SetReady(false)
	close(watchDone)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Shut down HTTP pool first (stops accepting new requests)
	if err := httpPool.Close(); err != nil {
		logger.Error("http pool shutdown error", "error", err)
	}

	// Shut down all pipelines
	for _, runner := range runners {
		if err := runner.pipeline.Shutdown(shutdownCtx); err != nil {
			logger.Error("pipeline shutdown error", "flow", runner.name, "error", err)
		}
	}

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}

	logger.Info("shutdown complete")
	return nil
}

func buildPipeline(flowDef *config.FlowDefinition, logger *slog.Logger, httpPool *httpsource.ServerPool, tracer trace.Tracer) (*pipeline.Pipeline, error) {
	// Build source
	var src source.Source
	var propagateErrors bool

	switch flowDef.Source.Type {
	case "kafka":
		topic, _ := flowDef.Source.Config["topic"].(string)
		consumerGroup, _ := flowDef.Source.Config["consumerGroup"].(string)
		startOffset, _ := flowDef.Source.Config["startOffset"].(string)

		clusterName, ok := flowDef.Source.Config["cluster"].(string)
		if !ok || clusterName == "" {
			return nil, fmt.Errorf("source config: cluster name is required")
		}
		cluster, found := flowDef.Kafka.Clusters[clusterName]
		if !found {
			return nil, fmt.Errorf("source config: cluster %q not found in kafka.clusters", clusterName)
		}

		kafkaCfg := kafka.Config{
			Cluster:       &cluster,
			Topic:         topic,
			ConsumerGroup: consumerGroup,
			StartOffset:   startOffset,
		}

		s, err := kafka.NewSource(kafkaCfg, logger)
		if err != nil {
			return nil, fmt.Errorf("kafka source: %w", err)
		}
		// Set tracer for instrumentation
		s.SetTracer(tracer)
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
		s, err := httpsource.NewPooledSource(httpPool, httpsource.Config{ListenAddr: listenAddr, Path: path})
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
	if flowDef.Transform != nil && len(flowDef.Transform.Fields) > 0 {
		transformer, err = unifiedxform.NewTransformer(flowDef.Transform.Fields)
		if err != nil {
			return nil, fmt.Errorf("unified transformer: %w", err)
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
		httpSink.SetTracer(tracer)
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

		// Parse TLS config
		if tlsRaw, ok := flowDef.Sink.Config["tls"].(map[string]interface{}); ok {
			if disabled, ok := tlsRaw["disabled"].(bool); ok {
				tcfg.TLS.Disabled = disabled
			}
			if enabled, ok := tlsRaw["enabled"].(bool); ok {
				tcfg.TLS.Enabled = enabled
			}
			if v := getString(tlsRaw, "caFile"); v != "" {
				tcfg.TLS.CAFile = v
			}
			if v := getString(tlsRaw, "certFile"); v != "" {
				tcfg.TLS.CertFile = v
			}
			if v := getString(tlsRaw, "keyFile"); v != "" {
				tcfg.TLS.KeyFile = v
			}
			if skip, ok := tlsRaw["skipVerify"].(bool); ok {
				tcfg.TLS.SkipVerify = skip
			}
		}

		// Parse auth config
		if authRaw, ok := flowDef.Sink.Config["auth"].(map[string]interface{}); ok {
			if v := getString(authRaw, "apiKey"); v != "" {
				tcfg.Auth.APIKey = v
			}
			if v := getString(authRaw, "apiKeyEnv"); v != "" {
				tcfg.Auth.APIKeyEnv = v
			}
			if v := getString(authRaw, "tokenFile"); v != "" {
				tcfg.Auth.TokenFile = v
			}
			if oidcRaw, ok := authRaw["oidc"].(map[string]interface{}); ok {
				tcfg.Auth.OIDC = &temporalsink.OIDCConfig{
					TokenURL:        getString(oidcRaw, "tokenURL"),
					ClientID:        getString(oidcRaw, "clientID"),
					ClientSecret:    getString(oidcRaw, "clientSecret"),
					ClientSecretEnv: getString(oidcRaw, "clientSecretEnv"),
				}
				if scopesRaw, ok := oidcRaw["scopes"].([]interface{}); ok {
					for _, s := range scopesRaw {
						if str, ok := s.(string); ok {
							tcfg.Auth.OIDC.Scopes = append(tcfg.Auth.OIDC.Scopes, str)
						}
					}
				}
			}
			if azureRaw, ok := authRaw["azure"].(map[string]interface{}); ok {
				tcfg.Auth.Azure = &temporalsink.AzureConfig{
					Scope: getString(azureRaw, "scope"),
				}
			}
		}

		// Parse typed params for cross-SDK compatibility
		if paramsRaw, ok := flowDef.Sink.Config["params"].([]interface{}); ok {
			for _, p := range paramsRaw {
				if pm, ok := p.(map[string]interface{}); ok {
					if expr := getString(pm, "expr"); expr != "" {
						tcfg.Params = append(tcfg.Params, temporalsink.ParamConfig{
							Expr: expr,
						})
					}
				}
			}
		}

		client, err := newTemporalSDKClient(tcfg)
		if err != nil {
			return nil, fmt.Errorf("temporal client: %w", err)
		}
		tSink, err := temporalsink.NewSink(client, tcfg)
		if err != nil {
			return nil, fmt.Errorf("temporal sink: %w", err)
		}
		tSink.SetTracer(tracer)
		sk = tSink

	case "kafka":
		topic, _ := flowDef.Sink.Config["topic"].(string)

		clusterName, ok := flowDef.Sink.Config["cluster"].(string)
		if !ok || clusterName == "" {
			return nil, fmt.Errorf("sink config: cluster name is required")
		}
		cluster, found := flowDef.Kafka.Clusters[clusterName]
		if !found {
			return nil, fmt.Errorf("sink config: cluster %q not found in kafka.clusters", clusterName)
		}

		kafkaSinkCfg := kafkasink.Config{
			Cluster: &cluster,
			Topic:   topic,
		}

		kSink, err := kafkasink.NewSink(kafkaSinkCfg)
		if err != nil {
			return nil, fmt.Errorf("kafka sink: %w", err)
		}
		kSink.SetTracer(tracer)
		sk = kSink

	default:
		return nil, fmt.Errorf("unsupported sink type: %s", flowDef.Sink.Type)
	}

	// Build DLQ handler — use Kafka publisher when source is Kafka, noop otherwise
	var dlqHandler *dlq.Handler
	if flowDef.Source.Type == "kafka" {
		clusterName, _ := flowDef.Source.Config["cluster"].(string)
		cluster, found := flowDef.Kafka.Clusters[clusterName]
		if !found {
			return nil, fmt.Errorf("dlq publisher: cluster %q not found", clusterName)
		}
		pub, err := kafka.NewPublisher(&cluster)
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
			ID:              flowDef.CloudEvents.ID,
			Type:            flowDef.CloudEvents.Type,
			Source:          flowDef.CloudEvents.Source,
			Subject:         flowDef.CloudEvents.Subject,
			Data:            flowDef.CloudEvents.Data,
			DataContentType: flowDef.CloudEvents.DataContentType,
			DataSchema:      flowDef.CloudEvents.DataSchema,
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
