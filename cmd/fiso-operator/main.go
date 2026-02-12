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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	fisov1alpha1 "github.com/lsm/fiso/api/v1alpha1"
	"github.com/lsm/fiso/internal/observability"
	"github.com/lsm/fiso/internal/operator/controller"
	"github.com/lsm/fiso/internal/operator/webhook"
)

var scheme = runtime.NewScheme()

var logLevel string

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fisov1alpha1.AddToScheme(scheme))
}

func main() {
	flag.StringVar(&logLevel, "log-level", "", "Log level (debug, info, warn, error). Can also be set via FISO_LOG_LEVEL env var.")
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	level := observability.GetLogLevel(logLevel)
	logger := observability.NewLogger("fiso-operator", level)
	slog.SetDefault(logger)

	slog.Debug("starting fiso-operator", "log_level", level.String())

	mode := os.Getenv("FISO_OPERATOR_MODE")
	if mode == "" {
		mode = "controller"
	}

	if mode == "webhook-only" {
		return runWebhookOnly(logger)
	}

	return runController(logger)
}

func runController(logger *slog.Logger) error {
	// Setup controller-runtime logging
	ctrllog.SetLogger(logr.FromSlogHandler(logger.Handler()))

	metricsAddr := envOrDefault("FISO_METRICS_ADDR", ":8080")
	healthAddr := envOrDefault("FISO_HEALTH_ADDR", ":9090")
	enableLeaderElection := os.Getenv("FISO_ENABLE_LEADER_ELECTION") == "true"

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: healthAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "fiso-operator-leader.fiso.io",
	})
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	// Setup controllers
	if err := (&controller.FlowDefinitionReconciler{
		Client: mgr.GetClient(),
		Logger: logger,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup FlowDefinition controller: %w", err)
	}

	if err := (&controller.LinkTargetReconciler{
		Client: mgr.GetClient(),
		Logger: logger,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup LinkTarget controller: %w", err)
	}

	// Setup webhook
	sidecarCfg := webhook.DefaultSidecarConfig()
	if img := os.Getenv("FISO_LINK_IMAGE"); img != "" {
		sidecarCfg.Image = img
	}
	whHandler := webhook.NewWebhookHandler(sidecarCfg)
	mgr.GetWebhookServer().Register("/mutate", whHandler)

	// Health probes
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("add healthz: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("add readyz: %w", err)
	}

	logger.Info("starting operator", "mode", "controller", "metrics", metricsAddr, "health", healthAddr, "leaderElection", enableLeaderElection)
	return mgr.Start(ctrl.SetupSignalHandler())
}

func runWebhookOnly(logger *slog.Logger) error {
	webhookAddr := os.Getenv("FISO_WEBHOOK_ADDR")
	if webhookAddr == "" {
		webhookAddr = ":8443"
	}

	healthAddr := os.Getenv("FISO_HEALTH_ADDR")
	if healthAddr == "" {
		healthAddr = ":9090"
	}

	tlsCertFile := os.Getenv("FISO_TLS_CERT_FILE")
	if tlsCertFile == "" {
		tlsCertFile = "/etc/fiso/tls/tls.crt"
	}
	tlsKeyFile := os.Getenv("FISO_TLS_KEY_FILE")
	if tlsKeyFile == "" {
		tlsKeyFile = "/etc/fiso/tls/tls.key"
	}

	// Setup sidecar injection webhook
	sidecarCfg := webhook.DefaultSidecarConfig()
	if img := os.Getenv("FISO_LINK_IMAGE"); img != "" {
		sidecarCfg.Image = img
	}

	wh := webhook.NewWebhookHandler(sidecarCfg)

	// Webhook server
	whMux := http.NewServeMux()
	whMux.Handle("POST /mutate", wh)

	whServer := &http.Server{Addr: webhookAddr, Handler: whMux}

	// Health server
	health := observability.NewHealthServer()
	healthMux := http.NewServeMux()
	healthMux.Handle("GET /healthz", health.Handler())
	healthMux.Handle("GET /readyz", health.Handler())

	healthServer := &http.Server{Addr: healthAddr, Handler: healthMux}

	// Start servers
	go func() {
		if _, err := os.Stat(tlsCertFile); err == nil {
			logger.Info("webhook server starting with TLS", "addr", webhookAddr, "cert", tlsCertFile)
			if err := whServer.ListenAndServeTLS(tlsCertFile, tlsKeyFile); err != nil && err != http.ErrServerClosed {
				logger.Error("webhook server error", "error", err)
			}
		} else {
			logger.Warn("TLS cert not found, starting webhook without TLS (local dev only)", "cert", tlsCertFile)
			if err := whServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("webhook server error", "error", err)
			}
		}
	}()

	go func() {
		logger.Info("health server starting", "addr", healthAddr)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("health server error", "error", err)
		}
	}()

	health.SetReady(true)

	// Wait for shutdown signal
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	<-ctx.Done()
	logger.Info("shutting down")
	health.SetReady(false)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	_ = whServer.Shutdown(shutdownCtx)
	_ = healthServer.Shutdown(shutdownCtx)

	logger.Info("shutdown complete")
	return nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
