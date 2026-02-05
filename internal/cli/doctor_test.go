package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
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

func TestRunDoctor_AllChecksPass(t *testing.T) {
	origLookPath := lookPathFunc
	origExecOutput := execOutputFunc
	origExecRun := execRunFunc
	defer func() {
		lookPathFunc = origLookPath
		execOutputFunc = origExecOutput
		execRunFunc = origExecRun
	}()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}
	execOutputFunc = func(name string, args ...string) ([]byte, error) {
		return []byte("27.5.1\n"), nil
	}
	execRunFunc = func(name string, args ...string) error {
		return nil
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create valid project structure
	if err := os.MkdirAll("fiso/flows", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

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

	err = RunDoctor([]string{})
	if err != nil {
		t.Errorf("expected no error when all checks pass, got: %v", err)
	}
}

func TestCheckDockerDaemon_VersionFormatting(t *testing.T) {
	origExecOutput := execOutputFunc
	defer func() { execOutputFunc = origExecOutput }()

	// Test with version string
	execOutputFunc = func(name string, args ...string) ([]byte, error) {
		return []byte("27.5.1\n"), nil
	}

	ok, detail := checkDockerDaemon()
	if !ok {
		t.Error("expected checkDockerDaemon to succeed")
	}
	if detail != " (v27.5.1)" {
		t.Errorf("expected detail ' (v27.5.1)', got: %s", detail)
	}

	// Test with empty version
	execOutputFunc = func(name string, args ...string) ([]byte, error) {
		return []byte("\n"), nil
	}

	ok, detail = checkDockerDaemon()
	if !ok {
		t.Error("expected checkDockerDaemon to succeed with empty version")
	}
	if detail != "" {
		t.Errorf("expected empty detail, got: %s", detail)
	}
}

func TestCheckConfigValidity_NonYAMLSkipped(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create fiso/flows directory
	if err := os.MkdirAll("fiso/flows", 0755); err != nil {
		t.Fatal(err)
	}

	// Create a non-YAML file (should be skipped)
	if err := os.WriteFile("fiso/flows/readme.txt", []byte("readme"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory (should be skipped)
	if err := os.MkdirAll("fiso/flows/subdir", 0755); err != nil {
		t.Fatal(err)
	}

	// Create a valid YAML file
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
	if err := os.WriteFile("fiso/flows/valid.yaml", []byte(validFlow), 0644); err != nil {
		t.Fatal(err)
	}

	ok, detail := checkConfigValidity()
	if !ok {
		t.Error("expected checkConfigValidity to succeed")
	}
	if detail != " (1 flow)" {
		t.Errorf("expected detail ' (1 flow)', got: %s", detail)
	}
}

func TestCheckDockerInstalled_NotFound(t *testing.T) {
	origLookPath := lookPathFunc
	defer func() { lookPathFunc = origLookPath }()

	lookPathFunc = func(file string) (string, error) {
		return "", fmt.Errorf("executable file not found in $PATH")
	}

	ok, _ := checkDockerInstalled()
	if ok {
		t.Error("expected checkDockerInstalled to fail when docker not found")
	}
}

func TestRunDoctor_DockerNotRunning(t *testing.T) {
	origLookPath := lookPathFunc
	origExecOutput := execOutputFunc
	origExecRun := execRunFunc
	defer func() {
		lookPathFunc = origLookPath
		execOutputFunc = origExecOutput
		execRunFunc = origExecRun
	}()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}
	execOutputFunc = func(name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("Cannot connect to the Docker daemon")
	}
	execRunFunc = func(name string, args ...string) error {
		return nil
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create valid project structure
	if err := os.MkdirAll("fiso/flows", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = RunDoctor([]string{})
	if err == nil {
		t.Fatal("expected error when Docker daemon not running")
	}
	if !strings.Contains(err.Error(), "issue(s)") {
		t.Errorf("expected error to contain 'issue(s)', got: %v", err)
	}
}

func TestRunDoctor_ComposeNotAvailable(t *testing.T) {
	origLookPath := lookPathFunc
	origExecOutput := execOutputFunc
	origExecRun := execRunFunc
	defer func() {
		lookPathFunc = origLookPath
		execOutputFunc = origExecOutput
		execRunFunc = origExecRun
	}()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}
	execOutputFunc = func(name string, args ...string) ([]byte, error) {
		return []byte("27.5.1\n"), nil
	}
	execRunFunc = func(name string, args ...string) error {
		return fmt.Errorf("docker: 'compose' is not a docker command")
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create valid project structure
	if err := os.MkdirAll("fiso/flows", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = RunDoctor([]string{})
	if err == nil {
		t.Fatal("expected error when Docker Compose not available")
	}
	if !strings.Contains(err.Error(), "issue(s)") {
		t.Errorf("expected error to contain 'issue(s)', got: %v", err)
	}
}

func TestRunDoctor_PortInUse(t *testing.T) {
	origLookPath := lookPathFunc
	origExecOutput := execOutputFunc
	origExecRun := execRunFunc
	defer func() {
		lookPathFunc = origLookPath
		execOutputFunc = origExecOutput
		execRunFunc = origExecRun
	}()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}
	execOutputFunc = func(name string, args ...string) ([]byte, error) {
		return []byte("27.5.1\n"), nil
	}
	execRunFunc = func(name string, args ...string) error {
		return nil
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Create valid project structure
	if err := os.MkdirAll("fiso/flows", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

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

	// Listen on port 8081
	listener, err := net.Listen("tcp", ":8081")
	if err != nil {
		t.Skipf("unable to bind port 8081: %v", err)
	}
	defer func() { _ = listener.Close() }()

	err = RunDoctor([]string{})
	// Port warnings are informational, not fatal
	if err != nil {
		t.Errorf("expected no error with port warnings, got: %v", err)
	}
}
