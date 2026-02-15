//go:build wasmer

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lsm/fiso/internal/observability"
	"github.com/lsm/fiso/internal/wasmer"
	"gopkg.in/yaml.v3"
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
	logger.Info("this is the all-in-one binary: Flow + Wasmer + Link")

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

	logger.Info("loaded config",
		"flow_dir", cfg.Flow.ConfigDir,
		"link_config", cfg.Link.ConfigPath,
		"wasmer_apps", len(cfg.Wasmer.Apps))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start Wasmer apps
	appManager := wasmer.NewManager()
	defer appManager.StopAll(context.Background())

	for _, appCfg := range cfg.Wasmer.Apps {
		if err := appManager.StartApp(ctx, appCfg); err != nil {
			logger.Error("failed to start wasmer app", "name", appCfg.Name, "error", err)
			appManager.StopAll(ctx)
			return fmt.Errorf("start wasmer app %s: %w", appCfg.Name, err)
		}
		app, _ := appManager.GetApp(appCfg.Name)
		logger.Info("started wasmer app", "name", appCfg.Name, "addr", app.Addr)
	}

	// TODO: Start Flow pipelines
	// TODO: Start Link proxy

	logger.Info("all components started")
	logger.Info("Note: Full Flow and Link integration requires additional implementation")
	logger.Info("For now, Wasmer apps are running. Press Ctrl+C to stop.")

	<-ctx.Done()

	logger.Info("shutting down")
	logger.Info("shutdown complete")
	return nil
}
