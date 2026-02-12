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

	"github.com/lsm/fiso/internal/kafka"
	"github.com/lsm/fiso/internal/link"
	"github.com/lsm/fiso/internal/link/auth"
	"github.com/lsm/fiso/internal/link/circuitbreaker"
	"github.com/lsm/fiso/internal/link/discovery"
	"github.com/lsm/fiso/internal/link/proxy"
	"github.com/lsm/fiso/internal/link/ratelimit"
	"github.com/lsm/fiso/internal/observability"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse CLI flags
	var (
		portFlag        = flag.Int("port", 0, "Override listen port (e.g., 8081)")
		configFlag      = flag.String("config", "", "Path to config file")
		metricsPortFlag = flag.Int("metrics-port", 0, "Override metrics port")
		logLevelFlag    = flag.String("log-level", "", "Log level (debug, info, warn, error). Can also be set via FISO_LOG_LEVEL env var.")
	)
	flag.Parse()

	level := observability.GetLogLevel(*logLevelFlag)
	logger := observability.NewLogger("fiso-link", level)
	slog.SetDefault(logger)

	slog.Debug("starting fiso-link", "log_level", level.String())

	configPath := *configFlag
	if configPath == "" {
		configPath = os.Getenv("FISO_LINK_CONFIG")
	}
	if configPath == "" {
		configPath = "/etc/fiso/link/config.yaml"
	}

	cfg, err := link.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Apply CLI overrides
	if *portFlag > 0 {
		cfg.ListenAddr = fmt.Sprintf(":%d", *portFlag)
	}
	if *metricsPortFlag > 0 {
		cfg.MetricsAddr = fmt.Sprintf(":%d", *metricsPortFlag)
	}

	logger.Info("loaded config", "targets", len(cfg.Targets), "listenAddr", cfg.ListenAddr)

	// Initialize Kafka cluster registry and publisher pool
	clusterRegistry := kafka.NewRegistry()

	// Load kafka.clusters
	if len(cfg.Kafka.Clusters) > 0 {
		if err := clusterRegistry.LoadFromMap(cfg.Kafka.Clusters); err != nil {
			return fmt.Errorf("load kafka clusters: %w", err)
		}
		logger.Info("loaded kafka clusters", "clusters", clusterRegistry.Names())
	}

	// Create publisher pool
	publisherPool := kafka.NewPublisherPool(clusterRegistry)
	defer func() {
		if err := publisherPool.Close(); err != nil {
			logger.Error("failed to close kafka publisher pool", "error", err)
		}
	}()

	// Setup metrics
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(collectors.NewGoCollector())
	metrics := link.NewMetrics(reg)

	// Build circuit breakers
	breakers := make(map[string]*circuitbreaker.Breaker)
	for _, t := range cfg.Targets {
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

	// Build auth provider
	var authConfigs []auth.SecretConfig
	for _, t := range cfg.Targets {
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

	// Build rate limiter
	rateLimiter := ratelimit.New()
	for _, t := range cfg.Targets {
		if t.RateLimit.RequestsPerSecond > 0 {
			rateLimiter.Set(t.Name, t.RateLimit.RequestsPerSecond, t.RateLimit.Burst)
		}
	}

	// Build target store
	store := link.NewTargetStore(cfg.Targets)

	// Build proxy handler
	handlerCfg := proxy.Config{
		Targets:       store,
		Breakers:      breakers,
		RateLimiter:   rateLimiter,
		Auth:          authProvider,
		Resolver:      discovery.NewDNSResolver(),
		Metrics:       metrics,
		Logger:        logger,
		KafkaRegistry: clusterRegistry,
		KafkaPool:     publisherPool,
	}
	handler := proxy.NewHandler(handlerCfg)

	// Health server
	health := observability.NewHealthServer()

	// Metrics + health HTTP server
	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	metricsMux.Handle("GET /healthz", health.Handler())
	metricsMux.Handle("GET /readyz", health.Handler())

	metricsServer := &http.Server{
		Addr:    cfg.MetricsAddr,
		Handler: metricsMux,
	}

	// Proxy server
	proxyMux := http.NewServeMux()
	proxyMux.Handle("/link/", handler)

	proxyServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: proxyMux,
	}

	// Signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start servers
	errCh := make(chan error, 2)
	go func() {
		logger.Info("metrics server starting", "addr", cfg.MetricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("metrics server: %w", err)
		}
	}()
	go func() {
		logger.Info("proxy server starting", "addr", cfg.ListenAddr)
		if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("proxy server: %w", err)
		}
	}()

	health.SetReady(true)
	logger.Info("fiso-link started")

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-errCh:
		return err
	}

	health.SetReady(false)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := proxyServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("proxy server shutdown error", "error", err)
	}
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("metrics server shutdown error", "error", err)
	}

	logger.Info("shutdown complete")
	return nil
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
