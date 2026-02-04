package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunDoctor_Help(t *testing.T) {
	err := RunDoctor([]string{"-h"})
	if err != nil {
		t.Errorf("expected no error for -h flag, got: %v", err)
	}

	err = RunDoctor([]string{"--help"})
	if err != nil {
		t.Errorf("expected no error for --help flag, got: %v", err)
	}
}

func TestCheckProjectStructure_Missing(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	ok, _ := checkProjectStructure()
	if ok {
		t.Error("expected checkProjectStructure to fail in empty directory")
	}
}

func TestCheckProjectStructure_Found(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create project structure
	if err := os.MkdirAll("fiso/flows", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("version: '3'\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ok, _ := checkProjectStructure()
	if !ok {
		t.Error("expected checkProjectStructure to succeed with valid structure")
	}
}

func TestCheckConfigValidity_Valid(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create project structure
	if err := os.MkdirAll("fiso/flows", 0755); err != nil {
		t.Fatal(err)
	}

	// Create a valid flow file
	validFlow := `name: test-flow
source:
  type: http
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://localhost:8082/process
errorHandling:
  maxRetries: 3
`
	if err := os.WriteFile("fiso/flows/test.yaml", []byte(validFlow), 0644); err != nil {
		t.Fatal(err)
	}

	ok, detail := checkConfigValidity()
	if !ok {
		t.Error("expected checkConfigValidity to succeed with valid flow")
	}
	if detail != " (1 flow)" {
		t.Errorf("expected detail ' (1 flow)', got: %s", detail)
	}
}

func TestCheckConfigValidity_Invalid(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create project structure
	if err := os.MkdirAll("fiso/flows", 0755); err != nil {
		t.Fatal(err)
	}

	// Create an invalid flow file (missing required fields)
	invalidFlow := `name: test-flow
source:
  type: http
`
	if err := os.WriteFile("fiso/flows/test.yaml", []byte(invalidFlow), 0644); err != nil {
		t.Fatal(err)
	}

	ok, _ := checkConfigValidity()
	if ok {
		t.Error("expected checkConfigValidity to fail with invalid flow")
	}
}

func TestCheckPorts_Available(t *testing.T) {
	// Test that port checking works - we check a high port that's likely free
	ports := []int{19999}
	warnings := checkPortAvailability(ports)
	if len(warnings) > 0 {
		t.Logf("port 19999 is in use (this is OK, just informational): %v", warnings)
	}
}

func TestRunDoctor_NoProject(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Run doctor in empty directory
	err = RunDoctor([]string{})
	// We expect an error because project structure is missing
	if err == nil {
		t.Error("expected RunDoctor to fail in empty directory")
	}
}

func TestCheckConfigValidity_MultipleFlows(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create project structure
	if err := os.MkdirAll("fiso/flows", 0755); err != nil {
		t.Fatal(err)
	}

	// Create multiple valid flow files
	validFlow := `name: test-flow
source:
  type: http
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://localhost:8082/process
errorHandling:
  maxRetries: 3
`
	if err := os.WriteFile("fiso/flows/flow1.yaml", []byte(validFlow), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/flows/flow2.yml", []byte(validFlow), 0644); err != nil {
		t.Fatal(err)
	}

	ok, detail := checkConfigValidity()
	if !ok {
		t.Error("expected checkConfigValidity to succeed with valid flows")
	}
	if detail != " (2 flows)" {
		t.Errorf("expected detail ' (2 flows)', got: %s", detail)
	}
}

func TestCheckConfigValidity_EmptyFlowsDir(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create project structure with empty flows directory
	if err := os.MkdirAll("fiso/flows", 0755); err != nil {
		t.Fatal(err)
	}

	ok, detail := checkConfigValidity()
	if !ok {
		t.Error("expected checkConfigValidity to succeed with empty flows directory")
	}
	if detail != "" {
		t.Errorf("expected empty detail string, got: %s", detail)
	}
}

func TestCheckConfigValidity_WithLinkConfig(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create project structure
	if err := os.MkdirAll("fiso/flows", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("fiso/link", 0755); err != nil {
		t.Fatal(err)
	}

	// Create a valid flow file
	validFlow := `name: test-flow
source:
  type: http
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://localhost:8082/process
errorHandling:
  maxRetries: 3
`
	if err := os.WriteFile("fiso/flows/test.yaml", []byte(validFlow), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a valid link config
	validLink := `targets:
  - name: external-api
    protocol: http
    host: external-api:9090
`
	if err := os.WriteFile(filepath.Join("fiso", "link", "config.yaml"), []byte(validLink), 0644); err != nil {
		t.Fatal(err)
	}

	ok, detail := checkConfigValidity()
	if !ok {
		t.Error("expected checkConfigValidity to succeed with valid flow and link config")
	}
	if detail != " (1 flow)" {
		t.Errorf("expected detail ' (1 flow)', got: %s", detail)
	}
}
