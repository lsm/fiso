package cli

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestRunValidate_InvalidCEL(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "cel-bad.yaml", `
name: cel-test
source:
  type: kafka
  config: {}
transform:
  cel: 'this is not valid CEL {{{'
sink:
  type: http
  config: {}
`)
	if err := RunValidate([]string{dir}); err == nil {
		t.Fatal("expected error for invalid CEL expression")
	}
}

func TestRunValidate_ValidCEL(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "cel-good.yaml", `
name: cel-test
source:
  type: kafka
  config: {}
transform:
  cel: '{"id": data.id}'
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
	defer os.Chdir(orig)
	os.Chdir(dir)

	os.MkdirAll(filepath.Join(dir, "flows"), 0755)
	writeTestFile(t, filepath.Join(dir, "flows"), "good.yaml", `
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
	}
	for _, tt := range tests {
		got := inferField(tt.msg)
		if got != tt.want {
			t.Errorf("inferField(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
