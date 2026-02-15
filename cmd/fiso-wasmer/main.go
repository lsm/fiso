//go:build wasmer

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/lsm/fiso/internal/wasmer"
	"gopkg.in/yaml.v3"
)

func main() {
	cfgPath := flag.String("config", "", "Path to configuration file")
	module := flag.String("module", "", "Path to WASM module (overrides config)")
	port := flag.Int("port", 0, "Port to listen on (0 = auto-allocate)")
	flag.Parse()

	if *cfgPath == "" && *module == "" {
		fmt.Fprintln(os.Stderr, "Error: either -config or -module is required")
		flag.Usage()
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	manager := wasmer.NewManager()

	if *module != "" {
		// Single module mode
		cfg := wasmer.AppConfig{
			Name:   "main",
			Module: *module,
			Port:   *port,
		}
		if err := manager.StartApp(ctx, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting app: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Config file mode
		data, err := os.ReadFile(*cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
			os.Exit(1)
		}

		var cfg wasmer.ManagerConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing config: %v\n", err)
			os.Exit(1)
		}

		for _, appCfg := range cfg.Apps {
			if err := manager.StartApp(ctx, appCfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error starting app %s: %v\n", appCfg.Name, err)
				// Stop already started apps
				manager.StopAll(ctx)
				os.Exit(1)
			}
			fmt.Printf("Started app %s\n", appCfg.Name)
		}
	}

	// List running apps
	apps := manager.ListApps()
	for _, app := range apps {
		fmt.Printf("App %s running at %s\n", app.Name, app.Addr)
	}

	fmt.Println("Press Ctrl+C to stop")
	<-ctx.Done()

	fmt.Println("\nShutting down...")
	if err := manager.StopAll(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping apps: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Stopped")
}
