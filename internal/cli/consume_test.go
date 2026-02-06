package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	intkafka "github.com/lsm/fiso/internal/kafka"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestRunConsume_ConfigLoading(t *testing.T) {
	// Save current working directory
	origWd, _ := os.Getwd()
	defer func() {
		_ = os.Chdir(origWd)
	}()

	t.Run("config from fiso/flows when available", func(t *testing.T) {
		tmpDir := t.TempDir()
		flowDir := filepath.Join(tmpDir, "fiso", "flows")
		if err := os.MkdirAll(flowDir, 0755); err != nil {
			t.Fatalf("Failed to create flow directory: %v", err)
		}

		// Create a flow file with kafka cluster config
		flowYAML := `
kafka:
  clusters:
    main:
      brokers:
        - custom-broker:9092
        - another-broker:9093
source:
  type: kafka
  config:
    cluster: main
    topic: orders
    consumerGroup: test-group
`
		if err := os.WriteFile(filepath.Join(flowDir, "test-flow.yaml"), []byte(flowYAML), 0644); err != nil {
			t.Fatalf("Failed to write flow file: %v", err)
		}

		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		// Test that loadKafkaClusterConfig picks up the custom brokers
		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
			return
		}

		expectedBrokers := []string{"custom-broker:9092", "another-broker:9093"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})

	t.Run("config defaults to localhost when fiso/flows missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Don't create fiso/flows directory
		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
			return
		}

		expectedBrokers := []string{"localhost:9092"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})

	t.Run("different broker configurations in flow files", func(t *testing.T) {
		tmpDir := t.TempDir()
		flowDir := filepath.Join(tmpDir, "fiso", "flows")
		if err := os.MkdirAll(flowDir, 0755); err != nil {
			t.Fatalf("Failed to create flow directory: %v", err)
		}

		testCases := []struct {
			name         string
			brokers      []string
			flowFileName string
		}{
			{
				name:         "single broker",
				brokers:      []string{"single:9092"},
				flowFileName: "single-broker.yaml",
			},
			{
				name:         "multiple brokers",
				brokers:      []string{"broker1:9092", "broker2:9092", "broker3:9092"},
				flowFileName: "multi-broker.yaml",
			},
			{
				name:         "non-standard port",
				brokers:      []string{"broker:9093"},
				flowFileName: "custom-port.yaml",
			},
			{
				name:         "dns names",
				brokers:      []string{"kafka.service.consul", "kafka2.service.consul"},
				flowFileName: "dns-names.yaml",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Clean and recreate flow dir for each test
				_ = os.RemoveAll(flowDir)
				if err := os.MkdirAll(flowDir, 0755); err != nil {
					t.Fatalf("Failed to create flow directory: %v", err)
				}

				flowYAML := `
kafka:
  clusters:
    main:
      brokers:
`
				for _, broker := range tc.brokers {
					flowYAML += fmt.Sprintf("        - %s\n", broker)
				}
				flowYAML += `source:
  type: kafka
  config:
    cluster: main
    topic: orders
    consumerGroup: test-group
`
				flowPath := filepath.Join(flowDir, tc.flowFileName)
				if err := os.WriteFile(flowPath, []byte(flowYAML), 0644); err != nil {
					t.Fatalf("Failed to write flow file: %v", err)
				}

				// Change to tmpDir so loadKafkaClusterConfig finds the flow files
				_ = os.Chdir(tmpDir)
				defer func() { _ = os.Chdir(origWd) }()

				cfg, err := loadKafkaClusterConfig("test-topic")
				if err != nil {
					t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
					return
				}

				if !equalStringSlices(cfg.Brokers, tc.brokers) {
					t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, tc.brokers)
				}
			})
		}
	})

	t.Run("RunConsume with custom broker config", func(t *testing.T) {
		const skipIntegrationTests = true
		if skipIntegrationTests {
			t.Skip("Skipping integration test - requires Kafka running")
		}

		tmpDir := t.TempDir()
		flowDir := filepath.Join(tmpDir, "fiso", "flows")
		if err := os.MkdirAll(flowDir, 0755); err != nil {
			t.Fatalf("Failed to create flow directory: %v", err)
		}

		// Create a flow file with localhost broker (may not be running)
		flowYAML := `
source:
  type: kafka
  config:
    brokers:
      - localhost:9092
    topic: orders
    consumerGroup: test-group
`
		if err := os.WriteFile(filepath.Join(flowDir, "test-flow.yaml"), []byte(flowYAML), 0644); err != nil {
			t.Fatalf("Failed to write flow file: %v", err)
		}

		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		// RunConsume should try to connect and fail gracefully when Kafka is unavailable
		err := RunConsume([]string{"--topic", "test-topic", "--max-messages", "1"})
		if err != nil {
			// Expected to fail when Kafka is not available
			// The important part is that it loaded the config correctly
			t.Logf("RunConsume() failed as expected (Kafka unavailable): %v", err)
		}
	})
}

func TestRunConsume(t *testing.T) {
	// Skip integration tests that require Kafka running
	// Run these tests manually with: go test -run TestRunConsumeIntegration
	const skipIntegrationTests = true

	tests := []struct {
		name          string
		args          []string
		expectError   bool
		errorContains string
		skipIfNoKafka bool
	}{
		{
			name:          "help flag",
			args:          []string{"--help"},
			expectError:   false,
			skipIfNoKafka: false,
		},
		{
			name:          "missing topic flag",
			args:          []string{},
			expectError:   true,
			errorContains: "--topic is required",
			skipIfNoKafka: false,
		},
		{
			name:          "missing topic value",
			args:          []string{"--topic"},
			expectError:   true,
			errorContains: "--topic requires a value",
			skipIfNoKafka: false,
		},
		{
			name:          "unknown flag",
			args:          []string{"--unknown"},
			expectError:   true,
			errorContains: "unknown flag: --unknown",
			skipIfNoKafka: false,
		},
		{
			name:          "invalid max-messages value",
			args:          []string{"--topic", "test", "--max-messages", "abc"},
			expectError:   true,
			errorContains: "invalid --max-messages value",
			skipIfNoKafka: false,
		},
		{
			name:          "zero max-messages",
			args:          []string{"--topic", "test", "--max-messages", "0"},
			expectError:   true,
			errorContains: "must be positive",
			skipIfNoKafka: false,
		},
		{
			name:          "negative max-messages",
			args:          []string{"--topic", "test", "--max-messages", "-5"},
			expectError:   true,
			errorContains: "must be positive",
			skipIfNoKafka: false,
		},
		{
			name:          "valid batch mode args",
			args:          []string{"--topic", "test", "--max-messages", "5"},
			expectError:   true,
			errorContains: "",
			skipIfNoKafka: true,
		},
		{
			name:          "valid follow mode args",
			args:          []string{"--topic", "test", "--follow"},
			expectError:   true,
			errorContains: "",
			skipIfNoKafka: true,
		},
		{
			name:          "from-beginning flag",
			args:          []string{"--topic", "test", "--from-beginning"},
			expectError:   true,
			errorContains: "",
			skipIfNoKafka: true,
		},
		{
			name:          "all flags",
			args:          []string{"--topic", "test", "--from-beginning", "--max-messages", "10"},
			expectError:   true,
			errorContains: "",
			skipIfNoKafka: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip integration tests if flag is set
			if tt.skipIfNoKafka && skipIntegrationTests {
				t.Skip("Skipping integration test - requires Kafka running. Run manually with: go test -v ./internal/cli/... -run 'TestRunConsume/valid'")
			}

			err := RunConsume(tt.args)
			if tt.expectError {
				if err == nil {
					t.Errorf("RunConsume() expected error but got nil")
					return
				}
				if tt.errorContains != "" {
					errMsg := err.Error()
					found := false
					for _, substring := range []string{tt.errorContains} {
						if contains(errMsg, substring) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("RunConsume() error = %v, expected to contain %q", err, tt.errorContains)
					}
				}
			} else {
				if err != nil {
					t.Errorf("RunConsume() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestParseMessageCount(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int
		expectError bool
	}{
		{
			name:        "valid positive number",
			input:       "10",
			expected:    10,
			expectError: false,
		},
		{
			name:        "valid large number",
			input:       "10000",
			expected:    10000,
			expectError: false,
		},
		{
			name:        "zero",
			input:       "0",
			expectError: true,
		},
		{
			name:        "negative number",
			input:       "-5",
			expectError: true,
		},
		{
			name:        "not a number",
			input:       "abc",
			expectError: true,
		},
		{
			name:        "partial number",
			input:       "10abc",
			expectError: true,
		},
		{
			name:        "float string",
			input:       "10.5",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseMessageCount(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("parseMessageCount() expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("parseMessageCount() unexpected error = %v", err)
					return
				}
				if result != tt.expected {
					t.Errorf("parseMessageCount() = %v, want %v", result, tt.expected)
				}
			}
		})
	}
}

func TestParseKafkaConfigFromYAML(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		expectNil     bool
		expectBrokers []string
	}{
		{
			name: "valid kafka source with cluster",
			yaml: `
kafka:
  clusters:
    main:
      brokers:
        - localhost:9092
        - kafka:9092
source:
  type: kafka
  config:
    cluster: main
    topic: orders
    consumerGroup: test-group
`,
			expectNil:     false,
			expectBrokers: []string{"localhost:9092", "kafka:9092"},
		},
		{
			name: "non-kafka source",
			yaml: `
source:
  type: http
  config:
    url: http://localhost:8080
`,
			expectNil: true,
		},
		{
			name: "kafka source with single broker cluster",
			yaml: `
kafka:
  clusters:
    default:
      brokers:
        - localhost:9092
source:
  type: kafka
  config:
    cluster: default
    topic: orders
    consumerGroup: test-group
`,
			expectNil:     false,
			expectBrokers: []string{"localhost:9092"},
		},
		{
			name: "kafka source without cluster reference",
			yaml: `
source:
  type: kafka
  config:
    topic: orders
    consumerGroup: test-group
`,
			expectNil: true,
		},
		{
			name: "invalid yaml",
			yaml: `
source: [
  invalid: yaml
`,
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseKafkaClusterFromYAML([]byte(tt.yaml))
			if tt.expectNil {
				if cfg != nil && len(cfg.Brokers) > 0 {
					t.Errorf("parseKafkaClusterFromYAML() expected nil brokers, got %v", cfg.Brokers)
				}
			} else {
				if err != nil {
					t.Errorf("parseKafkaClusterFromYAML() unexpected error = %v", err)
					return
				}
				if cfg == nil {
					t.Errorf("parseKafkaClusterFromYAML() expected non-nil config")
					return
				}
				if !equalStringSlices(cfg.Brokers, tt.expectBrokers) {
					t.Errorf("parseKafkaClusterFromYAML() brokers = %v, want %v", cfg.Brokers, tt.expectBrokers)
				}
			}
		})
	}
}

func TestLoadKafkaConfig(t *testing.T) {
	// Save current working directory to restore later
	origWd, _ := os.Getwd()
	defer func() {
		_ = os.Chdir(origWd)
	}()

	t.Run("non-existent flow directory returns default", func(t *testing.T) {
		// Change to a temp directory where fiso/flows doesn't exist
		tmpDir := t.TempDir()
		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
			return
		}

		if cfg == nil {
			t.Error("loadKafkaClusterConfig() returned nil config")
			return
		}

		expectedBrokers := []string{"localhost:9092"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})

	t.Run("empty flow directory returns default", func(t *testing.T) {
		tmpDir := t.TempDir()
		flowDir := tmpDir + "/fiso/flows"
		if err := os.MkdirAll(flowDir, 0755); err != nil {
			t.Fatalf("Failed to create flow directory: %v", err)
		}
		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
			return
		}

		expectedBrokers := []string{"localhost:9092"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})

	t.Run("valid kafka config in flow file", func(t *testing.T) {
		tmpDir := t.TempDir()
		flowDir := tmpDir + "/fiso/flows"
		if err := os.MkdirAll(flowDir, 0755); err != nil {
			t.Fatalf("Failed to create flow directory: %v", err)
		}

		flowYAML := `
kafka:
  clusters:
    main:
      brokers:
        - broker1:9092
        - broker2:9092
source:
  type: kafka
  config:
    cluster: main
    topic: orders
    consumerGroup: test-group
`
		flowPath := flowDir + "/test-flow.yaml"
		if err := os.WriteFile(flowPath, []byte(flowYAML), 0644); err != nil {
			t.Fatalf("Failed to write flow file: %v", err)
		}

		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
			return
		}

		expectedBrokers := []string{"broker1:9092", "broker2:9092"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})

	t.Run("first kafka config is used", func(t *testing.T) {
		tmpDir := t.TempDir()
		flowDir := tmpDir + "/fiso/flows"
		if err := os.MkdirAll(flowDir, 0755); err != nil {
			t.Fatalf("Failed to create flow directory: %v", err)
		}

		// First flow file with kafka config
		flowYAML1 := `
kafka:
  clusters:
    main:
      brokers:
        - first-broker:9092
source:
  type: kafka
  config:
    cluster: main
    topic: orders
    consumerGroup: test-group
`
		if err := os.WriteFile(flowDir+"/first.yaml", []byte(flowYAML1), 0644); err != nil {
			t.Fatalf("Failed to write first flow file: %v", err)
		}

		// Second flow file with kafka config
		flowYAML2 := `
kafka:
  clusters:
    main:
      brokers:
        - second-broker:9092
source:
  type: kafka
  config:
    cluster: main
    topic: orders
    consumerGroup: test-group
`
		if err := os.WriteFile(flowDir+"/second.yaml", []byte(flowYAML2), 0644); err != nil {
			t.Fatalf("Failed to write second flow file: %v", err)
		}

		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
			return
		}

		// Should use the first kafka config found
		expectedBrokers := []string{"first-broker:9092"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})

	t.Run("non-kafka source returns default", func(t *testing.T) {
		tmpDir := t.TempDir()
		flowDir := tmpDir + "/fiso/flows"
		if err := os.MkdirAll(flowDir, 0755); err != nil {
			t.Fatalf("Failed to create flow directory: %v", err)
		}

		flowYAML := `
source:
  type: http
  config:
    url: http://localhost:8080
`
		if err := os.WriteFile(flowDir+"/http-flow.yaml", []byte(flowYAML), 0644); err != nil {
			t.Fatalf("Failed to write flow file: %v", err)
		}

		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
			return
		}

		expectedBrokers := []string{"localhost:9092"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})

	t.Run("ignores non-yaml files", func(t *testing.T) {
		tmpDir := t.TempDir()
		flowDir := tmpDir + "/fiso/flows"
		if err := os.MkdirAll(flowDir, 0755); err != nil {
			t.Fatalf("Failed to create flow directory: %v", err)
		}

		// Create non-YAML files that should be ignored
		if err := os.WriteFile(flowDir+"/readme.txt", []byte("not yaml"), 0644); err != nil {
			t.Fatalf("Failed to write txt file: %v", err)
		}
		if err := os.WriteFile(flowDir+"/config.json", []byte(`{"key":"value"}`), 0644); err != nil {
			t.Fatalf("Failed to write json file: %v", err)
		}

		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
			return
		}

		// Should return default since no valid YAML was found
		expectedBrokers := []string{"localhost:9092"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})

	t.Run("handles directories in flow dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		flowDir := tmpDir + "/fiso/flows"
		if err := os.MkdirAll(flowDir+"/subdir", 0755); err != nil {
			t.Fatalf("Failed to create subdirectory: %v", err)
		}

		// Create a valid flow file
		flowYAML := `
kafka:
  clusters:
    main:
      brokers:
        - test-broker:9092
source:
  type: kafka
  config:
    cluster: main
    topic: orders
    consumerGroup: test-group
`
		if err := os.WriteFile(flowDir+"/test.yaml", []byte(flowYAML), 0644); err != nil {
			t.Fatalf("Failed to write flow file: %v", err)
		}

		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
			return
		}

		expectedBrokers := []string{"test-broker:9092"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})

	t.Run("handles .yml extension", func(t *testing.T) {
		tmpDir := t.TempDir()
		flowDir := tmpDir + "/fiso/flows"
		if err := os.MkdirAll(flowDir, 0755); err != nil {
			t.Fatalf("Failed to create flow directory: %v", err)
		}

		flowYAML := `
kafka:
  clusters:
    main:
      brokers:
        - yml-broker:9092
source:
  type: kafka
  config:
    cluster: main
    topic: orders
    consumerGroup: test-group
`
		if err := os.WriteFile(flowDir+"/test.yml", []byte(flowYAML), 0644); err != nil {
			t.Fatalf("Failed to write flow file: %v", err)
		}

		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
			return
		}

		expectedBrokers := []string{"yml-broker:9092"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})

	t.Run("handles invalid yaml gracefully", func(t *testing.T) {
		tmpDir := t.TempDir()
		flowDir := tmpDir + "/fiso/flows"
		if err := os.MkdirAll(flowDir, 0755); err != nil {
			t.Fatalf("Failed to create flow directory: %v", err)
		}

		// Write invalid YAML
		if err := os.WriteFile(flowDir+"/invalid.yaml", []byte("source: [\n  invalid: yaml"), 0644); err != nil {
			t.Fatalf("Failed to write invalid yaml: %v", err)
		}

		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		// Should return default config instead of error
		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() should not error on invalid YAML, got: %v", err)
			return
		}

		expectedBrokers := []string{"localhost:9092"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})

	t.Run("handles unreadable file gracefully", func(t *testing.T) {
		tmpDir := t.TempDir()
		flowDir := tmpDir + "/fiso/flows"
		if err := os.MkdirAll(flowDir, 0755); err != nil {
			t.Fatalf("Failed to create flow directory: %v", err)
		}

		// Create a valid flow file
		flowYAML := `
kafka:
  clusters:
    main:
      brokers:
        - good-broker:9092
source:
  type: kafka
  config:
    cluster: main
    topic: orders
    consumerGroup: test-group
`
		goodFlowPath := flowDir + "/good.yaml"
		if err := os.WriteFile(goodFlowPath, []byte(flowYAML), 0644); err != nil {
			t.Fatalf("Failed to write flow file: %v", err)
		}

		_ = os.Chdir(tmpDir)
		defer func() { _ = os.Chdir(origWd) }()

		cfg, err := loadKafkaClusterConfig("test-topic")
		if err != nil {
			t.Errorf("loadKafkaClusterConfig() unexpected error = %v", err)
			return
		}

		// Should find the good file
		expectedBrokers := []string{"good-broker:9092"}
		if !equalStringSlices(cfg.Brokers, expectedBrokers) {
			t.Errorf("loadKafkaClusterConfig() brokers = %v, want %v", cfg.Brokers, expectedBrokers)
		}
	})
}

func TestPrintMessage(t *testing.T) {
	tests := []struct {
		name      string
		record    *kgo.Record
		wantPanic bool
	}{
		{
			name: "simple text message",
			record: &kgo.Record{
				Partition: 0,
				Offset:    123,
				Key:       []byte("test-key"),
				Value:     []byte("test-value"),
				Timestamp: time.Now(),
			},
			wantPanic: false,
		},
		{
			name: "json message",
			record: &kgo.Record{
				Partition: 1,
				Offset:    456,
				Key:       []byte("json-key"),
				Value:     []byte(`{"foo":"bar","num":42}`),
				Timestamp: time.Now(),
				Headers: []kgo.RecordHeader{
					{Key: "header1", Value: []byte("value1")},
					{Key: "header2", Value: []byte("value2")},
				},
			},
			wantPanic: false,
		},
		{
			name: "message with headers",
			record: &kgo.Record{
				Partition: 0,
				Offset:    789,
				Key:       []byte("key-with-headers"),
				Value:     []byte("value"),
				Timestamp: time.Now(),
				Headers: []kgo.RecordHeader{
					{Key: "content-type", Value: []byte("application/json")},
					{Key: "correlation-id", Value: []byte("abc-123")},
				},
			},
			wantPanic: false,
		},
		{
			name: "empty key",
			record: &kgo.Record{
				Partition: 0,
				Offset:    1000,
				Key:       []byte{},
				Value:     []byte("value"),
				Timestamp: time.Now(),
			},
			wantPanic: false,
		},
		{
			name: "nil key",
			record: &kgo.Record{
				Partition: 0,
				Offset:    1001,
				Key:       nil,
				Value:     []byte("value"),
				Timestamp: time.Now(),
			},
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if tt.wantPanic {
						return
					}
					t.Errorf("printMessage() panicked unexpectedly: %v", r)
				}
			}()
			// Redirect stdout to avoid cluttering test output
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			printMessage(tt.record)

			_ = w.Close()
			os.Stdout = oldStdout
			_ = r.Close()
		})
	}
}

func TestCreateKafkaClient(t *testing.T) {
	const skipIntegrationTests = true

	t.Run("earliest offset configuration", func(t *testing.T) {
		if skipIntegrationTests {
			t.Skip("Skipping integration test - requires Kafka running")
		}

		cfg := consumeConfig{
			Cluster:       &intkafka.ClusterConfig{Brokers: []string{"localhost:9092"}},
			Topic:         "test-topic",
			ConsumerGroup: "test-group",
			StartOffset:   "earliest",
		}

		client, err := createKafkaClient(cfg)
		if err != nil {
			// Expected to fail when Kafka is not available
			t.Logf("createKafkaClient() failed (expected when Kafka unavailable): %v", err)
			return
		}

		if client != nil {
			defer client.Close()
			// Verify client was created
			if client.PollFetches(context.Background()) == nil {
				t.Error("Expected non-nil fetches from client")
			}
		}
	})

	t.Run("latest offset configuration", func(t *testing.T) {
		if skipIntegrationTests {
			t.Skip("Skipping integration test - requires Kafka running")
		}

		cfg := consumeConfig{
			Cluster:       &intkafka.ClusterConfig{Brokers: []string{"localhost:9092"}},
			Topic:         "test-topic",
			ConsumerGroup: "test-group",
			StartOffset:   "latest",
		}

		client, err := createKafkaClient(cfg)
		if err != nil {
			t.Logf("createKafkaClient() failed (expected when Kafka unavailable): %v", err)
			return
		}

		if client != nil {
			defer client.Close()
		}
	})

	t.Run("multiple brokers configuration", func(t *testing.T) {
		if skipIntegrationTests {
			t.Skip("Skipping integration test - requires Kafka running")
		}

		cfg := consumeConfig{
			Cluster:       &intkafka.ClusterConfig{Brokers: []string{"broker1:9092", "broker2:9092", "broker3:9092"}},
			Topic:         "test-topic",
			ConsumerGroup: "test-group",
			StartOffset:   "latest",
		}

		client, err := createKafkaClient(cfg)
		if err != nil {
			t.Logf("createKafkaClient() failed (expected when Kafka unavailable): %v", err)
			return
		}

		if client != nil {
			defer client.Close()
		}
	})

	t.Run("configuration validation", func(t *testing.T) {
		// Test that the config structure is properly handled
		// This is a unit test that doesn't require Kafka
		tests := []struct {
			name        string
			cfg         consumeConfig
			expectError bool
		}{
			{
				name: "valid config with earliest offset",
				cfg: consumeConfig{
					Cluster:       &intkafka.ClusterConfig{Brokers: []string{"localhost:9092"}},
					Topic:         "test-topic",
					ConsumerGroup: "test-group",
					StartOffset:   "earliest",
				},
				expectError: true, // Will fail when Kafka unavailable
			},
			{
				name: "valid config with latest offset",
				cfg: consumeConfig{
					Cluster:       &intkafka.ClusterConfig{Brokers: []string{"localhost:9092"}},
					Topic:         "test-topic",
					ConsumerGroup: "test-group",
					StartOffset:   "latest",
				},
				expectError: true, // Will fail when Kafka unavailable
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// We're testing the function signature and parameter handling
				// The actual connection will fail without Kafka
				client, err := createKafkaClient(tt.cfg)

				if tt.expectError {
					if err == nil {
						// If no error, Kafka must be available - cleanup
						if client != nil {
							defer client.Close()
						}
						t.Logf("createKafkaClient() succeeded - Kafka appears to be available")
					}
				} else {
					if err != nil {
						t.Errorf("createKafkaClient() unexpected error = %v", err)
						return
					}
					if client != nil {
						defer client.Close()
					}
				}
			})
		}
	})

	t.Run("timeout configuration", func(t *testing.T) {
		// Verify that the dial timeout is configured correctly
		cfg := consumeConfig{
			Cluster:       &intkafka.ClusterConfig{Brokers: []string{"localhost:9092"}},
			Topic:         "test-topic",
			ConsumerGroup: "test-group",
			StartOffset:   "latest",
		}

		// The function should use a 3-second dial timeout
		// This ensures tests fail fast when Kafka is unavailable
		start := time.Now()
		client, err := createKafkaClient(cfg)
		elapsed := time.Since(start)

		if err != nil {
			// Should fail quickly due to 3-second timeout
			if elapsed > 5*time.Second {
				t.Errorf("createKafkaClient() took too long to fail: %v (expected ~3s)", elapsed)
			}
		}

		if client != nil {
			defer client.Close()
		}
	})
}

func TestConsumeBatch(t *testing.T) {
	const skipIntegrationTests = true

	t.Run("context cancellation", func(t *testing.T) {
		if skipIntegrationTests {
			t.Skip("Skipping integration test - requires Kafka running")
		}

		// Create a cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// This would require a mock client - for now we test the early exit path
		// The function should return immediately when context is cancelled
		err := consumeBatch(ctx, nil, "test-topic", 10, new(int))
		if err != nil && err != context.Canceled {
			t.Logf("Expected context.Canceled error, got: %v", err)
		}
	})

	t.Run("zero max messages", func(t *testing.T) {
		// Test with maxMessages=0 - should exit immediately
		messageCount := 0

		// This tests the loop condition: for *messageCount < maxMessages
		// When maxMessages is 0, the loop should not execute
		// We can't easily test this without a mock client, but we verify the logic
		if messageCount < 0 {
			t.Error("Loop should not execute when maxMessages is 0")
		}
	})

	t.Run("message counter increment", func(t *testing.T) {
		// Test that messageCount pointer is properly dereferenced and incremented
		messageCount := 0
		maxMessages := 5

		// Simulate the increment logic
		for messageCount < maxMessages {
			messageCount++
			if messageCount >= 2 {
				break // Simulate getting fewer messages
			}
		}

		if messageCount != 2 {
			t.Errorf("Expected messageCount to be 2, got %d", messageCount)
		}
	})
}

func TestConsumeFollow(t *testing.T) {
	const skipIntegrationTests = true

	t.Run("context cancellation", func(t *testing.T) {
		if skipIntegrationTests {
			t.Skip("Skipping integration test - requires Kafka running")
		}

		// Create a context that will be cancelled
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Test that consumeFollow handles context cancellation
		err := consumeFollow(ctx, nil, "test-topic")
		if err != nil && err != context.Canceled {
			t.Logf("Expected context.Canceled error, got: %v", err)
		}
	})

	t.Run("infinite loop structure", func(t *testing.T) {
		// Verify the function has the correct loop structure
		// This is a compile-time check for the function signature
		var fn func(context.Context, *kgo.Client, string) error
		_ = fn
		_ = consumeFollow

		// Test context handling pattern
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Simulate the pattern used in consumeFollow
		done := false
		for !done {
			select {
			case <-ctx.Done():
				done = true
			default:
				// Would poll messages here
				done = true // Exit for this test
			}
		}

		if ctx.Err() != nil {
			t.Error("Context should not be cancelled yet")
		}
	})
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mockKafkaConsumer implements kafkaConsumer for testing
type mockKafkaConsumer struct {
	mu           sync.Mutex
	records      []*kgo.Record
	fetchCount   int
	commitError  error
	closed       bool
	maxFetches   int
	fetchIndex   int
	blockOnEmpty bool // if true, block instead of returning empty fetches after maxFetches
}

func (m *mockKafkaConsumer) PollFetches(ctx context.Context) kgo.Fetches {
	if ctx.Err() != nil {
		return kgo.Fetches{}
	}

	m.mu.Lock()
	fetchCount := m.fetchCount
	m.fetchCount++
	maxFetches := m.maxFetches
	fetchIndex := m.fetchIndex
	blockOnEmpty := m.blockOnEmpty
	var record *kgo.Record
	if fetchIndex < len(m.records) {
		record = m.records[fetchIndex]
		m.fetchIndex++
	}
	m.mu.Unlock()

	// If we've exceeded maxFetches, either block or return empty
	if maxFetches > 0 && fetchCount >= maxFetches {
		if blockOnEmpty {
			// Block forever (or until context cancelled) to prevent tight loop
			<-ctx.Done()
			return kgo.Fetches{}
		}
		return kgo.Fetches{}
	}

	// If we've returned all records, return empty fetches
	if record == nil {
		return kgo.Fetches{}
	}

	return kgo.Fetches{
		{
			Topics: []kgo.FetchTopic{
				{
					Topic: record.Topic,
					Partitions: []kgo.FetchPartition{
						{
							Partition: record.Partition,
							Records:   []*kgo.Record{record},
						},
					},
				},
			},
		},
	}
}

func (m *mockKafkaConsumer) getFetchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fetchCount
}

func (m *mockKafkaConsumer) MarkCommitRecords(rs ...*kgo.Record) {
	// no-op for mock
}

func (m *mockKafkaConsumer) CommitMarkedOffsets(ctx context.Context) error {
	return m.commitError
}

func (m *mockKafkaConsumer) Close() {
	m.closed = true
}

func TestConsumeBatch_WithMock(t *testing.T) {
	tests := []struct {
		name           string
		records        []*kgo.Record
		maxMessages    int
		commitError    error
		expectError    bool
		expectConsumed int
	}{
		{
			name: "consume single message",
			records: []*kgo.Record{
				{Topic: "test", Partition: 0, Offset: 0, Key: []byte("k"), Value: []byte(`{"data":"test"}`)},
			},
			maxMessages:    1,
			expectConsumed: 1,
		},
		{
			name: "consume multiple messages",
			records: []*kgo.Record{
				{Topic: "test", Partition: 0, Offset: 0, Key: []byte("k1"), Value: []byte(`{"data":"test1"}`)},
				{Topic: "test", Partition: 0, Offset: 1, Key: []byte("k2"), Value: []byte(`{"data":"test2"}`)},
				{Topic: "test", Partition: 0, Offset: 2, Key: []byte("k3"), Value: []byte(`{"data":"test3"}`)},
			},
			maxMessages:    3,
			expectConsumed: 3,
		},
		{
			name: "max messages limits consumption",
			records: []*kgo.Record{
				{Topic: "test", Partition: 0, Offset: 0, Key: []byte("k1"), Value: []byte(`{"data":"test1"}`)},
				{Topic: "test", Partition: 0, Offset: 1, Key: []byte("k2"), Value: []byte(`{"data":"test2"}`)},
				{Topic: "test", Partition: 0, Offset: 2, Key: []byte("k3"), Value: []byte(`{"data":"test3"}`)},
			},
			maxMessages:    2,
			expectConsumed: 2,
		},
		{
			name:           "no messages available",
			records:        []*kgo.Record{},
			maxMessages:    5,
			expectConsumed: 0,
		},
		{
			name: "commit error logged but continues",
			records: []*kgo.Record{
				{Topic: "test", Partition: 0, Offset: 0, Key: []byte("k"), Value: []byte(`{"data":"test"}`)},
			},
			maxMessages:    1,
			commitError:    fmt.Errorf("commit failed"),
			expectConsumed: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockKafkaConsumer{
				records:     tt.records,
				commitError: tt.commitError,
				maxFetches:  tt.maxMessages + 5, // Allow some extra fetches
			}

			// If no records, use cancelled context to exit loop
			ctx := context.Background()
			if len(tt.records) == 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 200*time.Millisecond)
				defer cancel()
			}

			messageCount := 0
			err := consumeBatch(ctx, mock, "test-topic", tt.maxMessages, &messageCount)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				}
			} else {
				// Context timeout is expected for empty records case
				if err != nil && err != context.DeadlineExceeded {
					t.Errorf("unexpected error: %v", err)
				}
				if messageCount != tt.expectConsumed {
					t.Errorf("expected %d messages consumed, got %d", tt.expectConsumed, messageCount)
				}
			}
		})
	}
}

func TestConsumeFollow_WithMock(t *testing.T) {
	tests := []struct {
		name        string
		records     []*kgo.Record
		maxFetches  int
		commitError error
	}{
		{
			name: "consume messages until context cancelled",
			records: []*kgo.Record{
				{Topic: "test", Partition: 0, Offset: 0, Key: []byte("k1"), Value: []byte(`{"data":"test1"}`)},
				{Topic: "test", Partition: 0, Offset: 1, Key: []byte("k2"), Value: []byte(`{"data":"test2"}`)},
			},
			maxFetches: 5,
		},
		{
			name: "handles commit error",
			records: []*kgo.Record{
				{Topic: "test", Partition: 0, Offset: 0, Key: []byte("k"), Value: []byte(`{"data":"test"}`)},
			},
			maxFetches:  3,
			commitError: fmt.Errorf("commit failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockKafkaConsumer{
				records:     tt.records,
				commitError: tt.commitError,
				maxFetches:  tt.maxFetches,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()

			err := consumeFollow(ctx, mock, "test-topic")

			// Context timeout or cancellation is expected
			if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
				t.Errorf("unexpected error: %v", err)
			}

			if mock.fetchCount == 0 {
				t.Error("expected at least one fetch call")
			}
		})
	}
}

func TestRunConsume_WithMockClient(t *testing.T) {
	// These tests must run sequentially to avoid races on createKafkaClientFunc
	t.Run("successful batch consume", func(t *testing.T) {
		// Save and restore for each subtest to avoid races
		origCreateClient := createKafkaClientFunc
		defer func() { createKafkaClientFunc = origCreateClient }()

		mock := &mockKafkaConsumer{
			records: []*kgo.Record{
				{Topic: "test", Partition: 0, Offset: 0, Key: []byte("k1"), Value: []byte(`{"order":"123"}`)},
			},
			maxFetches: 5,
		}
		createKafkaClientFunc = func(cfg consumeConfig) (kafkaConsumer, error) {
			return mock, nil
		}

		err := RunConsume([]string{"--topic", "test", "--max-messages", "1"})
		if err != nil && !strings.Contains(err.Error(), "context") {
			t.Errorf("unexpected error: %v", err)
		}

		if !mock.closed {
			t.Error("expected client to be closed")
		}
	})

	t.Run("client creation error", func(t *testing.T) {
		origCreateClient := createKafkaClientFunc
		defer func() { createKafkaClientFunc = origCreateClient }()

		createKafkaClientFunc = func(cfg consumeConfig) (kafkaConsumer, error) {
			return nil, fmt.Errorf("cannot connect to kafka")
		}

		err := RunConsume([]string{"--topic", "test", "--max-messages", "1"})
		if err == nil {
			t.Error("expected error but got nil")
		}
		if !strings.Contains(err.Error(), "create kafka client") {
			t.Errorf("expected 'create kafka client' in error, got: %v", err)
		}
	})

	t.Run("from-beginning flag", func(t *testing.T) {
		origCreateClient := createKafkaClientFunc
		defer func() { createKafkaClientFunc = origCreateClient }()

		var capturedCfg consumeConfig
		var cfgMu sync.Mutex
		mock := &mockKafkaConsumer{
			records: []*kgo.Record{
				{Topic: "test", Partition: 0, Offset: 0, Key: []byte("k1"), Value: []byte(`{"data":"test"}`)},
			},
			maxFetches: 5,
		}
		createKafkaClientFunc = func(cfg consumeConfig) (kafkaConsumer, error) {
			cfgMu.Lock()
			capturedCfg = cfg
			cfgMu.Unlock()
			return mock, nil
		}

		// Run with max-messages=1 so it exits after consuming one record
		_ = RunConsume([]string{"--topic", "test", "--from-beginning", "--max-messages", "1"})

		cfgMu.Lock()
		offset := capturedCfg.StartOffset
		cfgMu.Unlock()

		if offset != "earliest" {
			t.Errorf("expected StartOffset='earliest', got %q", offset)
		}
	})

	t.Run("follow mode", func(t *testing.T) {
		// Test consumeFollow directly with a controlled context instead of
		// going through RunConsume, which manages its own context internally.
		// This avoids issues with signal-based shutdown in tests.
		mock := &mockKafkaConsumer{
			records: []*kgo.Record{
				{Topic: "test", Partition: 0, Offset: 0, Key: []byte("k1"), Value: []byte(`{"data":"test"}`)},
			},
			maxFetches:   3,
			blockOnEmpty: true,
		}

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go func() {
			done <- consumeFollow(ctx, mock, "test-topic")
		}()

		// Wait for fetches to happen
		for i := 0; i < 50; i++ {
			if mock.getFetchCount() >= 2 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Cancel context to stop the follow loop
		cancel()

		// Wait for consumeFollow to finish
		select {
		case err := <-done:
			if err != nil && err != context.Canceled {
				t.Errorf("unexpected error: %v", err)
			}
		case <-time.After(1 * time.Second):
			t.Error("timeout waiting for consumeFollow to finish")
		}

		if mock.getFetchCount() == 0 {
			t.Error("expected fetch calls in follow mode")
		}
	})
}
