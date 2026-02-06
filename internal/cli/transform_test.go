package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunTransform_Help(t *testing.T) {
	if err := RunTransform([]string{"-h"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTransformTest_Help(t *testing.T) {
	if err := RunTransformTest([]string{"-h"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTransformTest_InlineJSON(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "test-flow.yaml")
	writeTestFile(t, dir, "test-flow.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
transform:
  fields:
    order_id: data.legacy_id
    customer_id: data.customer_id
    status: '"pending"'
sink:
  type: http
  config: {}
`)

	input := `{"data":{"legacy_id":"ORD-123","customer_id":"CUST-456","amount":100.50}}`

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := RunTransformTest([]string{"--flow", flowPath, "--input", input})
	_ = w.Close()
	os.Stdout = old

	var buf strings.Builder
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output is valid JSON and contains expected fields
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput: %s", err, output)
	}

	if result["order_id"] != "ORD-123" {
		t.Errorf("expected order_id ORD-123, got %v", result["order_id"])
	}
	if result["customer_id"] != "CUST-456" {
		t.Errorf("expected customer_id CUST-456, got %v", result["customer_id"])
	}
	if result["status"] != "pending" {
		t.Errorf("expected status pending, got %v", result["status"])
	}
}

func TestRunTransformTest_JSONFile(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "test-flow.yaml")
	writeTestFile(t, dir, "test-flow.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
transform:
  fields:
    id: data.user_id
    email: data.contact_email
sink:
  type: http
  config: {}
`)

	inputPath := filepath.Join(dir, "input.json")
	inputData := `{"data":{"user_id":"user-1","contact_email":"test@example.com"}}`
	if err := os.WriteFile(inputPath, []byte(inputData), 0644); err != nil {
		t.Fatal(err)
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := RunTransformTest([]string{"--flow", flowPath, "--input", inputPath})
	_ = w.Close()
	os.Stdout = old

	var buf strings.Builder
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if result["id"] != "user-1" {
		t.Errorf("expected id user-1, got %v", result["id"])
	}
	if result["email"] != "test@example.com" {
		t.Errorf("expected email test@example.com, got %v", result["email"])
	}
}

func TestRunTransformTest_JSONLFile(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "test-flow.yaml")
	writeTestFile(t, dir, "test-flow.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
transform:
  fields:
    order_id: data.id
sink:
  type: http
  config: {}
`)

	inputPath := filepath.Join(dir, "input.jsonl")
	inputData := `{"data":{"id":"first-order"}}` + "\n" + `{"data":{"id":"second-order"}}`
	if err := os.WriteFile(inputPath, []byte(inputData), 0644); err != nil {
		t.Fatal(err)
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := RunTransformTest([]string{"--flow", flowPath, "--input", inputPath})
	_ = w.Close()
	os.Stdout = old

	var buf strings.Builder
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// Should use first line only
	if result["order_id"] != "first-order" {
		t.Errorf("expected order_id first-order, got %v", result["order_id"])
	}
}

func TestRunTransformTest_MissingFlowFlag(t *testing.T) {
	err := RunTransformTest([]string{"--input", `'{"test":"data"}'`})
	if err == nil {
		t.Fatal("expected error for missing --flow flag")
	}
	if !strings.Contains(err.Error(), "--flow is required") {
		t.Errorf("expected error about --flow, got: %v", err)
	}
}

func TestRunTransformTest_MissingInputFlag(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "test-flow.yaml")
	writeTestFile(t, dir, "test-flow.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
transform:
  fields:
    test: data.value
sink:
  type: http
  config: {}
`)

	err := RunTransformTest([]string{"--flow", flowPath})
	if err == nil {
		t.Fatal("expected error for missing --input flag")
	}
	if !strings.Contains(err.Error(), "--input is required") {
		t.Errorf("expected error about --input, got: %v", err)
	}
}

func TestRunTransformTest_NoTransformInFlow(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "test-flow.yaml")
	writeTestFile(t, dir, "test-flow.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)

	err := RunTransformTest([]string{"--flow", flowPath, "--input", `{"test":"data"}`})
	if err == nil {
		t.Fatal("expected error for flow without transform")
	}
	if !strings.Contains(err.Error(), "does not have a transform configuration") {
		t.Errorf("expected error about transform configuration, got: %v", err)
	}
}

func TestRunTransformTest_InvalidJSONInput(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "test-flow.yaml")
	writeTestFile(t, dir, "test-flow.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
transform:
  fields:
    output: data.input
sink:
  type: http
  config: {}
`)

	err := RunTransformTest([]string{"--flow", flowPath, "--input", `{invalid json}`})
	if err == nil {
		t.Fatal("expected error for invalid JSON input")
	}
}

func TestRunTransformTest_InvalidCELExpression(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "test-flow.yaml")
	writeTestFile(t, dir, "test-flow.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
transform:
  fields:
    output: 'this is not valid CEL {{{'
sink:
  type: http
  config: {}
`)

	err := RunTransformTest([]string{"--flow", flowPath, "--input", `{"test":"data"}`})
	if err == nil {
		t.Fatal("expected error for invalid CEL expression")
	}
}

func TestRunTransformTest_NonExistentFlowFile(t *testing.T) {
	err := RunTransformTest([]string{
		"--flow", "/nonexistent/flow.yaml",
		"--input", `{"test":"data"}`,
	})
	if err == nil {
		t.Fatal("expected error for non-existent flow file")
	}
}

func TestRunTransformTest_NonExistentInputFile(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "test-flow.yaml")
	writeTestFile(t, dir, "test-flow.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
transform:
  fields:
    output: data.input
sink:
  type: http
  config: {}
`)

	err := RunTransformTest([]string{
		"--flow", flowPath,
		"--input", "/nonexistent/input.json",
	})
	// Should treat as inline JSON, not a file
	if err == nil {
		t.Fatal("expected error for invalid JSON when file doesn't exist")
	}
}

func TestRunTransformTest_EqualSyntax(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "test-flow.yaml")
	writeTestFile(t, dir, "test-flow.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
transform:
  fields:
    result: data.value
sink:
  type: http
  config: {}
`)

	// Test --flow=path and --input=value syntax
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := RunTransformTest([]string{
		"--flow=" + flowPath,
		"--input=" + `{"data":{"value":"test"}}`,
	})
	_ = w.Close()
	os.Stdout = old

	var buf strings.Builder
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if result["result"] != "test" {
		t.Errorf("expected result test, got %v", result["result"])
	}
}

func TestRunTransform_UnknownSubcommand(t *testing.T) {
	err := RunTransform([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown transform subcommand") {
		t.Errorf("expected error about unknown subcommand, got: %v", err)
	}
}

func TestLoadFlow(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "test-flow.yaml")
	writeTestFile(t, dir, "test-flow.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
transform:
  fields:
    test: data.value
sink:
  type: http
  config: {}
`)

	flow, err := loadFlow(flowPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if flow.Name != "test-flow" {
		t.Errorf("expected name test-flow, got %v", flow.Name)
	}
	if flow.Transform == nil {
		t.Error("expected transform to be non-nil")
	}
}

func TestLoadFlow_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(flowPath, []byte("{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadFlow(flowPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadFlow_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "invalid.yaml")
	writeTestFile(t, dir, "invalid.yaml", `
name: ""
source:
  type: invalid
`)

	_, err := loadFlow(flowPath)
	if err == nil {
		t.Fatal("expected error for invalid flow config")
	}
}

func TestLoadInput_InlineJSON(t *testing.T) {
	input := `{"test":"value"}`
	data, err := loadInput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != input {
		t.Errorf("expected %s, got %s", input, string(data))
	}
}

func TestLoadInput_File(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.json")
	inputData := `{"file":"data"}`
	if err := os.WriteFile(inputPath, []byte(inputData), 0644); err != nil {
		t.Fatal(err)
	}

	data, err := loadInput(inputPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != inputData {
		t.Errorf("expected %s, got %s", inputData, string(data))
	}
}

func TestLoadInput_JSONLFile(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.jsonl")
	inputData := "{\"first\":\"line1\"}\n{\"second\":\"line2\"}"
	if err := os.WriteFile(inputPath, []byte(inputData), 0644); err != nil {
		t.Fatal(err)
	}

	data, err := loadInput(inputPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "{\"first\":\"line1\"}"
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}
