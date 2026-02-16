//go:build wasmer

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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"

	"github.com/lsm/fiso/internal/config"
	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/interceptor"
	"github.com/lsm/fiso/internal/interceptor/wasm"
	internal_kafka "github.com/lsm/fiso/internal/kafka"
	"github.com/lsm/fiso/internal/link"
	"github.com/lsm/fiso/internal/link/auth"
	"github.com/lsm/fiso/internal/link/circuitbreaker"
	"github.com/lsm/fiso/internal/link/discovery"
	linkinterceptor "github.com/lsm/fiso/internal/link/interceptor"
	"github.com/lsm/fiso/internal/link/proxy"
	"github.com/lsm/fiso/internal/link/ratelimit"
	"github.com/lsm/fiso/internal/observability"
	"github.com/lsm/fiso/internal/pipeline"
	httpsink "github.com/lsm/fiso/internal/sink/http"
	kafkasink "github.com/lsm/fiso/internal/sink/kafka"
	temporalsink "github.com/lsm/fiso/internal/sink/temporal"
	"github.com/lsm/fiso/internal/source"
	grpcsource "github.com/lsm/fiso/internal/source/grpc"
	httpsource "github.com/lsm/fiso/internal/source/http"
	kafka_source "github.com/lsm/fiso/internal/source/kafka"
	"github.com/lsm/fiso/internal/tracing"
	"github.com/lsm/fiso/internal/transform"
	unifiedxform "github.com/lsm/fiso/internal/transform/unified"
	wasmruntime "github.com/lsm/fiso/internal/wasm"
	"github.com/lsm/fiso/internal/wasmer"
)

// UnifiedConfig is the all-in-one configuration.
type UnifiedConfig struct {
	Flow struct {
		ConfigDir   string `yaml:"configDir"`
		MetricsAddr string `yaml:"metricsAddr"`
	} `yaml:"flow"`

	Link struct {
		ConfigPath string `yaml:"configPath"`
		ListenAddr string `yaml:"listenAddr"`
	} `yaml:"link"`

	Wasmer struct {
		Apps []wasmer.AppConfig `yaml:"apps"`
	} `yaml:"wasmer"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "", "Path to unified config file")
	logLevelFlag := flag.String("log-level", "", "Log level")
	flag.Parse()

	level := observability.GetLogLevel(*logLevelFlag)
	logger := observability.NewLogger("fiso-wasmer-aio", level)
	slog.SetDefault(logger)

	logger.Info("starting fiso-wasmer-aio", "log_level", level.String())

	// Initialize tracing
	tracerCfg := tracing.GetConfig("fiso-wasmer-aio")
	tracer, tracerShutdown, err := tracing.Initialize(tracerCfg, logger)
	if err != nil {
		return fmt.Errorf("initialize tracing: %w", err)
	}
	defer func() {
		if err := tracerShutdown(context.Background()); err != nil {
			logger.Error("tracer shutdown error", "error", err)
		}
	}()

	if *cfgPath == "" {
		*cfgPath = os.Getenv("FISO_AIO_CONFIG")
	}
	if *cfgPath == "" {
		*cfgPath = "/etc/fiso/aio/config.yaml"
	}

	// Load unified config
	data, err := os.ReadFile(*cfgPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var cfg UnifiedConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Setup metrics registry (shared)
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(collectors.NewGoCollector())
	_ = observability.NewMetrics(reg)
	linkMetrics := link.NewMetrics(reg)

	// Health server (shared)
	health := observability.NewHealthServer()

	// Metrics + health HTTP server
	// Use Flow's metrics addr for the shared metrics endpoint
	metricsAddr := cfg.Flow.MetricsAddr
	if metricsAddr == "" {
		metricsAddr = ":9090"
	}

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.Handle("GET /healthz", health.Handler())
	mux.Handle("GET /readyz", health.Handler())

	metricsServer := &http.Server{Addr: metricsAddr, Handler: mux}
	go func() {
		logger.Info("metrics server starting", "addr", metricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server error", "error", err)
		}
	}()

	// Context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 1. Start Wasmer Apps
	appManager := wasmer.NewManager()
	defer appManager.StopAll(context.Background())

	for _, appCfg := range cfg.Wasmer.Apps {
		if err := appManager.StartApp(ctx, appCfg); err != nil {
			return fmt.Errorf("start wasmer app %s: %w", appCfg.Name, err)
		}
		app, _ := appManager.GetApp(appCfg.Name)
		logger.Info("started wasmer app", "name", appCfg.Name, "addr", app.Addr)
	}

	// 2. Start Flow
	// Load Flow definitions
	if cfg.Flow.ConfigDir == "" {
		cfg.Flow.ConfigDir = "/etc/fiso/flows"
	}
	loader := config.NewLoader(cfg.Flow.ConfigDir, logger)
	flows, err := loader.Load()
	if err != nil {
		logger.Warn("failed to load flows, continuing without flow", "error", err)
	}

	// Start config watcher
	watchDone := make(chan struct{})
	go func() {
		if err := loader.Watch(watchDone); err != nil {
			logger.Error("config watcher error", "error", err)
		}
	}()

	// HTTP Pool for Flow sources
	httpPool := httpsource.NewServerPool(logger)

	type flowRunner struct {
		name     string
		pipeline *pipeline.Pipeline
	}
	runners := make([]*flowRunner, 0, len(flows))

	if len(flows) > 0 {
		for name, def := range flows {
			p, err := buildPipeline(def, logger, httpPool, tracer)
			if err != nil {
				return fmt.Errorf("build flow %s: %w", name, err)
			}
			runners = append(runners, &flowRunner{name: name, pipeline: p})
		}
		
		go func() {
			if err := httpPool.Start(ctx); err != nil && err != context.Canceled {
				logger.Error("http pool error", "error", err)
			}
		}()

		for _, runner := range runners {
			go func(r *flowRunner) {
				logger.Info("flow started", "name", r.name)
				if err := r.pipeline.Run(ctx); err != nil {
					logger.Error("flow stopped with error", "name", r.name, "error", err)
				}
			}(runner)
		}
	}

	// 3. Start Link
	var linkServer *http.Server
	if cfg.Link.ConfigPath != "" {
		linkCfg, err := link.LoadConfig(cfg.Link.ConfigPath)
		if err != nil {
			logger.Warn("failed to load link config, continuing without link", "error", err)
		} else {
			if cfg.Link.ListenAddr != "" {
				linkCfg.ListenAddr = cfg.Link.ListenAddr
			}

			// Initialize Kafka registry (shared if possible, but creating new one for Link context)
			clusterRegistry := internal_kafka.NewRegistry()
			if len(linkCfg.Kafka.Clusters) > 0 {
				if err := clusterRegistry.LoadFromMap(linkCfg.Kafka.Clusters); err != nil {
					return fmt.Errorf("load link kafka clusters: %w", err)
				}
			}
			publisherPool := internal_kafka.NewPublisherPool(clusterRegistry)
			defer publisherPool.Close()

			// Build Link components
			breakers := make(map[string]*circuitbreaker.Breaker)
			for _, t := range linkCfg.Targets {
				if t.CircuitBreaker.Enabled {
					cbCfg := circuitbreaker.DefaultConfig()
					if t.CircuitBreaker.FailureThreshold > 0 {
						cbCfg.FailureThreshold = t.CircuitBreaker.FailureThreshold
					}
					if t.CircuitBreaker.SuccessThreshold > 0 {
						cbCfg.SuccessThreshold = t.CircuitBreaker.SuccessThreshold
					}
					if d, parseErr := time.ParseDuration(t.CircuitBreaker.ResetTimeout); parseErr == nil {
						cbCfg.ResetTimeout = d
					}
					breakers[t.Name] = circuitbreaker.New(cbCfg)
				}
			}

			var authConfigs []auth.SecretConfig
			for _, t := range linkCfg.Targets {
				if t.Auth.Type != "" && t.Auth.Type != "none" && t.Auth.SecretRef != nil {
					authConfigs = append(authConfigs, auth.SecretConfig{
						TargetName: t.Name,
						Type:       capitalizeAuthType(t.Auth.Type),
						FilePath:   t.Auth.SecretRef.FilePath,
						EnvVar:     t.Auth.SecretRef.EnvVar,
					})
				}
			}
			var authProvider auth.Provider
			if len(authConfigs) > 0 {
				authProvider = auth.NewSecretProvider(authConfigs)
			} else {
				authProvider = &auth.NoopProvider{}
			}

			rateLimiter := ratelimit.New()
			for _, t := range linkCfg.Targets {
				if t.RateLimit.RequestsPerSecond > 0 {
					rateLimiter.Set(t.Name, t.RateLimit.RequestsPerSecond, t.RateLimit.Burst)
				}
			}

			store := link.NewTargetStore(linkCfg.Targets)
			interceptorRegistry := linkinterceptor.NewRegistry(linkMetrics, logger)
			defer interceptorRegistry.Close()
			if err := interceptorRegistry.Load(context.Background(), linkCfg.Targets); err != nil {
				return fmt.Errorf("load link interceptors: %w", err)
			}

			handlerCfg := proxy.Config{
				Targets:       store,
				Breakers:      breakers,
				RateLimiter:   rateLimiter,
				Auth:          authProvider,
				Resolver:      discovery.NewDNSResolver(),
				Metrics:       linkMetrics,
				Logger:        logger,
				KafkaRegistry: clusterRegistry,
				KafkaPool:     publisherPool,
				Interceptors:  interceptorRegistry,
			}
			handler := proxy.NewHandler(handlerCfg)
			handler.SetTracer(tracer)

			proxyMux := http.NewServeMux()
			proxyMux.Handle("/link/", otelhttp.NewHandler(handler, "proxy"))

			linkServer = &http.Server{
				Addr:    linkCfg.ListenAddr,
				Handler: proxyMux,
			}

			go func() {
				logger.Info("link server starting", "addr", linkCfg.ListenAddr)
				if err := linkServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Error("link server error", "error", err)
				}
			}()
		}
	}

	health.SetReady(true)
	logger.Info("all components started")

	<-ctx.Done()

	logger.Info("shutting down")
	health.SetReady(false)
	close(watchDone)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if linkServer != nil {
		linkServer.Shutdown(shutdownCtx)
	}
	httpPool.Close()
	for _, runner := range runners {
		runner.pipeline.Shutdown(shutdownCtx)
	}
	metricsServer.Shutdown(shutdownCtx)

	return nil
}

func buildPipeline(flowDef *config.FlowDefinition, logger *slog.Logger, httpPool *httpsource.ServerPool, tracer trace.Tracer) (*pipeline.Pipeline, error) {
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
			return nil, fmt.Errorf("source config: cluster %q not found", clusterName)
		}
		kafkaCfg := kafka_source.Config{
			Cluster:       &cluster,
			Topic:         topic,
			ConsumerGroup: consumerGroup,
			StartOffset:   startOffset,
		}
		s, err := kafka_source.NewSource(kafkaCfg, logger)
		if err != nil {
			return nil, fmt.Errorf("kafka source: %w", err)
		}
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

	var transformer transform.Transformer
	var err error
	if flowDef.Transform != nil && len(flowDef.Transform.Fields) > 0 {
		transformer, err = unifiedxform.NewTransformer(flowDef.Transform.Fields)
		if err != nil {
			return nil, fmt.Errorf("unified transformer: %w", err)
		}
	}

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
			Retry: httpsink.RetryConfig{MaxAttempts: flowDef.ErrorHandling.MaxRetries, InitialInterval: 200 * time.Millisecond, MaxInterval: 30 * time.Second},
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
		// ... simpler version of temporal config parsing ...
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
			return nil, fmt.Errorf("sink config: cluster %q not found", clusterName)
		}
		kSink, err := kafkasink.NewSink(kafkasink.Config{Cluster: &cluster, Topic: topic})
		if err != nil {
			return nil, fmt.Errorf("kafka sink: %w", err)
		}
		kSink.SetTracer(tracer)
		sk = kSink
	}

	// DLQ logic
	var dlqHandler *dlq.Handler
	if flowDef.Source.Type == "kafka" {
		// ... simplified dlq ...
		dlqHandler = dlq.NewHandler(&dlq.NoopPublisher{})
	} else {
		dlqHandler = dlq.NewHandler(&dlq.NoopPublisher{})
	}

	cfg := pipeline.Config{FlowName: flowDef.Name, PropagateErrors: propagateErrors}
	
	// Interceptors
	var chain *interceptor.Chain
	if len(flowDef.Interceptors) > 0 {
		var interceptors []interceptor.Interceptor
		for _, ic := range flowDef.Interceptors {
			switch ic.Type {
			case "wasm":
				modulePath := getString(ic.Config, "module")
				runtimeType := getString(ic.Config, "runtime") // read runtime type
				
				wasmCfg := wasmruntime.Config{
					Type:       wasmruntime.RuntimeType(runtimeType),
					ModulePath: modulePath,
				}
				
				// Create runtime via factory to support both Wasmer and Wazero
				factory := wasmruntime.NewFactory()
				rt, err := factory.Create(context.Background(), wasmCfg)
				if err != nil {
					return nil, fmt.Errorf("wasm runtime for %s: %w", modulePath, err)
				}
				
				interceptors = append(interceptors, wasm.New(rt, modulePath))
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

func capitalizeAuthType(t string) string {
	switch t {
	case "bearer":
		return "Bearer"
	case "apikey":
		return "APIKey"
	case "basic":
		return "Basic"
	default:
		return t
	}
}
