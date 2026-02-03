package cli

import (
	"bytes"
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
func RunInit(args []string, stdin io.Reader) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("Usage: fiso init [--defaults]\n\nScaffolds Fiso infrastructure in a fiso/ subdirectory.\nUse --defaults to skip interactive prompts and use HTTP defaults.")
		return nil
	}

	cfg := initConfig{ProjectName: "fiso"}

	if shouldPrompt(args, stdin) {
		p := newPrompter(stdin, os.Stdout)
		promptChoices(p, &cfg)
	} else {
		cfg.Source = "http"
		cfg.Sink = "http"
		cfg.Transform = "none"
		cfg.CloudEvents = false
		cfg.IncludeK8s = false
	}

	cfg.IncludeKafka = cfg.Source == "kafka"
	cfg.IncludeTemporal = cfg.Sink == "temporal"

	return scaffold(cfg)
}

// shouldPrompt returns true when interactive prompts should be shown.
// Returns false when --defaults is passed, when stdin is provided explicitly
// (indicating programmatic/test usage), or when stdin is not a terminal.
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
	sourceIdx := p.choose("Source type:", []string{"HTTP", "Kafka"}, 0)
	cfg.Source = []string{"http", "kafka"}[sourceIdx]

	sinkIdx := p.choose("Sink type:", []string{"HTTP", "Temporal"}, 0)
	cfg.Sink = []string{"http", "temporal"}[sinkIdx]

	transformIdx := p.choose("Transform:", []string{"None", "CEL expression", "YAML field mapping"}, 0)
	cfg.Transform = []string{"none", "cel", "mapping"}[transformIdx]

	cfg.CloudEvents = p.confirm("Customize CloudEvents envelope fields?", false)

	cfg.IncludeK8s = p.confirm("Include Kubernetes deployment manifests?", false)
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
