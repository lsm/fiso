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

	"github.com/lsm/fiso/internal/observability"
	"github.com/lsm/fiso/internal/operator/webhook"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := observability.NewLogger("fiso-operator", slog.LevelInfo)
	slog.SetDefault(logger)

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
