package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lsm/fiso/internal/config"
	unifiedxform "github.com/lsm/fiso/internal/transform/unified"
)

// RunTransform dispatches transform subcommands.
func RunTransform(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println(`Usage: fiso transform <command> [arguments]

Commands:
  test    Test a transform configuration (dry-run)

Run 'fiso transform <command> -h' for help on a specific command.`)
		return nil
	}

	switch args[0] {
	case "test":
		return RunTransformTest(args[1:])
	default:
		return fmt.Errorf("unknown transform subcommand %q\nRun 'fiso transform -h' for usage", args[0])
	}
}

// RunTransformTest performs a dry-run test of a transform configuration.
func RunTransformTest(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println(`Usage: fiso transform test --flow <path> --input <json|file>

Test a transform configuration without starting the full stack.

Options:
  --flow <path>    Path to flow YAML file (required)
  --input <data>   Input JSON as string or path to JSON file (required)

Examples:
  # Test with inline JSON
  fiso transform test --flow fiso/flows/order-flow.yaml --input '{"order_id":"TEST-001","customer_id":"CUST-123"}'

  # Test with JSON file
  fiso transform test --flow fiso/flows/order-flow.yaml --input sample-order.json

  # Test with JSONL file (first line only)
  fiso transform test --flow fiso/flows/order-flow.yaml --input sample-orders.jsonl`)
		return nil
	}

	var flowPath, inputData string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--flow" && i+1 < len(args):
			flowPath = args[i+1]
			i++
		case args[i] == "--input" && i+1 < len(args):
			inputData = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--flow="):
			flowPath = strings.TrimPrefix(args[i], "--flow=")
		case strings.HasPrefix(args[i], "--input="):
			inputData = strings.TrimPrefix(args[i], "--input=")
		}
	}

	if flowPath == "" {
		return fmt.Errorf("--flow is required")
	}
	if inputData == "" {
		return fmt.Errorf("--input is required")
	}

	// Load flow configuration
	flow, err := loadFlow(flowPath)
	if err != nil {
		return fmt.Errorf("load flow: %w", err)
	}

	if flow.Transform == nil || len(flow.Transform.Fields) == 0 {
		return fmt.Errorf("flow %q does not have a transform configuration", flow.Name)
	}

	// Load input data
	inputJSON, err := loadInput(inputData)
	if err != nil {
		return fmt.Errorf("load input: %w", err)
	}

	// Create transformer
	transformer, err := unifiedxform.NewTransformer(flow.Transform.Fields)
	if err != nil {
		return fmt.Errorf("create transformer: %w", err)
	}

	// Apply transform
	ctx := context.Background()
	outputJSON, err := transformer.Transform(ctx, inputJSON)
	if err != nil {
		return fmt.Errorf("transform error: %w", err)
	}

	// Pretty-print result
	var output interface{}
	if err := json.Unmarshal(outputJSON, &output); err != nil {
		return fmt.Errorf("parse output: %w", err)
	}
	pretty, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("format output: %w", err)
	}

	fmt.Println(string(pretty))
	return nil
}

func loadFlow(path string) (*config.FlowDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var flow config.FlowDefinition
	if err := yaml.Unmarshal(data, &flow); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	if err := flow.Validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}

	return &flow, nil
}

func loadInput(input string) ([]byte, error) {
	// Check if it's a file path
	if _, err := os.Stat(input); err == nil {
		data, err := os.ReadFile(filepath.Clean(input))
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}
		// Handle JSONL: use first line only
		dataStr := string(data)
		if idx := strings.IndexByte(dataStr, '\n'); idx != -1 {
			dataStr = dataStr[:idx]
		}
		return []byte(dataStr), nil
	}

	// Treat as inline JSON
	return []byte(input), nil
}
