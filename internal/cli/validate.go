package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lsm/fiso/internal/config"
	"github.com/lsm/fiso/internal/link"
	unifiedxform "github.com/lsm/fiso/internal/transform/unified"
)

// RunValidate validates flow and link configuration files.
func RunValidate(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("Usage: fiso validate [path]\n\nValidates all flow YAML files in the given directory (default: ./flows).\nAlso validates link target configs if found alongside the flows directory.")
		return nil
	}

	dir := "./fiso/flows"
	if len(args) > 0 && args[0] != "" {
		dir = args[0]
	}

	var allErrors []validationError

	flowErrors, err := validateFlowDir(dir)
	if err != nil {
		return fmt.Errorf("scanning %s: %w", dir, err)
	}
	allErrors = append(allErrors, flowErrors...)

	// Check for link config files alongside the flows directory
	parent := filepath.Dir(dir)
	linkSearchPaths := []string{
		filepath.Join(parent, "link", "config.yaml"),
		filepath.Join(parent, "link-config.yaml"),
		filepath.Join(parent, "links.yaml"),
	}
	for _, linkPath := range linkSearchPaths {
		if _, err := os.Stat(linkPath); err == nil {
			linkErrors := validateLinkConfig(linkPath)
			allErrors = append(allErrors, linkErrors...)
		}
	}

	if len(allErrors) == 0 {
		fmt.Println("All configurations are valid.")
		return nil
	}

	fmt.Fprintf(os.Stderr, "Found %d validation error(s):\n\n", len(allErrors))
	for _, ve := range allErrors {
		fmt.Fprintf(os.Stderr, "  %s\n    field: %s\n    error: %s\n\n", ve.File, ve.Field, ve.Message)
	}

	return fmt.Errorf("%d validation error(s) found", len(allErrors))
}

type validationError struct {
	File    string
	Field   string
	Message string
}

func validateFlowDir(dir string) ([]validationError, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var errs []validationError
	fileCount := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		fileCount++
		errs = append(errs, validateFlowFile(path)...)
	}

	if fileCount == 0 {
		fmt.Fprintf(os.Stderr, "warning: no YAML files found in %s\n", dir)
	} else {
		fmt.Printf("Validated %d flow file(s) in %s\n", fileCount, dir)
	}

	return errs, nil
}

func validateFlowFile(path string) []validationError {
	var errs []validationError

	data, err := os.ReadFile(path)
	if err != nil {
		return []validationError{{File: path, Field: "-", Message: fmt.Sprintf("read error: %v", err)}}
	}

	var flow config.FlowDefinition
	if err := yaml.Unmarshal(data, &flow); err != nil {
		// Check if the error is related to YAML mapping values (often caused by unquoted ternary operators)
		errMsg := err.Error()
		if strings.Contains(errMsg, "mapping values") || strings.Contains(errMsg, "did not find expected key") {
			hint := "\n\nHint: If using CEL expressions with ':' or '?', quote the entire expression as a string.\n" +
				"Example: change `priority: data.urgent ? 1 : 2` to `priority: \"data.urgent ? 1 : 2\"`"
			return []validationError{{File: path, Field: "-", Message: fmt.Sprintf("YAML parse error: %v%s", err, hint)}}
		}
		return []validationError{{File: path, Field: "-", Message: fmt.Sprintf("YAML parse error: %v", err)}}
	}

	if err := flow.Validate(); err != nil {
		for _, msg := range splitErrors(err) {
			errs = append(errs, validationError{File: path, Field: inferField(msg), Message: msg})
		}
	}

	if flow.Transform != nil && len(flow.Transform.Fields) > 0 {
		if _, err := unifiedxform.NewTransformer(flow.Transform.Fields); err != nil {
			errs = append(errs, validationError{
				File:    path,
				Field:   "transform.fields",
				Message: err.Error(),
			})
		}
	}

	return errs
}

func validateLinkConfig(path string) []validationError {
	var errs []validationError

	data, err := os.ReadFile(path)
	if err != nil {
		return []validationError{{File: path, Field: "-", Message: fmt.Sprintf("read error: %v", err)}}
	}

	var cfg link.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		// Check if the error is related to YAML mapping values
		errMsg := err.Error()
		if strings.Contains(errMsg, "mapping values") || strings.Contains(errMsg, "did not find expected key") {
			hint := "\n\nHint: If using values with ':' in YAML (e.g., durations, URLs), quote them as strings.\n" +
				"Example: change `timeout: 30s` to `timeout: \"30s\"` if it contains special characters"
			return []validationError{{File: path, Field: "-", Message: fmt.Sprintf("YAML parse error: %v%s", err, hint)}}
		}
		return []validationError{{File: path, Field: "-", Message: fmt.Sprintf("YAML parse error: %v", err)}}
	}

	if err := cfg.Validate(); err != nil {
		for _, msg := range splitErrors(err) {
			errs = append(errs, validationError{File: path, Field: inferField(msg), Message: msg})
		}
	}

	return errs
}

// splitErrors breaks an errors.Join result into individual error strings.
func splitErrors(err error) []string {
	if err == nil {
		return nil
	}
	parts := strings.Split(err.Error(), "\n")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// inferField extracts a field name from an error message.
func inferField(msg string) string {
	if idx := strings.Index(msg, ": "); idx > 0 {
		prefix := msg[:idx]
		if strings.Contains(prefix, "[") {
			remainder := msg[idx+2:]
			parts := strings.Fields(remainder)
			if len(parts) > 0 {
				return parts[0]
			}
		}
	}
	parts := strings.Fields(msg)
	if len(parts) > 0 {
		return parts[0]
	}
	return "-"
}
