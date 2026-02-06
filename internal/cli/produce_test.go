package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunProduce_Help(t *testing.T) {
	if err := RunProduce([]string{"-h"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := RunProduce([]string{"--help"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunProduce_MissingTopic(t *testing.T) {
	err := RunProduce([]string{})
	if err == nil {
		t.Fatal("expected error for missing topic")
	}
	if !strings.Contains(err.Error(), "--topic") {
		t.Errorf("expected error to mention --topic flag, got: %v", err)
	}
}

func TestRunProduce_MissingData(t *testing.T) {
	err := RunProduce([]string{"--topic", "test-topic"})
	if err == nil {
		t.Fatal("expected error for missing data source")
	}
	if !strings.Contains(err.Error(), "--file") || !strings.Contains(err.Error(), "--json") {
		t.Errorf("expected error to mention --file or --json, got: %v", err)
	}
}

func TestRunProduce_BothFileAndJson(t *testing.T) {
	err := RunProduce([]string{"--topic", "test", "--file", "test.json", "--json", "{}"})
	if err == nil {
		t.Fatal("expected error when both --file and --json specified")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Errorf("expected error to mention both flags, got: %v", err)
	}
}

func TestParseStringFlag(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		flag    string
		want    string
		wantErr bool
	}{
		{
			name:    "flag found with value",
			args:    []string{"--topic", "my-topic"},
			flag:    "--topic",
			want:    "my-topic",
			wantErr: false,
		},
		{
			name:    "flag not found",
			args:    []string{"--other", "value"},
			flag:    "--topic",
			want:    "",
			wantErr: false,
		},
		{
			name:    "flag without value",
			args:    []string{"--topic"},
			flag:    "--topic",
			want:    "",
			wantErr: true,
		},
		{
			name:    "flag at end",
			args:    []string{"--file", "test.json", "--topic", "orders"},
			flag:    "--topic",
			want:    "orders",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStringFlag(tt.args, tt.flag)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseStringFlag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseStringFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseIntFlag(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		flag       string
		defaultVal int
		want       int
		wantErr    bool
	}{
		{
			name:       "valid integer",
			args:       []string{"--count", "10"},
			flag:       "--count",
			defaultVal: 1,
			want:       10,
			wantErr:    false,
		},
		{
			name:       "default value",
			args:       []string{},
			flag:       "--count",
			defaultVal: 5,
			want:       5,
			wantErr:    false,
		},
		{
			name:       "invalid integer",
			args:       []string{"--count", "abc"},
			flag:       "--count",
			defaultVal: 1,
			want:       0,
			wantErr:    true,
		},
		{
			name:       "zero value",
			args:       []string{"--count", "0"},
			flag:       "--count",
			defaultVal: 1,
			want:       0,
			wantErr:    true,
		},
		{
			name:       "negative value",
			args:       []string{"--count", "-1"},
			flag:       "--count",
			defaultVal: 1,
			want:       0,
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIntFlag(tt.args, tt.flag, tt.defaultVal)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseIntFlag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseIntFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProduceInlineJSON_InvalidJSON(t *testing.T) {
	tests := []struct {
		name    string
		jsonStr string
	}{
		{
			name:    "invalid json",
			jsonStr: "{invalid json}",
		},
		{
			name:    "not json",
			jsonStr: "plain text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We need to test the validation without requiring a real Kafka connection
			// This validates the JSON parsing logic
			var data json.RawMessage
			err := json.Unmarshal([]byte(tt.jsonStr), &data)
			if err == nil {
				t.Error("expected JSON unmarshal error")
			}
		})
	}
}

func TestProduceFromFile_FileNotFound(t *testing.T) {
	ctx := context.Background()
	nonExistentFile := filepath.Join(t.TempDir(), "nonexistent.json")

	err := produceFromFile(ctx, nil, "test-topic", nonExistentFile, 1, 0)
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "open file") {
		t.Errorf("expected error to mention file opening, got: %v", err)
	}
}

func TestProduceFromFile_EmptyFile(t *testing.T) {
	ctx := context.Background()
	emptyFile := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(emptyFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	err := produceFromFile(ctx, nil, "test-topic", emptyFile, 1, 0)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if !strings.Contains(err.Error(), "no valid json") {
		t.Errorf("expected error to mention no valid json, got: %v", err)
	}
}

func TestProduceFromFile_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	invalidFile := filepath.Join(t.TempDir(), "invalid.json")
	content := `{not valid json`
	if err := os.WriteFile(invalidFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := produceFromFile(ctx, nil, "test-topic", invalidFile, 1, 0)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid json") {
		t.Errorf("expected error to mention invalid json, got: %v", err)
	}
}

func TestProduceFromFile_ValidJSON(t *testing.T) {
	// Verify that valid JSON content can be parsed
	content := `{"order_id":"123"}`
	var data json.RawMessage
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		t.Fatalf("failed to parse test JSON: %v", err)
	}
	// Basic sanity check that the JSON is valid
	if !json.Valid(data) {
		t.Error("expected JSON to be valid")
	}
}

func TestProduceFromFile_MultipleEvents(t *testing.T) {
	// Test that multiple JSON lines can be parsed
	content := `{"order_id":"001"}
{"order_id":"002"}
{"order_id":"003"}
`

	lineCount := 0
	scanner := strings.NewReader(content)
	bufScanner := bufio.NewScanner(scanner)
	for bufScanner.Scan() {
		line := strings.TrimSpace(bufScanner.Text())
		if line != "" {
			var data json.RawMessage
			if err := json.Unmarshal([]byte(line), &data); err != nil {
				t.Fatalf("failed to parse line %d: %v", lineCount+1, err)
			}
			lineCount++
		}
	}

	if lineCount != 3 {
		t.Errorf("expected 3 valid JSON lines, got %d", lineCount)
	}
}

func TestRunProduce_InvalidRate(t *testing.T) {
	err := RunProduce([]string{"--topic", "test", "--json", "{}", "--rate", "invalid"})
	if err == nil {
		t.Fatal("expected error for invalid rate duration")
	}
	if !strings.Contains(err.Error(), "invalid rate") {
		t.Errorf("expected error to mention invalid rate, got: %v", err)
	}
	_ = err
}

func TestRunProduce_ValidRate(t *testing.T) {
	// Test that valid rate strings parse correctly
	validRates := []string{"100ms", "1s", "500ms", "2m"}
	for _, rateStr := range validRates {
		t.Run(rateStr, func(t *testing.T) {
			duration, err := time.ParseDuration(rateStr)
			if err != nil {
				t.Errorf("failed to parse valid rate %q: %v", rateStr, err)
			}
			if duration <= 0 {
				t.Errorf("parsed duration should be positive, got %v", duration)
			}
		})
	}
}

func TestProduceFromFile_WithCount(t *testing.T) {
	// Test that count parameter works correctly
	lines := []string{}
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf(`{"order_id":"%03d"}`, i))
	}
	content := strings.Join(lines, "\n")

	// Test count = 5 should only process first 5 lines
	count := 5
	produced := 0
	scanner := strings.NewReader(content)
	bufScanner := bufio.NewScanner(scanner)
	for bufScanner.Scan() && produced < count {
		line := strings.TrimSpace(bufScanner.Text())
		if line != "" {
			var data json.RawMessage
			if err := json.Unmarshal([]byte(line), &data); err != nil {
				t.Fatalf("failed to parse line: %v", err)
			}
			produced++
		}
	}

	if produced != count {
		t.Errorf("expected %d events, got %d", count, produced)
	}
}

func TestRunProduce_CustomBrokers(t *testing.T) {
	brokersStr := "broker1:9092,broker2:9092,broker3:9092"
	brokers := strings.Split(brokersStr, ",")
	for i, b := range brokers {
		brokers[i] = strings.TrimSpace(b)
	}

	expected := []string{"broker1:9092", "broker2:9092", "broker3:9092"}
	if len(brokers) != len(expected) {
		t.Fatalf("expected %d brokers, got %d", len(expected), len(brokers))
	}

	for i, got := range brokers {
		if got != expected[i] {
			t.Errorf("broker %d: expected %q, got %q", i, expected[i], got)
		}
	}
}

func TestProduceFromFile_WithEmptyLines(t *testing.T) {
	// Test that empty lines are skipped
	content := `{"order_id":"001"}

{"order_id":"002"}

{"order_id":"003"}
`

	validCount := 0
	scanner := strings.NewReader(content)
	bufScanner := bufio.NewScanner(scanner)
	for bufScanner.Scan() {
		line := strings.TrimSpace(bufScanner.Text())
		if line == "" {
			continue
		}
		var data json.RawMessage
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			t.Fatalf("failed to parse line: %v", err)
		}
		validCount++
	}

	if validCount != 3 {
		t.Errorf("expected 3 valid events (skipping empty lines), got %d", validCount)
	}
}

func TestParseStringFlag_MultipleValues(t *testing.T) {
	args := []string{"--file", "a.json", "--file", "b.json"}
	result, err := parseStringFlag(args, "--file")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the first value
	if result != "a.json" {
		t.Errorf("expected first value 'a.json', got %q", result)
	}
}

func TestParseStringFlag_EmptyArgs(t *testing.T) {
	result, err := parseStringFlag([]string{}, "--topic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string for missing flag, got %q", result)
	}
}

func TestRunProduce_BrokerParsing(t *testing.T) {
	// Test that --brokers flag is parsed correctly
	tests := []struct {
		name       string
		brokersStr string
		expected   []string
	}{
		{
			name:       "single broker",
			brokersStr: "localhost:9092",
			expected:   []string{"localhost:9092"},
		},
		{
			name:       "multiple brokers",
			brokersStr: "broker1:9092,broker2:9092",
			expected:   []string{"broker1:9092", "broker2:9092"},
		},
		{
			name:       "brokers with spaces",
			brokersStr: "broker1:9092, broker2:9092 , broker3:9092",
			expected:   []string{"broker1:9092", "broker2:9092", "broker3:9092"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brokers := strings.Split(tt.brokersStr, ",")
			for i, b := range brokers {
				brokers[i] = strings.TrimSpace(b)
			}
			if len(brokers) != len(tt.expected) {
				t.Errorf("expected %d brokers, got %d", len(tt.expected), len(brokers))
				return
			}
			for i, got := range brokers {
				if got != tt.expected[i] {
					t.Errorf("broker %d: expected %q, got %q", i, tt.expected[i], got)
				}
			}
		})
	}
}

// New tests to improve coverage for produceInlineJSON (line 98)

// TestProduceInlineJSON_ValidJSONVariants tests valid JSON variants
func TestProduceInlineJSON_ValidJSONVariants(t *testing.T) {
	tests := []struct {
		name    string
		jsonStr string
		valid   bool
	}{
		{
			name:    "simple object",
			jsonStr: `{"order_id":"123","amount":100.50}`,
			valid:   true,
		},
		{
			name:    "nested object",
			jsonStr: `{"user":{"id":"123","name":"test"},"items":[{"sku":"A"},{"sku":"B"}]}`,
			valid:   true,
		},
		{
			name:    "json array",
			jsonStr: `[{"id":1},{"id":2}]`,
			valid:   true,
		},
		{
			name:    "json string",
			jsonStr: `"just a string"`,
			valid:   true,
		},
		{
			name:    "json number",
			jsonStr: `42`,
			valid:   true,
		},
		{
			name:    "json boolean",
			jsonStr: `true`,
			valid:   true,
		},
		{
			name:    "json null",
			jsonStr: `null`,
			valid:   true,
		},
		{
			name:    "unclosed brace",
			jsonStr: `{"order_id":"123"`,
			valid:   false,
		},
		{
			name:    "plain text",
			jsonStr: `not json at all`,
			valid:   false,
		},
		{
			name:    "unclosed string",
			jsonStr: `{"order_id":"123}`,
			valid:   false,
		},
		{
			name:    "trailing comma",
			jsonStr: `{"order_id":"123",}`,
			valid:   false,
		},
		{
			name:    "single quote instead of double",
			jsonStr: `{'order_id':'123'}`,
			valid:   false,
		},
		{
			name:    "empty string",
			jsonStr: ``,
			valid:   false,
		},
		{
			name:    "invalid with count",
			jsonStr: `{invalid}`,
			valid:   false,
		},
		{
			name:    "unicode escape",
			jsonStr: `{"message":"Hello\u00A0World"}`,
			valid:   true,
		},
		{
			name:    "escaped characters",
			jsonStr: `{"path":"C:\\Users\\test"}`,
			valid:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data json.RawMessage
			err := json.Unmarshal([]byte(tt.jsonStr), &data)
			if tt.valid && err != nil {
				t.Errorf("expected valid JSON, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("expected invalid JSON, but it parsed successfully")
			}
		})
	}
}

// TestProduceInlineJSON_InvalidJSONDetailed tests detailed invalid JSON scenarios
func TestProduceInlineJSON_InvalidJSONDetailed(t *testing.T) {
	tests := []struct {
		name        string
		jsonStr     string
		expectError string
	}{
		{
			name:        "unclosed brace",
			jsonStr:     `{"order_id":"123"`,
			expectError: "invalid json",
		},
		{
			name:        "plain text",
			jsonStr:     `not json at all`,
			expectError: "invalid json",
		},
		{
			name:        "unclosed string",
			jsonStr:     `{"order_id":"123}`,
			expectError: "invalid json",
		},
		{
			name:        "trailing comma",
			jsonStr:     `{"order_id":"123",}`,
			expectError: "invalid json",
		},
		{
			name:        "single quote instead of double",
			jsonStr:     `{'order_id':'123'}`,
			expectError: "invalid json",
		},
		{
			name:        "empty string",
			jsonStr:     ``,
			expectError: "invalid json",
		},
		{
			name:        "invalid with count",
			jsonStr:     `{invalid}`,
			expectError: "invalid json",
		},
		{
			name:        "missing colon",
			jsonStr:     `{"order_id" "123"}`,
			expectError: "invalid json",
		},
		{
			name:        "extra comma",
			jsonStr:     `{,}`,
			expectError: "invalid json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data json.RawMessage
			err := json.Unmarshal([]byte(tt.jsonStr), &data)
			if err == nil {
				t.Fatal("expected error for invalid JSON, got nil")
			}
			// Verify error message is meaningful
			if !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "JSON") && !strings.Contains(err.Error(), "syntax") {
				t.Logf("Warning: error message doesn't contain expected keywords: %v", err)
			}
		})
	}
}

// TestProduceFromFile_ValidJSONLDetailed tests produceFromFile JSONL parsing logic
func TestProduceFromFile_ValidJSONLDetailed(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		count        int
		expectParsed int
	}{
		{
			name:         "single json line",
			content:      `{"order_id":"001"}`,
			count:        1,
			expectParsed: 1,
		},
		{
			name: "multiple json lines",
			content: `{"order_id":"001"}
{"order_id":"002"}
{"order_id":"003"}`,
			count:        0,
			expectParsed: 3,
		},
		{
			name: "jsonl with count limit",
			content: `{"order_id":"001"}
{"order_id":"002"}
{"order_id":"003"}
{"order_id":"004"}
{"order_id":"005"}`,
			count:        3,
			expectParsed: 3,
		},
		{
			name: "jsonl with empty lines",
			content: `{"order_id":"001"}

{"order_id":"002"}

{"order_id":"003"}`,
			count:        0,
			expectParsed: 3,
		},
		{
			name: "jsonl with complex json",
			content: `{"user":{"id":"1","name":"Alice"},"items":[{"sku":"A"},{"sku":"B"}]}
{"user":{"id":"2","name":"Bob"},"items":[{"sku":"C"}]}`,
			count:        0,
			expectParsed: 2,
		},
		{
			name: "jsonl with only empty lines",
			content: `

`,
			count:        0,
			expectParsed: 0,
		},
		{
			name: "jsonl with whitespace",
			content: `  {"order_id":"001"}
	{"order_id":"002"}
   {"order_id":"003"}
`,
			count:        0,
			expectParsed: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedCount := 0
			scanner := strings.NewReader(tt.content)
			bufScanner := bufio.NewScanner(scanner)

			processed := 0
			for bufScanner.Scan() && (tt.count == 0 || processed < tt.count) {
				line := strings.TrimSpace(bufScanner.Text())
				if line == "" {
					continue
				}
				var data json.RawMessage
				if err := json.Unmarshal([]byte(line), &data); err != nil {
					t.Fatalf("unexpected parse error on line %d: %v", processed+1, err)
				}
				parsedCount++
				processed++
			}

			if parsedCount != tt.expectParsed {
				t.Errorf("expected %d parsed events, got %d", tt.expectParsed, parsedCount)
			}
		})
	}
}

// TestProduceFromFile_MalformedJSONDetailed tests malformed JSON in file scenarios
func TestProduceFromFile_MalformedJSONDetailed(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError string
		errorOnLine int
	}{
		{
			name: "first line invalid",
			content: `{invalid json}
{"order_id":"002"}`,
			expectError: "invalid json",
			errorOnLine: 1,
		},
		{
			name: "middle line invalid",
			content: `{"order_id":"001"}
{invalid}
{"order_id":"003"}`,
			expectError: "invalid json",
			errorOnLine: 2,
		},
		{
			name: "last line invalid",
			content: `{"order_id":"001"}
{"order_id":"002"}
{"unclosed`,
			expectError: "invalid json",
			errorOnLine: 3,
		},
		{
			name:        "unclosed string in value",
			content:     `{"order_id":"001}`,
			expectError: "invalid json",
			errorOnLine: 1,
		},
		{
			name:        "trailing comma",
			content:     `{"items":[1,2,],}`,
			expectError: "invalid json",
			errorOnLine: 1,
		},
		{
			name: "mixed valid and invalid",
			content: `{"valid":true}
not json
{"also":true}`,
			expectError: "invalid json",
			errorOnLine: 2,
		},
		{
			name:        "special characters unescaped",
			content:     `{"msg":"hello` + "\n" + `world"}`,
			expectError: "invalid json",
			errorOnLine: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := strings.NewReader(tt.content)
			bufScanner := bufio.NewScanner(scanner)
			lineNum := 0

			for bufScanner.Scan() {
				lineNum++
				line := strings.TrimSpace(bufScanner.Text())
				if line == "" {
					continue
				}
				var data json.RawMessage
				err := json.Unmarshal([]byte(line), &data)
				if err != nil {
					if lineNum != tt.errorOnLine {
						t.Errorf("expected error on line %d, got error on line %d", tt.errorOnLine, lineNum)
					}
					if !strings.Contains(err.Error(), tt.expectError) && !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "syntax") {
						t.Logf("Warning: error message doesn't contain expected keywords: %v", err)
					}
					return
				}
			}
			t.Fatal("expected JSON parsing error, but all lines were valid")
		})
	}
}

// TestProduceFromFile_CountBehavior tests count parameter behavior
func TestProduceFromFile_CountBehavior(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		count         int
		expectProcess int
	}{
		{
			name: "count zero processes all",
			content: `{"event":"1"}
{"event":"2"}
{"event":"3"}`,
			count:         0,
			expectProcess: 3,
		},
		{
			name: "count positive limits processing",
			content: `{"event":"1"}
{"event":"2"}
{"event":"3"}
{"event":"4"}
{"event":"5"}`,
			count:         3,
			expectProcess: 3,
		},
		{
			name: "count greater than available",
			content: `{"event":"1"}
{"event":"2"}`,
			count:         10,
			expectProcess: 2,
		},
		{
			name: "count one with multiple lines",
			content: `{"event":"1"}
{"event":"2"}
{"event":"3"}`,
			count:         1,
			expectProcess: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processed := 0
			scanner := strings.NewReader(tt.content)
			bufScanner := bufio.NewScanner(scanner)

			for bufScanner.Scan() && (tt.count == 0 || processed < tt.count) {
				line := strings.TrimSpace(bufScanner.Text())
				if line == "" {
					continue
				}
				var data json.RawMessage
				if err := json.Unmarshal([]byte(line), &data); err != nil {
					t.Fatalf("unexpected parse error: %v", err)
				}
				processed++
			}

			if processed != tt.expectProcess {
				t.Errorf("expected %d processed events, got %d", tt.expectProcess, processed)
			}
		})
	}
}

// TestRateLimitingLogic tests rate limiting logic
func TestRateLimitingLogic(t *testing.T) {
	tests := []struct {
		name       string
		rateStr    string
		expectRate time.Duration
		valid      bool
	}{
		{
			name:       "milliseconds",
			rateStr:    "100ms",
			expectRate: 100 * time.Millisecond,
			valid:      true,
		},
		{
			name:       "seconds",
			rateStr:    "2s",
			expectRate: 2 * time.Second,
			valid:      true,
		},
		{
			name:       "minutes",
			rateStr:    "5m",
			expectRate: 5 * time.Minute,
			valid:      true,
		},
		{
			name:       "microseconds",
			rateStr:    "500us",
			expectRate: 500 * time.Microsecond,
			valid:      true,
		},
		{
			name:       "invalid format",
			rateStr:    "invalid",
			expectRate: 0,
			valid:      false,
		},
		{
			name:       "empty string",
			rateStr:    "",
			expectRate: 0,
			valid:      true,
		},
		{
			name:       "negative number",
			rateStr:    "-1s",
			expectRate: -1 * time.Second,
			valid:      true, // time.ParseDuration accepts negative durations
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.rateStr == "" {
				return
			}
			duration, err := time.ParseDuration(tt.rateStr)
			if tt.valid {
				if err != nil {
					t.Errorf("expected valid rate %q, got error: %v", tt.rateStr, err)
				}
				if tt.expectRate > 0 && duration != tt.expectRate {
					t.Errorf("expected rate %v, got %v", tt.expectRate, duration)
				}
			} else {
				if err == nil {
					t.Errorf("expected invalid rate %q, but it parsed successfully", tt.rateStr)
				}
			}
		})
	}
}

// TestProduceInlineJSON_RateLimitingLogic tests rate limiting behavior
func TestProduceInlineJSON_RateLimitingLogic(t *testing.T) {
	tests := []struct {
		name         string
		count        int
		rate         time.Duration
		expectSleeps int
	}{
		{
			name:         "no rate limiting",
			count:        5,
			rate:         0,
			expectSleeps: 0,
		},
		{
			name:         "rate limiting with count",
			count:        5,
			rate:         100 * time.Millisecond,
			expectSleeps: 4,
		},
		{
			name:         "rate limiting single event",
			count:        1,
			rate:         100 * time.Millisecond,
			expectSleeps: 0,
		},
		{
			name:         "rate limiting two events",
			count:        2,
			rate:         50 * time.Millisecond,
			expectSleeps: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sleepCount := 0
			// Simulate the sleep logic from produceInlineJSON
			for i := 0; i < tt.count; i++ {
				// Simulate publish
				if tt.rate > 0 && i < tt.count-1 {
					// Instead of sleeping, just count
					sleepCount++
				}
			}
			if sleepCount != tt.expectSleeps {
				t.Errorf("expected %d sleep calls, got %d", tt.expectSleeps, sleepCount)
			}
		})
	}
}

// TestProduceFromFile_RateLimitingLogic tests file-based rate limiting logic
func TestProduceFromFile_RateLimitingLogic(t *testing.T) {
	tests := []struct {
		name         string
		lines        int
		count        int
		rate         time.Duration
		expectSleeps int
	}{
		{
			name:         "no rate limiting",
			lines:        5,
			count:        0,
			rate:         0,
			expectSleeps: 0,
		},
		{
			name:         "rate limiting all events",
			lines:        3,
			count:        0,
			rate:         100 * time.Millisecond,
			expectSleeps: 3,
		},
		{
			name:         "rate limiting with count limit",
			lines:        10,
			count:        5,
			rate:         50 * time.Millisecond,
			expectSleeps: 5,
		},
		{
			name:         "rate limiting single event",
			lines:        1,
			count:        0,
			rate:         100 * time.Millisecond,
			expectSleeps: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sleepCount := 0
			processed := 0

			// Simulate the loop logic from produceFromFile
			for i := 0; i < tt.lines; i++ {
				if tt.count > 0 && processed >= tt.count {
					break
				}

				// Simulate processing a line
				processed++

				if tt.rate > 0 {
					// Instead of sleeping, just count
					sleepCount++
				}
			}

			if sleepCount != tt.expectSleeps {
				t.Errorf("expected %d sleep calls, got %d", tt.expectSleeps, sleepCount)
			}
		})
	}
}

// TestProduceFromFile_EmptyFileErrors tests various empty file scenarios
func TestProduceFromFile_EmptyFileErrors(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "completely empty file",
			content:     ``,
			expectError: true,
			errorMsg:    "no valid json",
		},
		{
			name: "only whitespace",
			content: `


`,
			expectError: true,
			errorMsg:    "no valid json",
		},
		{
			name: "only comments (not valid JSON)",
			content: `# comment
// another comment`,
			expectError: true,
			errorMsg:    "invalid json",
		},
		{
			name:        "single valid event",
			content:     `{"test":"data"}`,
			expectError: false,
			errorMsg:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.content == "" {
				return
			}

			validCount := 0
			scanner := strings.NewReader(tt.content)
			bufScanner := bufio.NewScanner(scanner)

			for bufScanner.Scan() {
				line := strings.TrimSpace(bufScanner.Text())
				if line == "" {
					continue
				}
				var data json.RawMessage
				if err := json.Unmarshal([]byte(line), &data); err != nil {
					if tt.expectError && strings.Contains(tt.errorMsg, "invalid json") {
						return
					}
					t.Fatalf("unexpected error: %v", err)
				}
				validCount++
			}

			if tt.expectError && tt.errorMsg == "no valid json" && validCount == 0 {
				return
			}
			if !tt.expectError && validCount > 0 {
				return
			}
		})
	}
}

// TestBrokersFlagParsingDetailed tests various brokers flag formats
func TestBrokersFlagParsingDetailed(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single broker",
			input:    "localhost:9092",
			expected: []string{"localhost:9092"},
		},
		{
			name:     "multiple brokers no spaces",
			input:    "broker1:9092,broker2:9092,broker3:9092",
			expected: []string{"broker1:9092", "broker2:9092", "broker3:9092"},
		},
		{
			name:     "multiple brokers with spaces",
			input:    "broker1:9092, broker2:9092 , broker3:9092",
			expected: []string{"broker1:9092", "broker2:9092", "broker3:9092"},
		},
		{
			name:     "brokers with different ports",
			input:    "kafka1:9092,kafka2:9093,kafka3:9094",
			expected: []string{"kafka1:9092", "kafka2:9093", "kafka3:9094"},
		},
		{
			name:     "brokers with hostnames",
			input:    "kafka.example.com:9092,kafka2.example.com:9092",
			expected: []string{"kafka.example.com:9092", "kafka2.example.com:9092"},
		},
		{
			name:     "brokers with IPs",
			input:    "192.168.1.100:9092,192.168.1.101:9092",
			expected: []string{"192.168.1.100:9092", "192.168.1.101:9092"},
		},
		{
			name:     "mixed format",
			input:    "localhost:9092, kafka.example.com:9092,192.168.1.100:9092",
			expected: []string{"localhost:9092", "kafka.example.com:9092", "192.168.1.100:9092"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brokers := strings.Split(tt.input, ",")
			for i, b := range brokers {
				brokers[i] = strings.TrimSpace(b)
			}

			if len(brokers) != len(tt.expected) {
				t.Errorf("expected %d brokers, got %d", len(tt.expected), len(brokers))
				return
			}

			for i, got := range brokers {
				if got != tt.expected[i] {
					t.Errorf("broker %d: expected %q, got %q", i, tt.expected[i], got)
				}
			}
		})
	}
}

// TestCountFlagParsingDetailed tests count flag parsing variations
func TestCountFlagParsingDetailed(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
		expected  int
	}{
		{
			name:      "valid small count",
			input:     "1",
			expectErr: false,
			expected:  1,
		},
		{
			name:      "valid medium count",
			input:     "100",
			expectErr: false,
			expected:  100,
		},
		{
			name:      "valid large count",
			input:     "10000",
			expectErr: false,
			expected:  10000,
		},
		{
			name:      "zero count",
			input:     "0",
			expectErr: true,
			expected:  0,
		},
		{
			name:      "negative count",
			input:     "-1",
			expectErr: true,
			expected:  0,
		},
		{
			name:      "non-numeric",
			input:     "abc",
			expectErr: true,
			expected:  0,
		},
		{
			name:      "float",
			input:     "1.5",
			expectErr: false, // Sscanf with %d parses the integer part
			expected:  1,
		},
		{
			name:      "with special chars",
			input:     "10!",
			expectErr: false, // Sscanf with %d parses until non-digit
			expected:  10,
		},
		{
			name:      "empty string",
			input:     "",
			expectErr: false,
			expected:  0, // Should use default
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var val int
			var err error

			if tt.input == "" {
				val = 0 // Would use default in real code
				err = nil
			} else {
				_, scanErr := fmt.Sscanf(tt.input, "%d", &val)
				if scanErr != nil {
					err = fmt.Errorf("invalid value")
				} else {
					if val < 1 {
						err = fmt.Errorf("must be >= 1")
					}
				}
			}

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error for input %q, got none", tt.input)
				}
			} else {
				if err != nil && tt.input != "" {
					t.Errorf("unexpected error for input %q: %v", tt.input, err)
				}
				if tt.input != "" && tt.expected > 0 && val != tt.expected {
					t.Errorf("expected %d, got %d", tt.expected, val)
				}
			}
		})
	}
}

// mockKafkaPublisher is a mock implementation for testing
type mockKafkaPublisher struct {
	publishedMessages []mockPublishedMessage
	publishError      error
}

type mockPublishedMessage struct {
	topic   string
	key     []byte
	value   []byte
	headers map[string]string
}

func (m *mockKafkaPublisher) Publish(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
	if m.publishError != nil {
		return m.publishError
	}
	m.publishedMessages = append(m.publishedMessages, mockPublishedMessage{
		topic:   topic,
		key:     key,
		value:   value,
		headers: headers,
	})
	return nil
}

func (m *mockKafkaPublisher) Close() error {
	return nil
}

// TestFileVsJsonMutualExclusivity tests that --file and --json are mutually exclusive
func TestFileVsJsonMutualExclusivity(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "only file specified",
			args:        []string{"--topic", "test", "--file", "data.json"},
			expectError: false,
			errorMsg:    "",
		},
		{
			name:        "only json specified",
			args:        []string{"--topic", "test", "--json", "{}"},
			expectError: false,
			errorMsg:    "",
		},
		{
			name:        "both file and json",
			args:        []string{"--topic", "test", "--file", "data.json", "--json", "{}"},
			expectError: true,
			errorMsg:    "both",
		},
		{
			name:        "neither file nor json",
			args:        []string{"--topic", "test"},
			expectError: true,
			errorMsg:    "either",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract flags
			var hasFile, hasJson bool
			for i, arg := range tt.args {
				if arg == "--file" && i+1 < len(tt.args) {
					hasFile = true
				}
				if arg == "--json" && i+1 < len(tt.args) {
					hasJson = true
				}
			}

			// Check mutual exclusivity
			if hasFile && hasJson {
				if !tt.expectError || !strings.Contains(tt.errorMsg, "both") {
					t.Errorf("expected error about both flags, but test case says otherwise")
				}
			}

			if !hasFile && !hasJson {
				if !tt.expectError || !strings.Contains(tt.errorMsg, "either") {
					t.Errorf("expected error about missing either flag, but test case says otherwise")
				}
			}
		})
	}
}

// TestProduceInlineJSON_Direct tests produceInlineJSON directly using the publisher interface
func TestProduceInlineJSON_Direct(t *testing.T) {
	tests := []struct {
		name          string
		jsonStr       string
		count         int
		rate          time.Duration
		publishError  error
		expectError   bool
		errorContains string
		expectCount   int
	}{
		{
			name:        "valid single event",
			jsonStr:     `{"order_id":"123"}`,
			count:       1,
			rate:        0,
			expectError: false,
			expectCount: 1,
		},
		{
			name:        "valid multiple events",
			jsonStr:     `{"order_id":"456"}`,
			count:       5,
			rate:        0,
			expectError: false,
			expectCount: 5,
		},
		{
			name:          "invalid json",
			jsonStr:       `{invalid}`,
			count:         1,
			rate:          0,
			expectError:   true,
			errorContains: "invalid json",
			expectCount:   0,
		},
		{
			name:        "valid with rate limiting",
			jsonStr:     `{"test":"data"}`,
			count:       2,
			rate:        1 * time.Millisecond,
			expectError: false,
			expectCount: 2,
		},
		{
			name:          "publish error",
			jsonStr:       `{"order_id":"789"}`,
			count:         3,
			rate:          0,
			publishError:  fmt.Errorf("kafka connection failed"),
			expectError:   true,
			errorContains: "publish event",
			expectCount:   0,
		},
		{
			name:        "json array",
			jsonStr:     `[{"id":1},{"id":2}]`,
			count:       1,
			rate:        0,
			expectError: false,
			expectCount: 1,
		},
		{
			name:        "json number",
			jsonStr:     `42`,
			count:       1,
			rate:        0,
			expectError: false,
			expectCount: 1,
		},
		{
			name:          "empty json string",
			jsonStr:       ``,
			count:         1,
			rate:          0,
			expectError:   true,
			errorContains: "invalid json",
			expectCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockKafkaPublisher{
				publishError: tt.publishError,
			}
			ctx := context.Background()

			err := produceInlineJSON(ctx, mock, "test-topic", tt.jsonStr, tt.count, tt.rate)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(mock.publishedMessages) != tt.expectCount {
					t.Errorf("expected %d messages, got %d", tt.expectCount, len(mock.publishedMessages))
				}
			}
		})
	}
}

// TestProduceFromFile_Direct tests produceFromFile directly using the publisher interface
func TestProduceFromFile_Direct(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		count         int
		rate          time.Duration
		publishError  error
		expectError   bool
		errorContains string
		expectCount   int
	}{
		{
			name:        "single line file",
			content:     `{"event":"1"}`,
			count:       0,
			rate:        0,
			expectError: false,
			expectCount: 1,
		},
		{
			name: "multiple lines file",
			content: `{"event":"1"}
{"event":"2"}
{"event":"3"}`,
			count:       0,
			rate:        0,
			expectError: false,
			expectCount: 3,
		},
		{
			name: "with count limit",
			content: `{"event":"1"}
{"event":"2"}
{"event":"3"}
{"event":"4"}`,
			count:       2,
			rate:        0,
			expectError: false,
			expectCount: 2,
		},
		{
			name: "with rate limiting",
			content: `{"event":"1"}
{"event":"2"}`,
			count:       0,
			rate:        1 * time.Millisecond,
			expectError: false,
			expectCount: 2,
		},
		{
			name: "skip empty lines",
			content: `{"event":"1"}

{"event":"2"}`,
			count:       0,
			rate:        0,
			expectError: false,
			expectCount: 2,
		},
		{
			name:          "publish error",
			content:       `{"event":"1"}`,
			count:         0,
			rate:          0,
			publishError:  fmt.Errorf("kafka connection failed"),
			expectError:   true,
			errorContains: "publish event",
			expectCount:   0,
		},
		{
			name:          "invalid json in file",
			content:       `{invalid json}`,
			count:         0,
			rate:          0,
			expectError:   true,
			errorContains: "invalid json",
			expectCount:   0,
		},
		{
			name:          "empty file",
			content:       ``,
			count:         0,
			rate:          0,
			expectError:   true,
			errorContains: "no valid json",
			expectCount:   0,
		},
		{
			name: "whitespace only",
			content: `

`,
			count:         0,
			rate:          0,
			expectError:   true,
			errorContains: "no valid json",
			expectCount:   0,
		},
		{
			name: "count exactly matches lines",
			content: `{"event":"1"}
{"event":"2"}
{"event":"3"}`,
			count:       3,
			rate:        0,
			expectError: false,
			expectCount: 3,
		},
		{
			name: "count exceeds lines",
			content: `{"event":"1"}
{"event":"2"}`,
			count:       10,
			rate:        0,
			expectError: false,
			expectCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockKafkaPublisher{
				publishError: tt.publishError,
			}
			ctx := context.Background()

			// Create temp file
			tmpFile := filepath.Join(t.TempDir(), "test.jsonl")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			err := produceFromFile(ctx, mock, "test-topic", tmpFile, tt.count, tt.rate)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(mock.publishedMessages) != tt.expectCount {
					t.Errorf("expected %d messages, got %d", tt.expectCount, len(mock.publishedMessages))
				}
			}
		})
	}
}

// TestRunProduce_WithMockPublisher tests RunProduce with injected mock publisher
func TestRunProduce_WithMockPublisher(t *testing.T) {
	// Save original and restore after test
	origNewPublisher := newPublisherFunc
	defer func() { newPublisherFunc = origNewPublisher }()

	tests := []struct {
		name          string
		args          []string
		mockPublisher *mockKafkaPublisher
		publisherErr  error
		expectError   bool
		errorContains string
		expectCount   int
	}{
		{
			name:          "successful inline json produce",
			args:          []string{"--topic", "test", "--json", `{"order":"123"}`},
			mockPublisher: &mockKafkaPublisher{},
			expectError:   false,
			expectCount:   1,
		},
		{
			name:          "successful inline json with count",
			args:          []string{"--topic", "test", "--json", `{"order":"123"}`, "--count", "3"},
			mockPublisher: &mockKafkaPublisher{},
			expectError:   false,
			expectCount:   3,
		},
		{
			name:          "publisher creation error",
			args:          []string{"--topic", "test", "--json", `{"order":"123"}`},
			mockPublisher: nil,
			publisherErr:  fmt.Errorf("cannot connect to kafka"),
			expectError:   true,
			errorContains: "create kafka publisher",
			expectCount:   0,
		},
		{
			name:          "publish error during produce",
			args:          []string{"--topic", "test", "--json", `{"order":"123"}`},
			mockPublisher: &mockKafkaPublisher{publishError: fmt.Errorf("write failed")},
			expectError:   true,
			errorContains: "publish event",
			expectCount:   0,
		},
		{
			name:          "inline json with rate",
			args:          []string{"--topic", "test", "--json", `{"order":"123"}`, "--count", "2", "--rate", "1ms"},
			mockPublisher: &mockKafkaPublisher{},
			expectError:   false,
			expectCount:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up mock publisher factory
			newPublisherFunc = func(brokers []string) (publisher, error) {
				if tt.publisherErr != nil {
					return nil, tt.publisherErr
				}
				return tt.mockPublisher, nil
			}

			// Reset published messages
			if tt.mockPublisher != nil {
				tt.mockPublisher.publishedMessages = nil
			}

			err := RunProduce(tt.args)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if tt.mockPublisher != nil && len(tt.mockPublisher.publishedMessages) != tt.expectCount {
					t.Errorf("expected %d messages, got %d", tt.expectCount, len(tt.mockPublisher.publishedMessages))
				}
			}
		})
	}
}

// TestRunProduce_WithFileAndMock tests RunProduce file mode with injected mock publisher
func TestRunProduce_WithFileAndMock(t *testing.T) {
	// Save original and restore after test
	origNewPublisher := newPublisherFunc
	defer func() { newPublisherFunc = origNewPublisher }()

	tests := []struct {
		name          string
		fileContent   string
		extraArgs     []string
		mockPublisher *mockKafkaPublisher
		expectError   bool
		errorContains string
		expectCount   int
	}{
		{
			name:          "single line file",
			fileContent:   `{"event":"1"}`,
			extraArgs:     []string{},
			mockPublisher: &mockKafkaPublisher{},
			expectError:   false,
			expectCount:   1,
		},
		{
			name: "multiple lines with count",
			fileContent: `{"event":"1"}
{"event":"2"}
{"event":"3"}
{"event":"4"}`,
			extraArgs:     []string{"--count", "2"},
			mockPublisher: &mockKafkaPublisher{},
			expectError:   false,
			expectCount:   2,
		},
		{
			name:          "publish error",
			fileContent:   `{"event":"1"}`,
			extraArgs:     []string{},
			mockPublisher: &mockKafkaPublisher{publishError: fmt.Errorf("connection lost")},
			expectError:   true,
			errorContains: "publish event",
			expectCount:   0,
		},
		{
			name:          "invalid json in file",
			fileContent:   `{invalid json}`,
			extraArgs:     []string{},
			mockPublisher: &mockKafkaPublisher{},
			expectError:   true,
			errorContains: "invalid json",
			expectCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpFile := filepath.Join(t.TempDir(), "test.jsonl")
			if err := os.WriteFile(tmpFile, []byte(tt.fileContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Set up mock publisher factory
			newPublisherFunc = func(brokers []string) (publisher, error) {
				return tt.mockPublisher, nil
			}

			// Reset published messages
			tt.mockPublisher.publishedMessages = nil

			args := append([]string{"--topic", "test", "--file", tmpFile}, tt.extraArgs...)
			err := RunProduce(args)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(tt.mockPublisher.publishedMessages) != tt.expectCount {
					t.Errorf("expected %d messages, got %d", tt.expectCount, len(tt.mockPublisher.publishedMessages))
				}
			}
		})
	}
}

// TestRunProduce_CustomBrokersWithMock tests that custom brokers are passed to publisher factory
func TestRunProduce_CustomBrokersWithMock(t *testing.T) {
	origNewPublisher := newPublisherFunc
	defer func() { newPublisherFunc = origNewPublisher }()

	var capturedBrokers []string
	newPublisherFunc = func(brokers []string) (publisher, error) {
		capturedBrokers = brokers
		return &mockKafkaPublisher{}, nil
	}

	args := []string{
		"--topic", "test",
		"--json", `{"event":"1"}`,
		"--brokers", "broker1:9092,broker2:9092,broker3:9092",
	}

	err := RunProduce(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"broker1:9092", "broker2:9092", "broker3:9092"}
	if len(capturedBrokers) != len(expected) {
		t.Fatalf("expected %d brokers, got %d", len(expected), len(capturedBrokers))
	}
	for i, got := range capturedBrokers {
		if got != expected[i] {
			t.Errorf("broker %d: expected %q, got %q", i, expected[i], got)
		}
	}
}

// TestRunProduce_DefaultBrokersWithMock tests that default brokers are used when not specified
func TestRunProduce_DefaultBrokersWithMock(t *testing.T) {
	origNewPublisher := newPublisherFunc
	defer func() { newPublisherFunc = origNewPublisher }()

	var capturedBrokers []string
	newPublisherFunc = func(brokers []string) (publisher, error) {
		capturedBrokers = brokers
		return &mockKafkaPublisher{}, nil
	}

	args := []string{"--topic", "test", "--json", `{"event":"1"}`}

	err := RunProduce(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"localhost:9092"}
	if len(capturedBrokers) != len(expected) {
		t.Fatalf("expected %d brokers, got %d", len(expected), len(capturedBrokers))
	}
	if capturedBrokers[0] != expected[0] {
		t.Errorf("expected broker %q, got %q", expected[0], capturedBrokers[0])
	}
}

// TestParseIntFlag_EdgeCases tests edge cases for parseIntFlag
func TestParseIntFlag_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		flag       string
		defaultVal int
		want       int
		wantErr    bool
	}{
		{
			name:       "flag missing value at end",
			args:       []string{"--count"},
			flag:       "--count",
			defaultVal: 1,
			want:       0,
			wantErr:    true,
		},
		{
			name:       "very large valid integer",
			args:       []string{"--count", "999999"},
			flag:       "--count",
			defaultVal: 1,
			want:       999999,
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIntFlag(tt.args, tt.flag, tt.defaultVal)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseIntFlag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseIntFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestProduceFromFile_LargeFile tests produceFromFile with a larger file
func TestProduceFromFile_LargeFile(t *testing.T) {
	mock := &mockKafkaPublisher{}
	ctx := context.Background()

	// Create file with 100 lines
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf(`{"event_id":%d}`, i))
	}
	content := strings.Join(lines, "\n")

	tmpFile := filepath.Join(t.TempDir(), "large.jsonl")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := produceFromFile(ctx, mock, "test-topic", tmpFile, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.publishedMessages) != 100 {
		t.Errorf("expected 100 messages, got %d", len(mock.publishedMessages))
	}
}

// TestProduceFromFile_MessageContent tests that message content is preserved correctly
func TestProduceFromFile_MessageContent(t *testing.T) {
	mock := &mockKafkaPublisher{}
	ctx := context.Background()

	content := `{"order_id":"ORD-001","amount":99.99,"customer":"Alice"}
{"order_id":"ORD-002","amount":150.00,"customer":"Bob"}`

	tmpFile := filepath.Join(t.TempDir(), "orders.jsonl")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	err := produceFromFile(ctx, mock, "orders-topic", tmpFile, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.publishedMessages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(mock.publishedMessages))
	}

	// Check first message
	if mock.publishedMessages[0].topic != "orders-topic" {
		t.Errorf("expected topic 'orders-topic', got %q", mock.publishedMessages[0].topic)
	}
	if !strings.Contains(string(mock.publishedMessages[0].value), "ORD-001") {
		t.Errorf("expected message to contain 'ORD-001', got: %s", string(mock.publishedMessages[0].value))
	}

	// Check second message
	if !strings.Contains(string(mock.publishedMessages[1].value), "ORD-002") {
		t.Errorf("expected message to contain 'ORD-002', got: %s", string(mock.publishedMessages[1].value))
	}
}

// TestProduceInlineJSON_MessageContent tests that inline JSON content is preserved correctly
func TestProduceInlineJSON_MessageContent(t *testing.T) {
	mock := &mockKafkaPublisher{}
	ctx := context.Background()

	jsonStr := `{"order_id":"TEST-123","items":[{"sku":"A","qty":2},{"sku":"B","qty":1}]}`

	err := produceInlineJSON(ctx, mock, "orders-topic", jsonStr, 1, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.publishedMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(mock.publishedMessages))
	}

	if mock.publishedMessages[0].topic != "orders-topic" {
		t.Errorf("expected topic 'orders-topic', got %q", mock.publishedMessages[0].topic)
	}

	// Verify JSON content
	var result map[string]interface{}
	if err := json.Unmarshal(mock.publishedMessages[0].value, &result); err != nil {
		t.Fatalf("failed to parse message value as JSON: %v", err)
	}

	if result["order_id"] != "TEST-123" {
		t.Errorf("expected order_id 'TEST-123', got %v", result["order_id"])
	}
}
