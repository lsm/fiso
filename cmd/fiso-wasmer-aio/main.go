//go:build wasmer

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"

	"github.com/lsm/fiso/internal/config"
	"github.com/lsm/fiso/internal/delivery"
	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/flowruntime"
	"github.com/lsm/fiso/internal/interceptor"
	grpcinterceptor "github.com/lsm/fiso/internal/interceptor/grpc"
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
	grpcsink "github.com/lsm/fiso/internal/sink/grpc"
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
	defer func() { _ = appManager.StopAll(context.Background()) }()

	for _, appCfg := range cfg.Wasmer.Apps {
		if err := appManager.StartApp(ctx, appCfg); err != nil {
			return fmt.Errorf("start wasmer app %s: %w", appCfg.Name, err)
		}
		app, _ := appManager.GetApp(appCfg.Name)
		logger.Info("started wasmer app", "name", appCfg.Name, "addr", app.Addr)
	}

	// 2. Start Link (before Flows: HTTP-enabled interceptors
	// call into it at build/run time via the default linkAddr)
	var linkServer *http.Server
	// defaultLinkAddr is the origin of the embedded Link actually bound;
	// HTTP-enabled interceptors that omit linkAddr call this, not a
	// hard-coded port that a link.listenAddr override would strand.
	defaultLinkAddr := ""
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
			defer func() { _ = publisherPool.Close() }()

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
			defer func() { _ = interceptorRegistry.Close() }()
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

			// Bind synchronously: HTTP-enabled interceptors call into this
			// listener through the default linkAddr, and a goroutine-bound
			// listener would race the first guest call.
			ln, err := net.Listen("tcp", linkCfg.ListenAddr)
			if err != nil {
				return fmt.Errorf("link listen: %w", err)
			}
			defaultLinkAddr = loopbackLinkAddr(ln)
			go func() {
				logger.Info("link server starting", "addr", ln.Addr().String())
				if err := linkServer.Serve(ln); err != nil && err != http.ErrServerClosed {
					logger.Error("link server error", "error", err)
				}
			}()
		}
	}

	// 3. Start Flow
	// Load Flow definitions
	if cfg.Flow.ConfigDir == "" {
		cfg.Flow.ConfigDir = "/etc/fiso/flows"
	}
	loader := config.NewLoader(cfg.Flow.ConfigDir, logger)
	flows := map[string]*config.FlowDefinition{}
	if _, statErr := os.Stat(cfg.Flow.ConfigDir); os.IsNotExist(statErr) {
		logger.Warn("flow config dir not present, continuing without flow", "dir", cfg.Flow.ConfigDir)
	} else if statErr != nil {
		// A present-but-inaccessible flow directory is a configuration
		// error, not a missing component.
		return fmt.Errorf("stat flow config dir: %w", statErr)
	} else if flows, err = loader.LoadStrict(); err != nil {
		// Invalid flow definitions fail startup: silently skipping a file
		// would drop a configured pipeline without notice.
		return fmt.Errorf("load flows: %w", err)
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

	// Required-runner gate: a terminal Flow return drops process readiness
	// while surviving components keep running (ADR 0005).
	gate := flowruntime.NewGate(health)

	type flowRunner struct {
		name     string
		pipeline *pipeline.Pipeline
	}
	runners := make([]*flowRunner, 0, len(flows))

	if len(flows) > 0 {
		for name, def := range flows {
			p, err := buildPipeline(def, logger, httpPool, tracer, defaultLinkAddr)
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
			gate.GoContext(ctx, runner.name, runner.pipeline.Run)
		}
	}

	// Ready only after every component (Flows and, when configured, Link)
	// has completed startup — readiness before the Link listener exists
	// would route Link traffic to nothing.
	gate.SetRunning()
	logger.Info("all components started")

	<-ctx.Done()

	logger.Info("shutting down")
	health.SetReady(false)
	close(watchDone)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if linkServer != nil {
		if err := linkServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("link server shutdown error", "error", err)
		}
	}
	if err := httpPool.Close(); err != nil {
		logger.Error("http pool shutdown error", "error", err)
	}
	for _, runner := range runners {
		if err := runner.pipeline.Shutdown(shutdownCtx); err != nil {
			logger.Error("pipeline shutdown error", "flow", runner.name, "error", err)
		}
	}
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("metrics server shutdown error", "error", err)
	}

	return nil
}

func buildPipeline(flowDef *config.FlowDefinition, logger *slog.Logger, httpPool *httpsource.ServerPool, tracer trace.Tracer, defaultLinkAddr string) (*pipeline.Pipeline, error) {
	commitPolicy := delivery.NormalizeCommitPolicy(flowDef.ErrorHandling.CommitPolicy)
	if commitPolicy == delivery.CommitPolicyKafkaTransaction {
		if flowDef.Source.Type != "kafka" || flowDef.Sink.Type != "kafka" {
			return nil, fmt.Errorf("commitPolicy kafka_transaction requires kafka source and kafka sink")
		}
		sourceCluster, _ := flowDef.Source.Config["cluster"].(string)
		sinkCluster, _ := flowDef.Sink.Config["cluster"].(string)
		if sourceCluster == "" || sinkCluster == "" || sourceCluster != sinkCluster {
			return nil, fmt.Errorf("commitPolicy kafka_transaction requires source and sink to use the same kafka cluster")
		}
	}

	var src source.Source
	var propagateErrors bool

	switch flowDef.Source.Type {
	case "kafka":
		topic, _ := flowDef.Source.Config["topic"].(string)
		consumerGroup, _ := flowDef.Source.Config["consumerGroup"].(string)
		startOffset := flowDef.Source.Config["startOffset"]
		clusterName, ok := flowDef.Source.Config["cluster"].(string)
		if !ok || clusterName == "" {
			return nil, fmt.Errorf("source config: cluster name is required")
		}
		cluster, found := flowDef.Kafka.Clusters[clusterName]
		if !found {
			return nil, fmt.Errorf("source config: cluster %q not found", clusterName)
		}
		kafkaCfg := kafka_source.Config{
			Cluster:            &cluster,
			Topic:              topic,
			ConsumerGroup:      consumerGroup,
			StartOffset:        startOffset,
			StopOnHandlerError: true,
		}
		if commitPolicy == delivery.CommitPolicyKafkaTransaction {
			kafkaCfg.TransactionalID = flowDef.ErrorHandling.TransactionalID
			kafkaCfg.RequireStableFetchOffset = true
		}
		s, err := kafka_source.NewSource(kafkaCfg, logger)
		if err != nil {
			return nil, fmt.Errorf("kafka source: %w", err)
		}
		s.SetTracer(tracer)
		src = s
		propagateErrors = true

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
			Retry:  httpsink.RetryConfig{MaxAttempts: flowDef.ErrorHandling.MaxRetries, InitialInterval: 200 * time.Millisecond, MaxInterval: 30 * time.Second},
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
		kSink, err := kafkasink.NewSink(kafkasink.Config{Cluster: &cluster, Topic: topic, RequireTransactional: commitPolicy == delivery.CommitPolicyKafkaTransaction})
		if err != nil {
			return nil, fmt.Errorf("kafka sink: %w", err)
		}
		kSink.SetTracer(tracer)
		sk = kSink

	case "grpc":
		address, ok := flowDef.Sink.Config["address"].(string)
		if !ok || address == "" {
			return nil, fmt.Errorf("sink config: address is required for grpc sink")
		}
		grpcCfg := grpcsink.Config{Address: address}
		if timeoutRaw, present := flowDef.Sink.Config["timeout"]; present {
			timeoutStr, isStr := timeoutRaw.(string)
			if !isStr {
				return nil, fmt.Errorf("sink config: timeout must be a duration string")
			}
			timeout, err := time.ParseDuration(timeoutStr)
			if err != nil {
				return nil, fmt.Errorf("sink config: timeout %q is not a valid duration", timeoutStr)
			}
			if timeout < 0 {
				return nil, fmt.Errorf("sink config: timeout %q must not be negative", timeoutStr)
			}
			if timeout == 0 {
				return nil, fmt.Errorf("sink config: timeout %q must be positive", timeoutStr)
			}
			grpcCfg.Timeout = timeout
		}
		// The gRPC sink has no credentials configuration yet, so TLS cannot be
		// enabled; reject every value except an explicit false — including null,
		// which would otherwise silently select plaintext.
		if tlsRaw, present := flowDef.Sink.Config["tls"]; present {
			if enabled, isBool := tlsRaw.(bool); !isBool || enabled {
				return nil, fmt.Errorf("sink config: tls is not supported until gRPC TLS credentials are configurable")
			}
		}
		gSink, err := grpcsink.NewSink(grpcCfg)
		if err != nil {
			return nil, fmt.Errorf("grpc sink: %w", err)
		}
		gSink.SetTracer(tracer)
		sk = gSink

	default:
		return nil, fmt.Errorf("unsupported sink type: %s", flowDef.Sink.Type)
	}

	// DLQ logic
	var dlqHandler *dlq.Handler
	if flowDef.Source.Type == "kafka" {
		clusterName, _ := flowDef.Source.Config["cluster"].(string)
		cluster, found := flowDef.Kafka.Clusters[clusterName]
		if !found {
			return nil, fmt.Errorf("dlq publisher: cluster %q not found", clusterName)
		}
		pub, err := kafka_source.NewPublisher(&cluster)
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

	cfg := pipeline.Config{FlowName: flowDef.Name, SourceType: flowDef.Source.Type, PropagateErrors: propagateErrors, CommitPolicy: commitPolicy}

	// Interceptors
	var chain *interceptor.Chain
	if len(flowDef.Interceptors) > 0 {
		var interceptors []interceptor.Interceptor
		// Create the factory once, outside the loop, so it is reused across all wasm interceptors.
		factory := wasmruntime.NewFactory()
		for _, ic := range flowDef.Interceptors {
			switch ic.Type {
			case "wasm":
				modulePath := getString(ic.Config, "module")
				runtimeType := getString(ic.Config, "runtime")

				wasmCfg := wasmruntime.Config{
					Type:       wasmruntime.RuntimeType(runtimeType),
					ModulePath: modulePath,
				}
				// Env delivery (ADR 0008): configured env reaches the guest
				// at instantiation — the channel for key material such as
				// JWT verification keys.
				wasmCfg.Env, err = getEnvMap(ic.Config)
				if err != nil {
					return nil, fmt.Errorf("wasm interceptor %s: %w", modulePath, err)
				}
				if httpEnabled(ic.Config) {
					if runtimeType != "wazero" && runtimeType != "" {
						return nil, fmt.Errorf("wasm interceptor %s: host HTTP calls require the wazero runtime", modulePath)
					}
					cfg := hostHTTPConfig(ic.Config, defaultLinkAddr)
					wasmCfg.HostHTTP = &cfg
				}

				rt, err := factory.Create(context.Background(), wasmCfg)
				if err != nil {
					return nil, fmt.Errorf("wasm runtime for %s: %w", modulePath, err)
				}

				interceptors = append(interceptors, wasm.New(rt, modulePath))
			case "grpc":
				client, err := grpcinterceptor.NewConnClient(getString(ic.Config, "address"))
				if err != nil {
					return nil, fmt.Errorf("grpc interceptor: %w", err)
				}
				var timeout time.Duration
				if d, err := time.ParseDuration(getString(ic.Config, "timeout")); err == nil && d > 0 {
					timeout = d
				}
				interceptors = append(interceptors, grpcinterceptor.New(client, timeout))
				logger.Info("loaded grpc interceptor", "address", getString(ic.Config, "address"))
			default:
				return nil, fmt.Errorf("unsupported interceptor type: %s", ic.Type)
			}
		}
		chain = interceptor.NewChain(interceptors...)
	}

	return pipeline.New(cfg, src, transformer, sk, dlqHandler, chain), nil
}

// httpEnabled reports whether a wasm interceptor opted into host HTTP calls.
func httpEnabled(cfg map[string]interface{}) bool {
	enabled, _ := cfg["http"].(bool)
	return enabled
}

// hostHTTPConfig builds the host-function config from interceptor settings.
// defaultLinkAddr is the embedded Link's bound origin; it takes precedence
// over the documented default when the interceptor omits linkAddr, so a
// link.listenAddr override does not strand guests on a hard-coded port.
func hostHTTPConfig(cfg map[string]interface{}, defaultLinkAddr string) wasmruntime.HostHTTPConfig {
	linkAddr := getString(cfg, "linkAddr")
	if linkAddr == "" {
		linkAddr = defaultLinkAddr
	}
	if linkAddr == "" {
		// No embedded Link in this process: the documented Link default.
		linkAddr = "http://127.0.0.1:3500"
	}
	var targets []string
	if raw, ok := cfg["httpTargets"].([]interface{}); ok {
		for _, t := range raw {
			if name, isStr := t.(string); isStr && name != "" {
				targets = append(targets, name)
			}
		}
	}
	return wasmruntime.HostHTTPConfig{LinkAddr: linkAddr, AllowedTargets: targets}
}

// loopbackLinkAddr returns the http origin for reaching a Link bound to ln.
// An unspecified bind host (":3600", "0.0.0.0:3600") is dialed through the
// loopback — the proxy is in-process — and the actually bound port is used,
// so even a :0 override yields a dialable address.
func loopbackLinkAddr(ln net.Listener) string {
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return ""
	}
	host := "127.0.0.1"
	if tcp.IP != nil && !tcp.IP.IsUnspecified() {
		host = tcp.IP.String()
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(tcp.Port))
}

func getString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

// getEnvMap extracts the env delivered to the guest at instantiation (ADR
// 0008). Malformed values fail construction instead of being silently
// dropped — a dropped verification key would silently disable an
// authentication module's allow path.
func getEnvMap(cfg map[string]interface{}) (map[string]string, error) {
	raw, present := cfg["env"]
	if !present || raw == nil {
		return nil, nil
	}
	envMap, isMap := raw.(map[string]interface{})
	if !isMap {
		return nil, fmt.Errorf("config.env must be a map of strings")
	}
	env := make(map[string]string, len(envMap))
	for k, v := range envMap {
		s, isStr := v.(string)
		if !isStr {
			return nil, fmt.Errorf("config.env[%q] must be a string", k)
		}
		// WASI entries are KEY=VALUE strings; reject unrepresentable
		// names and values at construction, not per event.
		if k == "" || strings.ContainsAny(k, "=\x00") {
			return nil, fmt.Errorf("config.env[%q] is not a valid environment name", k)
		}
		if strings.ContainsRune(s, '\x00') {
			return nil, fmt.Errorf("config.env[%q] is not a valid environment value", k)
		}
		env[k] = s
	}
	return env, nil
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
