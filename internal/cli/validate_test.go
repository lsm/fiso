package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunValidate_ValidFlows(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "good.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)
	if err := RunValidate([]string{dir}); err != nil {
		t.Errorf("expected no error for valid config, got: %v", err)
	}
}

func TestRunValidate_InvalidFlow(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "bad.yaml", `
source:
  type: redis
sink:
  type: ftp
`)
	if err := RunValidate([]string{dir}); err == nil {
		t.Fatal("expected error for invalid flow config")
	}
}

func TestRunValidate_InvalidFields(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "fields-bad.yaml", `
name: fields-test
source:
  type: kafka
  config: {}
transform:
  fields:
    invalid_field: 'this is not valid CEL {{{'
sink:
  type: http
  config: {}
`)
	if err := RunValidate([]string{dir}); err == nil {
		t.Fatal("expected error for invalid CEL expression")
	}
}

func TestRunValidate_ValidFields(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "fields-good.yaml", `
name: fields-test
source:
  type: kafka
  config: {}
transform:
  fields: {id: data.id}
sink:
  type: http
  config: {}
`)
	if err := RunValidate([]string{dir}); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestRunValidate_MultipleErrors(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "multi-err.yaml", `
source:
  type: invalid
sink:
  type: invalid
errorHandling:
  maxRetries: -1
`)
	errs := validateFlowFile(filepath.Join(dir, "multi-err.yaml"))
	if len(errs) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(errs), errs)
	}
}

func TestRunValidate_DefaultPath(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "fiso", "flows"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "fiso", "flows"), "good.yaml", `
name: test
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)
	if err := RunValidate(nil); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestRunValidate_Help(t *testing.T) {
	if err := RunValidate([]string{"-h"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunValidate_LinkConfigInSubdir(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create fiso/flows/ with a valid flow
	if err := os.MkdirAll(filepath.Join(dir, "fiso", "flows"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "fiso", "flows"), "good.yaml", `
name: test
source:
  type: http
  config: {}
sink:
  type: http
  config: {}
`)

	// Create fiso/link/config.yaml with a valid link config
	if err := os.MkdirAll(filepath.Join(dir, "fiso", "link"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "fiso", "link"), "config.yaml", `
listenAddr: ":3500"
targets:
  - name: echo
    protocol: http
    host: external-api
`)

	if err := RunValidate(nil); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestSplitErrors(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"error one\nerror two", 2},
		{"single error", 1},
	}
	for _, tt := range tests {
		got := splitErrors(fmt.Errorf("%s", tt.input))
		if len(got) != tt.want {
			t.Errorf("splitErrors(%q) = %d parts, want %d", tt.input, len(got), tt.want)
		}
	}
}

func TestSplitErrors_Nil(t *testing.T) {
	got := splitErrors(nil)
	if len(got) != 0 {
		t.Errorf("splitErrors(nil) = %d parts, want 0", len(got))
	}
}

func TestInferField(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{"name is required", "name"},
		{"source.type is required", "source.type"},
		{`source.type "redis" is not valid`, "source.type"},
		{`target[0] "crm": host is required`, "host"},
		{"", "-"},
	}
	for _, tt := range tests {
		got := inferField(tt.msg)
		if got != tt.want {
			t.Errorf("inferField(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}

func TestRunValidate_NonExistentDir(t *testing.T) {
	err := RunValidate([]string{"/nonexistent/path"})
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
	if !strings.Contains(err.Error(), "scanning") {
		t.Errorf("expected error to contain 'scanning', got: %v", err)
	}
}

func TestRunValidate_InvalidLinkConfig(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "fiso", "flows"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "fiso", "flows"), "good.yaml", `
name: test
source:
  type: http
  config: {}
sink:
  type: http
  config: {}
`)

	if err := os.MkdirAll(filepath.Join(dir, "fiso", "link"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "fiso", "link"), "config.yaml", `{{{`)

	if err := RunValidate(nil); err == nil {
		t.Fatal("expected error for invalid link config YAML")
	}
}

func TestRunValidate_LinkValidationErrors(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "fiso", "flows"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "fiso", "flows"), "good.yaml", `
name: test
source:
  type: http
  config: {}
sink:
  type: http
  config: {}
`)

	if err := os.MkdirAll(filepath.Join(dir, "fiso", "link"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "fiso", "link"), "config.yaml", `
targets:
  - protocol: http
`)

	if err := RunValidate(nil); err == nil {
		t.Fatal("expected validation error for link config with missing required fields")
	}
}

func TestRunValidate_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if err := RunValidate([]string{dir}); err != nil {
		t.Errorf("expected no error for empty directory, got: %v", err)
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateFlowFile_ReadError(t *testing.T) {
	nonExistentPath := filepath.Join(t.TempDir(), "nonexistent.yaml")

	errs := validateFlowFile(nonExistentPath)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for non-existent file")
	}
	if !strings.Contains(errs[0].Message, "read error") {
		t.Errorf("expected read error, got: %v", errs[0].Message)
	}
}

func TestValidateFlowFile_InvalidMapping(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "invalid-mapping.yaml")

	// Create a flow with a non-empty mapping but missing name (to trigger validation error)
	// Since mapping validation only checks if empty, we test with an invalid flow instead
	flowYAML := `name: ""
source:
  type: http
  config: {}
transform:
  fields:
    field1: value1
sink:
  type: http
  config: {}
`
	if err := os.WriteFile(flowPath, []byte(flowYAML), 0644); err != nil {
		t.Fatal(err)
	}

	errs := validateFlowFile(flowPath)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for invalid flow")
	}

	// Check that we got a validation error (in this case, for the empty name)
	foundError := false
	for _, e := range errs {
		if strings.Contains(e.Message, "name is required") {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Errorf("expected validation error, got: %v", errs)
	}
}

func TestValidateFlowFile_ValidMapping(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "valid-mapping.yaml")

	// Create a flow with a valid mapping
	flowYAML := `name: mapping-test
source:
  type: http
  config: {}
transform:
  fields:
    order_id: data.user_id
    total: data.amount
    status: '"pending"'
sink:
  type: http
  config: {}
`
	if err := os.WriteFile(flowPath, []byte(flowYAML), 0644); err != nil {
		t.Fatal(err)
	}

	errs := validateFlowFile(flowPath)
	if len(errs) > 0 {
		t.Errorf("expected no validation errors for valid mapping, got: %v", errs)
	}
}

func TestValidateFlowFile_MappingValuesError(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "mapping-error.yaml")

	// Create a YAML that triggers "mapping values" error - unquoted ternary
	// The error occurs when YAML sees an unexpected colon in a value
	flowYAML := `name: test
transform:
  fields:
    priority: data.urgent ? 1 : 2
sink:
  type: http
`
	if err := os.WriteFile(flowPath, []byte(flowYAML), 0644); err != nil {
		t.Fatal(err)
	}

	errs := validateFlowFile(flowPath)
	if len(errs) == 0 {
		t.Fatal("expected validation error for mapping values")
	}

	// Check that we got the hint about quoting CEL expressions
	foundHint := false
	for _, e := range errs {
		if strings.Contains(e.Message, "Hint:") && strings.Contains(e.Message, "quote") {
			foundHint = true
			break
		}
	}
	if !foundHint {
		t.Errorf("expected hint about quoting CEL expressions, got: %v", errs)
	}
}

func TestValidateFlowFile_GenericYAMLError(t *testing.T) {
	dir := t.TempDir()
	flowPath := filepath.Join(dir, "generic-error.yaml")

	// Create a YAML with a generic parse error (not mapping values)
	flowYAML := `name: [invalid yaml structure
source:
  type: http
`
	if err := os.WriteFile(flowPath, []byte(flowYAML), 0644); err != nil {
		t.Fatal(err)
	}

	errs := validateFlowFile(flowPath)
	if len(errs) == 0 {
		t.Fatal("expected validation error for invalid YAML")
	}

	// Check that we got a YAML parse error (not a hint)
	foundParseError := false
	for _, e := range errs {
		if strings.Contains(e.Message, "YAML parse error") && !strings.Contains(e.Message, "Hint:") {
			foundParseError = true
			break
		}
	}
	if !foundParseError {
		t.Errorf("expected generic YAML parse error, got: %v", errs)
	}
}

func TestValidateLinkConfig_MappingValuesError(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "link-mapping-error.yaml")

	// Create a YAML that triggers "mapping values" error - unquoted colon in value position
	linkYAML := `listenAddr: ":3500"
targets:
  - name: test
    timeout: 30s: invalid
`
	if err := os.WriteFile(linkPath, []byte(linkYAML), 0644); err != nil {
		t.Fatal(err)
	}

	errs := validateLinkConfig(linkPath)
	if len(errs) == 0 {
		t.Fatal("expected validation error for mapping values")
	}

	// Check that we got the hint
	foundHint := false
	for _, e := range errs {
		if strings.Contains(e.Message, "Hint:") {
			foundHint = true
			break
		}
	}
	if !foundHint {
		t.Errorf("expected hint about quoting values, got: %v", errs)
	}
}

func TestValidateLinkConfig_GenericYAMLError(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "link-generic-error.yaml")

	// Create a YAML with a generic parse error (not mapping values)
	linkYAML := `listenAddr: [invalid yaml structure
targets:
`
	if err := os.WriteFile(linkPath, []byte(linkYAML), 0644); err != nil {
		t.Fatal(err)
	}

	errs := validateLinkConfig(linkPath)
	if len(errs) == 0 {
		t.Fatal("expected validation error for invalid YAML")
	}

	// Check that we got a YAML parse error (not a hint)
	foundParseError := false
	for _, e := range errs {
		if strings.Contains(e.Message, "YAML parse error") && !strings.Contains(e.Message, "Hint:") {
			foundParseError = true
			break
		}
	}
	if !foundParseError {
		t.Errorf("expected generic YAML parse error, got: %v", errs)
	}
}

func TestValidateLinkConfig_ReadError(t *testing.T) {
	nonExistentPath := filepath.Join(t.TempDir(), "nonexistent.yaml")

	errs := validateLinkConfig(nonExistentPath)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for non-existent file")
	}
	if !strings.Contains(errs[0].Message, "read error") {
		t.Errorf("expected read error, got: %v", errs[0].Message)
	}
}

func TestValidateFlowDir_SkipsDirectories(t *testing.T) {
	dir := t.TempDir()

	// Create a subdirectory that should be skipped
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create a valid flow file
	writeTestFile(t, dir, "good.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)

	errs, err := validateFlowDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) > 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidateFlowDir_SkipsNonYAML(t *testing.T) {
	dir := t.TempDir()

	// Create a non-YAML file that should be skipped
	writeTestFile(t, dir, "readme.txt", "This is not YAML")
	writeTestFile(t, dir, "config.json", `{"key": "value"}`)

	// Create a valid flow file
	writeTestFile(t, dir, "good.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)

	errs, err := validateFlowDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) > 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestRunValidate_LinkConfigAtAlternateLocations(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create fiso/flows/ with a valid flow
	if err := os.MkdirAll(filepath.Join(dir, "fiso", "flows"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "fiso", "flows"), "good.yaml", `
name: test
source:
  type: http
  config: {}
sink:
  type: http
  config: {}
`)

	// Create fiso/link-config.yaml (alternate location)
	writeTestFile(t, filepath.Join(dir, "fiso"), "link-config.yaml", `
listenAddr: ":3500"
targets:
  - name: echo
    protocol: http
    host: external-api
`)

	if err := RunValidate(nil); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestRunValidate_LinkConfigLinksYAML(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create fiso/flows/ with a valid flow
	if err := os.MkdirAll(filepath.Join(dir, "fiso", "flows"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "fiso", "flows"), "good.yaml", `
name: test
source:
  type: http
  config: {}
sink:
  type: http
  config: {}
`)

	// Create fiso/links.yaml (another alternate location)
	writeTestFile(t, filepath.Join(dir, "fiso"), "links.yaml", `
listenAddr: ":3500"
targets:
  - name: echo
    protocol: http
    host: external-api
`)

	if err := RunValidate(nil); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateFlowDir_YMLExtension(t *testing.T) {
	dir := t.TempDir()

	// Create a file with .yml extension (not .yaml)
	writeTestFile(t, dir, "good.yml", `
name: test-flow
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)

	errs, err := validateFlowDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) > 0 {
		t.Errorf("expected no errors for .yml file, got: %v", errs)
	}
}
