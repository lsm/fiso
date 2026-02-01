package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

type initConfig struct {
	ProjectName string
}

// RunInit scaffolds Fiso infrastructure under a "fiso/" subdirectory.
// The user's business logic (flow definitions) lives in "flows/" at the
// project root, while all Fiso runtime files go into "fiso/".
func RunInit(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("Usage: fiso init\n\nScaffolds Fiso infrastructure in a fiso/ subdirectory and\ncreates a flows/ directory for your pipeline definitions.")
		return nil
	}

	fisoDir := "fiso"
	flowsDir := filepath.Join(fisoDir, "flows")

	if err := os.MkdirAll(fisoDir, 0755); err != nil {
		return fmt.Errorf("create fiso directory: %w", err)
	}
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		return fmt.Errorf("create flows directory: %w", err)
	}

	cfg := initConfig{ProjectName: "fiso"}
	if err := writeTemplate(fisoDir, "docker-compose.yml", "templates/docker-compose.yml.tmpl", cfg); err != nil {
		return err
	}

	if err := copyEmbedded(flowsDir, "example-flow.yaml", "templates/sample-flow.yaml"); err != nil {
		return err
	}
	if err := copyEmbedded(fisoDir, "prometheus.yml", "templates/prometheus.yml"); err != nil {
		return err
	}

	// .gitignore goes at the project root if not already present
	if _, err := os.Stat(".gitignore"); os.IsNotExist(err) {
		if err := copyEmbedded(".", ".gitignore", "templates/gitignore"); err != nil {
			return err
		}
	}

	fmt.Println("Fiso initialized.")
	fmt.Println("")
	fmt.Println("  fiso/                 Fiso environment (docker-compose, prometheus)")
	fmt.Println("  fiso/flows/           Your pipeline definitions")
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  fiso dev              Start local development environment")
	fmt.Println("  fiso validate         Validate flow configurations")

	return nil
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
