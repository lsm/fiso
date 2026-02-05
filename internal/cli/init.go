package cli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/template"
)

type initConfig struct {
	ProjectName     string
	Source          string // "http" or "kafka"
	Sink            string // "http" or "temporal"
	Transform       string // "none", "cel", or "mapping"
	CloudEvents     bool
	IncludeKafka    bool // derived: Source == "kafka"
	IncludeTemporal bool // derived: Sink == "temporal"
	IncludeK8s      bool // scaffold fiso/deploy/ with Kustomize manifests
}

// flowTemplateData extends initConfig with a FlowName for the flow template.
type flowTemplateData struct {
	initConfig
	FlowName string
}

// RunInit scaffolds Fiso infrastructure under a "fiso/" subdirectory.
// When running interactively (TTY), prompts for source, sink, transform,
// and CloudEvents options. Use --defaults to skip prompts.
// Supports flags for non-interactive mode: --source, --sink, --transform, --cloudevents, --k8s
func RunInit(args []string, stdin io.Reader) error {
	// Check for help flags before parsing
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("Usage: fiso init [options]\n\n" +
			"Scaffolds Fiso infrastructure in a fiso/ subdirectory.\n\n" +
			"Options:\n" +
			"  --source <type>      Source type (http, kafka)\n" +
			"  --sink <type>        Sink type (http, temporal)\n" +
			"  --transform <type>   Transform type (none, fields)\n" +
			"  --cloudevents        Customize CloudEvents envelope\n" +
			"  --k8s                Include Kubernetes deployment manifests\n" +
			"  --defaults           Use default HTTP configuration (equivalent to --source=http --sink=http --transform=none)\n" +
			"\n" +
			"Flags override all other settings. If some flags are provided, prompts will only\n" +
			"be asked for the remaining unset values. Use --defaults or pipe input for fully\n" +
			"non-interactive operation.")
		return nil
	}

	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var (
		flagSource      string
		flagSink        string
		flagTransform   string
		flagCloudEvents bool
		flagK8s         bool
		flagDefaults    bool
	)

	flags.StringVar(&flagSource, "source", "", "Source type (http, kafka)")
	flags.StringVar(&flagSink, "sink", "", "Sink type (http, temporal)")
	flags.StringVar(&flagTransform, "transform", "", "Transform type (none, fields)")
	flags.BoolVar(&flagCloudEvents, "cloudevents", false, "Customize CloudEvents envelope")
	flags.BoolVar(&flagK8s, "k8s", false, "Include Kubernetes deployment manifests")
	flags.BoolVar(&flagDefaults, "defaults", false, "Use default HTTP configuration")

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	cfg, err := buildConfig(stdin, flagSource, flagSink, flagTransform, flagCloudEvents, flagK8s, flagDefaults, args)
	if err != nil {
		return err
	}

	cfg.IncludeKafka = cfg.Source == "kafka"
	cfg.IncludeTemporal = cfg.Sink == "temporal"

	return scaffold(*cfg)
}

// buildConfig consolidates configuration logic from flags, prompts, and defaults.
// Priority: flags > prompts > defaults
func buildConfig(stdin io.Reader, flagSource, flagSink, flagTransform string, flagCloudEvents, flagK8s, flagDefaults bool, rawArgs []string) (*initConfig, error) {
	cfg := &initConfig{ProjectName: "fiso"}

	// Check if --defaults flag was passed
	useDefaults := false
	for _, arg := range rawArgs {
		if arg == "--defaults" {
			useDefaults = true
			break
		}
	}

	// Determine which values need prompting
	needSource := flagSource == ""
	needSink := flagSink == ""
	needTransform := flagTransform == ""
	needCloudEvents := !flagCloudEvents && !useDefaults
	needK8s := !flagK8s && !useDefaults

	// Apply flags if provided
	if flagSource != "" {
		cfg.Source = flagSource
	}
	if flagSink != "" {
		cfg.Sink = flagSink
	}
	if flagTransform != "" {
		cfg.Transform = flagTransform
	}
	if flagCloudEvents {
		cfg.CloudEvents = true
	}
	if flagK8s {
		cfg.IncludeK8s = true
	}

	// Apply defaults if no flags and not prompting
	if useDefaults || (!shouldPromptForValues(needSource, needSink, needTransform, needCloudEvents, needK8s, stdin)) {
		if cfg.Source == "" {
			cfg.Source = "http"
		}
		if cfg.Sink == "" {
			cfg.Sink = "http"
		}
		if cfg.Transform == "" {
			cfg.Transform = "none"
		}
		if !flagCloudEvents {
			cfg.CloudEvents = false
		}
		if !flagK8s {
			cfg.IncludeK8s = false
		}
		return cfg, nil
	}

	// Prompt for missing values
	p := newPrompter(stdin, os.Stdout)
	promptForChoices(p, cfg, needSource, needSink, needTransform, needCloudEvents, needK8s)

	return cfg, nil
}

// shouldPromptForValues determines if we should prompt based on what's needed and stdin availability
func shouldPromptForValues(needSource, needSink, needTransform, needCloudEvents, needK8s bool, stdin io.Reader) bool {
	// If nothing needs prompting, don't prompt
	if !needSource && !needSink && !needTransform && !needCloudEvents && !needK8s {
		return false
	}

	// If stdin was explicitly provided (non-nil), we're in test/pipe mode - prompt
	if stdin != nil {
		return true
	}

	// Check if real stdin is a terminal
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// promptForChoices prompts only for the values that haven't been set via flags
func promptForChoices(p *prompter, cfg *initConfig, needSource, needSink, needTransform, needCloudEvents, needK8s bool) {
	if needSource {
		sourceIdx := p.choose("Source type:", []string{"HTTP", "Kafka"}, 0)
		cfg.Source = []string{"http", "kafka"}[sourceIdx]
	}

	if needSink {
		sinkIdx := p.choose("Sink type:", []string{"HTTP", "Temporal"}, 0)
		cfg.Sink = []string{"http", "temporal"}[sinkIdx]
	}

	if needTransform {
		transformIdx := p.choose("Transform:", []string{"None", "Field-based transform (CEL expressions)"}, 0)
		cfg.Transform = []string{"none", "fields"}[transformIdx]
	}

	if needCloudEvents {
		cfg.CloudEvents = p.confirm("Customize CloudEvents envelope fields?", false)
	}

	if needK8s {
		cfg.IncludeK8s = p.confirm("Include Kubernetes deployment manifests?", false)
	}
}

// shouldPrompt returns true when interactive prompts should be shown.
// Returns false when --defaults is passed, when stdin is provided explicitly
// (indicating programmatic/test usage), or when stdin is not a terminal.
// DEPRECATED: Use buildConfig instead.
func shouldPrompt(args []string, stdin io.Reader) bool {
	for _, a := range args {
		if a == "--defaults" {
			return false
		}
	}
	// If stdin was explicitly provided (non-nil), we're in interactive/test mode
	if stdin != nil {
		return true
	}
	// Check if real stdin is a terminal
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func promptChoices(p *prompter, cfg *initConfig) {
	promptForChoices(p, cfg, true, true, true, true, true)
}

func scaffold(cfg initConfig) error {
	fisoDir := "fiso"
	flowsDir := filepath.Join(fisoDir, "flows")
	linkDir := filepath.Join(fisoDir, "link")

	dirs := []string{fisoDir, flowsDir, linkDir}
	if cfg.IncludeTemporal {
		dirs = append(dirs, filepath.Join(fisoDir, "temporal-worker"))
	} else {
		dirs = append(dirs, filepath.Join(fisoDir, "user-service"))
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	// Docker-compose (templated with conditional blocks)
	if err := writeTemplate(fisoDir, "docker-compose.yml", "templates/docker-compose.yml.tmpl", cfg); err != nil {
		return err
	}

	// Flow definition (single templated file)
	flowName := deriveFlowName(cfg.Source, cfg.Sink)
	flowData := flowTemplateData{initConfig: cfg, FlowName: flowName}
	if err := writeTemplate(flowsDir, flowName+".yaml", "templates/flow.yaml.tmpl", flowData); err != nil {
		return err
	}

	// Link config (always)
	if err := copyEmbedded(linkDir, "config.yaml", "templates/link-config.yaml"); err != nil {
		return err
	}

	// Prometheus (always)
	if err := copyEmbedded(fisoDir, "prometheus.yml", "templates/prometheus.yml"); err != nil {
		return err
	}

	// Service scaffold
	if cfg.IncludeTemporal {
		workerDir := filepath.Join(fisoDir, "temporal-worker")
		workerFiles := map[string]string{
			"main.go":     "templates/temporal-worker/main.go.tmpl",
			"workflow.go": "templates/temporal-worker/workflow.go.tmpl",
			"activity.go": "templates/temporal-worker/activity.go.tmpl",
			"Dockerfile":  "templates/temporal-worker/Dockerfile.tmpl",
			"go.mod":      "templates/temporal-worker/go.mod.tmpl",
		}
		for dest, src := range workerFiles {
			if err := copyEmbedded(workerDir, dest, src); err != nil {
				return err
			}
		}
	} else {
		userServiceDir := filepath.Join(fisoDir, "user-service")
		userServiceFiles := map[string]string{
			"main.go":    "templates/user-service/main.go.tmpl",
			"Dockerfile": "templates/user-service/Dockerfile.tmpl",
			"go.mod":     "templates/user-service/go.mod.tmpl",
		}
		for dest, src := range userServiceFiles {
			if err := copyEmbedded(userServiceDir, dest, src); err != nil {
				return err
			}
		}
	}

	// .gitignore at project root (only if not present)
	if _, err := os.Stat(".gitignore"); os.IsNotExist(err) {
		if err := copyEmbedded(".", ".gitignore", "templates/gitignore"); err != nil {
			return err
		}
	}

	// Kubernetes deployment manifests (Kustomize)
	if cfg.IncludeK8s {
		deployDir := filepath.Join(fisoDir, "deploy")
		if err := os.MkdirAll(deployDir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", deployDir, err)
		}

		flowName := deriveFlowName(cfg.Source, cfg.Sink)
		flowData := flowTemplateData{initConfig: cfg, FlowName: flowName}

		if err := writeTemplate(deployDir, "kustomization.yaml", "templates/deploy/kustomization.yaml.tmpl", flowData); err != nil {
			return err
		}
		if err := writeTemplate(deployDir, "flow-deployment.yaml", "templates/deploy/flow-deployment.yaml.tmpl", flowData); err != nil {
			return err
		}
		if err := copyEmbedded(deployDir, "namespace.yaml", "templates/deploy/namespace.yaml"); err != nil {
			return err
		}
		if err := copyEmbedded(deployDir, "link-deployment.yaml", "templates/deploy/link-deployment.yaml"); err != nil {
			return err
		}
	}

	printInitSummary(cfg)
	return nil
}

func deriveFlowName(source, sink string) string {
	if source == "http" && sink == "http" {
		return "example-flow"
	}
	return source + "-" + sink + "-flow"
}

func printInitSummary(cfg initConfig) {
	fmt.Printf("\nFiso initialized with %s source → %s sink.\n", cfg.Source, cfg.Sink)
	fmt.Println("")
	fmt.Println("  fiso/                   Fiso environment (docker-compose, prometheus)")
	fmt.Println("  fiso/flows/             Your pipeline definitions")
	fmt.Println("  fiso/link/              Egress proxy target configs")
	if cfg.IncludeTemporal {
		fmt.Println("  fiso/temporal-worker/   Temporal workflow worker (edit or replace)")
	} else {
		fmt.Println("  fiso/user-service/      Example backend service (edit or replace)")
	}
	if cfg.IncludeK8s {
		fmt.Println("  fiso/deploy/            Kubernetes manifests (kubectl apply -k fiso/deploy/)")
	}
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  fiso dev                Start local development environment")
	fmt.Println("  fiso validate           Validate flow and link configurations")
}

func writeTemplate(dir, filename, tmplPath string, data interface{}) error {
	tmplBytes, err := templateFS.ReadFile(tmplPath)
	if err != nil {
		return fmt.Errorf("read template %s: %w", tmplPath, err)
	}

	tmpl, err := template.New(filename).Parse(string(tmplBytes))
	if err != nil {
		return fmt.Errorf("parse template %s: %w", tmplPath, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute template %s: %w", tmplPath, err)
	}

	outPath := filepath.Join(dir, filename)
	if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}

	return nil
}

func copyEmbedded(dir, filename, embedPath string) error {
	data, err := templateFS.ReadFile(embedPath)
	if err != nil {
		return fmt.Errorf("read embedded %s: %w", embedPath, err)
	}

	outPath := filepath.Join(dir, filename)
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}

	return nil
}
