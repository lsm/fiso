package cli

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunLogs_Help(t *testing.T) {
	if err := RunLogs([]string{"-h"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLogs_HelpLong(t *testing.T) {
	if err := RunLogs([]string{"--help"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLogs_EnvironmentNotFound(t *testing.T) {
	origLookPath := lookPathFunc
	origUserHomeDir := userHomeDirFunc
	defer func() {
		lookPathFunc = origLookPath
		userHomeDirFunc = origUserHomeDir
	}()

	lookPathFunc = func(file string) (string, error) {
		return "", os.ErrNotExist
	}
	userHomeDirFunc = func() (string, error) {
		return "/tmp/test-no-kube", nil
	}

	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	err = RunLogs([]string{})
	if err == nil {
		t.Fatal("expected error when no environment found")
	}
	if !strings.Contains(err.Error(), "no supported environment found") {
		t.Errorf("expected error to contain 'no supported environment found', got: %v", err)
	}
}

func TestRunLogs_DockerComposeEnv(t *testing.T) {
	origLookPath := lookPathFunc
	origShowDockerComposeLogs := showDockerComposeLogsFn
	defer func() {
		lookPathFunc = origLookPath
		showDockerComposeLogsFn = origShowDockerComposeLogs
	}()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}

	var receivedService string
	var receivedTail int
	var receivedFollow bool

	showDockerComposeLogsFn = func(service string, tail int, follow bool) error {
		receivedService = service
		receivedTail = tail
		receivedFollow = follow
		return nil
	}

	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("fiso", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = RunLogs([]string{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if receivedService != "fiso-flow" {
		t.Errorf("expected service 'fiso-flow', got: %s", receivedService)
	}
	if receivedTail != 100 {
		t.Errorf("expected tail 100, got: %d", receivedTail)
	}
	if receivedFollow {
		t.Error("expected follow to be false by default")
	}
}

func TestRunLogs_WithTail(t *testing.T) {
	origLookPath := lookPathFunc
	origShowDockerComposeLogs := showDockerComposeLogsFn
	defer func() {
		lookPathFunc = origLookPath
		showDockerComposeLogsFn = origShowDockerComposeLogs
	}()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}

	var receivedTail int
	showDockerComposeLogsFn = func(service string, tail int, follow bool) error {
		receivedTail = tail
		return nil
	}

	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("fiso", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = RunLogs([]string{"--tail", "50"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if receivedTail != 50 {
		t.Errorf("expected tail 50, got: %d", receivedTail)
	}
}

func TestRunLogs_WithFollow(t *testing.T) {
	origLookPath := lookPathFunc
	origShowDockerComposeLogs := showDockerComposeLogsFn
	defer func() {
		lookPathFunc = origLookPath
		showDockerComposeLogsFn = origShowDockerComposeLogs
	}()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}

	var receivedFollow bool
	showDockerComposeLogsFn = func(service string, tail int, follow bool) error {
		receivedFollow = follow
		return nil
	}

	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("fiso", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = RunLogs([]string{"--follow"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !receivedFollow {
		t.Error("expected follow to be true")
	}
}

func TestRunLogs_WithFollowShort(t *testing.T) {
	origLookPath := lookPathFunc
	origShowDockerComposeLogs := showDockerComposeLogsFn
	defer func() {
		lookPathFunc = origLookPath
		showDockerComposeLogsFn = origShowDockerComposeLogs
	}()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}

	var receivedFollow bool
	showDockerComposeLogsFn = func(service string, tail int, follow bool) error {
		receivedFollow = follow
		return nil
	}

	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("fiso", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = RunLogs([]string{"-f"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !receivedFollow {
		t.Error("expected follow to be true")
	}
}

func TestRunLogs_WithServiceFlag(t *testing.T) {
	origLookPath := lookPathFunc
	origShowDockerComposeLogs := showDockerComposeLogsFn
	defer func() {
		lookPathFunc = origLookPath
		showDockerComposeLogsFn = origShowDockerComposeLogs
	}()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}

	var receivedService string
	showDockerComposeLogsFn = func(service string, tail int, follow bool) error {
		receivedService = service
		return nil
	}

	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("fiso", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = RunLogs([]string{"--service", "prometheus"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if receivedService != "prometheus" {
		t.Errorf("expected service 'prometheus', got: %s", receivedService)
	}
}

func TestRunLogs_AllOptions(t *testing.T) {
	origLookPath := lookPathFunc
	origShowDockerComposeLogs := showDockerComposeLogsFn
	defer func() {
		lookPathFunc = origLookPath
		showDockerComposeLogsFn = origShowDockerComposeLogs
	}()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}

	var receivedService string
	var receivedTail int
	var receivedFollow bool

	showDockerComposeLogsFn = func(service string, tail int, follow bool) error {
		receivedService = service
		receivedTail = tail
		receivedFollow = follow
		return nil
	}

	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("fiso", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = RunLogs([]string{"--tail", "200", "--follow", "--service", "fiso-link"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if receivedService != "fiso-link" {
		t.Errorf("expected service 'fiso-link', got: %s", receivedService)
	}
	if receivedTail != 200 {
		t.Errorf("expected tail 200, got: %d", receivedTail)
	}
	if !receivedFollow {
		t.Error("expected follow to be true")
	}
}

func TestRunLogs_InvalidTail(t *testing.T) {
	origLookPath := lookPathFunc
	defer func() { lookPathFunc = origLookPath }()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}

	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("fiso", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = RunLogs([]string{"--tail", "abc"})
	if err == nil {
		t.Fatal("expected error for invalid tail value")
	}
	if !strings.Contains(err.Error(), "invalid tail value") {
		t.Errorf("expected error to contain 'invalid tail value', got: %v", err)
	}
}

func TestRunLogs_UnknownFlag(t *testing.T) {
	origLookPath := lookPathFunc
	defer func() { lookPathFunc = origLookPath }()

	lookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/docker", nil
	}

	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("fiso", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = RunLogs([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected error to contain 'unknown flag', got: %v", err)
	}
}

func TestDetectEnvironment_DockerCompose(t *testing.T) {
	origLookPath := lookPathFunc
	defer func() { lookPathFunc = origLookPath }()

	lookPathFunc = func(file string) (string, error) {
		if file == "docker" {
			return "/usr/local/bin/docker", nil
		}
		return "", os.ErrNotExist
	}

	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("fiso", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("fiso/docker-compose.yml", []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	env, err := detectEnvironment()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if env != "docker-compose" {
		t.Errorf("expected 'docker-compose', got: %s", env)
	}
}

func TestDetectEnvironment_Kubernetes(t *testing.T) {
	origLookPath := lookPathFunc
	origUserHomeDir := userHomeDirFunc
	defer func() {
		lookPathFunc = origLookPath
		userHomeDirFunc = origUserHomeDir
	}()

	dir := t.TempDir()
	kubeconfigDir := dir + "/.kube"
	if err := os.MkdirAll(kubeconfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	kubeconfigPath := kubeconfigDir + "/config"
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	userHomeDirFunc = func() (string, error) {
		return dir, nil
	}
	lookPathFunc = func(file string) (string, error) {
		if file == "kubectl" {
			return "/usr/local/bin/kubectl", nil
		}
		return "", os.ErrNotExist
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Change to a directory without docker-compose.yml
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}

	env, err := detectEnvironment()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if env != "kubernetes" {
		t.Errorf("expected 'kubernetes', got: %s", env)
	}
}

func TestDetectEnvironment_UserHomeDirError(t *testing.T) {
	origLookPath := lookPathFunc
	origUserHomeDir := userHomeDirFunc
	defer func() {
		lookPathFunc = origLookPath
		userHomeDirFunc = origUserHomeDir
	}()

	lookPathFunc = func(file string) (string, error) {
		return "", os.ErrNotExist
	}
	userHomeDirFunc = func() (string, error) {
		return "", os.ErrPermission
	}

	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	_, err = detectEnvironment()
	if err == nil {
		t.Fatal("expected error when userHomeDir fails")
	}
}

func TestShowDockerComposeLogs(t *testing.T) {
	origExecCommand := execCommandFunc
	defer func() { execCommandFunc = origExecCommand }()

	var capturedArgs []string
	var capturedName string
	mockCmd := &mockExecCmd{
		runFunc: func() error {
			return nil
		},
	}

	execCommandFunc = func(ctx context.Context, name string, args ...string) execCmd {
		capturedName = name
		capturedArgs = args
		return mockCmd
	}

	err := showDockerComposeLogs("fiso-flow", 100, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedName != "docker" {
		t.Errorf("expected command 'docker', got: %s", capturedName)
	}

	expectedArgsSubset := []string{"compose", "logs", "--tail=100", "fiso-flow"}
	for _, expected := range expectedArgsSubset {
		found := false
		for _, arg := range capturedArgs {
			if arg == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected arg %q not found in: %v", expected, capturedArgs)
		}
	}
}

func TestShowDockerComposeLogs_WithFollow(t *testing.T) {
	origExecCommand := execCommandFunc
	defer func() { execCommandFunc = origExecCommand }()

	var capturedArgs []string
	mockCmd := &mockExecCmd{
		runFunc: func() error {
			return nil
		},
	}

	execCommandFunc = func(ctx context.Context, name string, args ...string) execCmd {
		capturedArgs = args
		return mockCmd
	}

	err := showDockerComposeLogs("fiso-flow", 200, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check for -f flag
	found := false
	for _, arg := range capturedArgs {
		if arg == "-f" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected -f flag in args: %v", capturedArgs)
	}
}

func TestShowKubernetesLogs(t *testing.T) {
	origExecCommand := execCommandFunc
	defer func() { execCommandFunc = origExecCommand }()

	var capturedArgs []string
	var capturedName string
	mockCmd := &mockExecCmd{
		runFunc: func() error {
			return nil
		},
	}

	execCommandFunc = func(ctx context.Context, name string, args ...string) execCmd {
		capturedName = name
		capturedArgs = args
		return mockCmd
	}

	err := showKubernetesLogs("fiso-flow", 50, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedName != "kubectl" {
		t.Errorf("expected command 'kubectl', got: %s", capturedName)
	}

	expectedArgs := []string{"logs", "--tail=50", "deployment/fiso-flow"}
	if len(capturedArgs) != len(expectedArgs) {
		t.Errorf("expected %d args, got %d: %v", len(expectedArgs), len(capturedArgs), capturedArgs)
	}
	for i, expected := range expectedArgs {
		if i >= len(capturedArgs) || capturedArgs[i] != expected {
			t.Errorf("arg %d: expected %q, got %q", i, expected, capturedArgs[i])
		}
	}
}

func TestShowKubernetesLogs_WithFollow(t *testing.T) {
	origExecCommand := execCommandFunc
	defer func() { execCommandFunc = origExecCommand }()

	var capturedArgs []string
	mockCmd := &mockExecCmd{
		runFunc: func() error {
			return nil
		},
	}

	execCommandFunc = func(ctx context.Context, name string, args ...string) execCmd {
		capturedArgs = args
		return mockCmd
	}

	err := showKubernetesLogs("prometheus", 100, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check for -f flag
	found := false
	for _, arg := range capturedArgs {
		if arg == "-f" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected -f flag in args: %v", capturedArgs)
	}
}

func TestRealExecCmd_SetStdout(t *testing.T) {
	cmd := &realExecCmd{cmd: &exec.Cmd{}}
	f := os.Stdout
	cmd.SetStdout(f)
	if cmd.cmd.Stdout != f {
		t.Error("SetStdout did not set Stdout correctly")
	}
}

func TestRealExecCmd_SetStderr(t *testing.T) {
	cmd := &realExecCmd{cmd: &exec.Cmd{}}
	f := os.Stderr
	cmd.SetStderr(f)
	if cmd.cmd.Stderr != f {
		t.Error("SetStderr did not set Stderr correctly")
	}
}

func TestRealExecCmd_SetStdin(t *testing.T) {
	cmd := &realExecCmd{cmd: &exec.Cmd{}}
	f := os.Stdin
	cmd.SetStdin(f)
	if cmd.cmd.Stdin != f {
		t.Error("SetStdin did not set Stdin correctly")
	}
}

func TestRealExecCmd_Run(t *testing.T) {
	// Create a simple command that will succeed
	realCmd := exec.Command("echo", "test")
	cmd := &realExecCmd{cmd: realCmd}
	err := cmd.Run()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestRunLogs_ServiceFlagMissingValue(t *testing.T) {
	err := RunLogs([]string{"--service"})
	if err == nil {
		t.Fatal("expected error for --service without value")
	}
	if !strings.Contains(err.Error(), "--service requires a value") {
		t.Errorf("expected error about missing value, got: %v", err)
	}
}

func TestRunLogs_TailFlagMissingValue(t *testing.T) {
	err := RunLogs([]string{"--tail"})
	if err == nil {
		t.Fatal("expected error for --tail without value")
	}
	if !strings.Contains(err.Error(), "--tail requires a value") {
		t.Errorf("expected error about missing value, got: %v", err)
	}
}

func TestRunLogs_KubernetesEnv(t *testing.T) {
	origLookPath := lookPathFunc
	origUserHomeDir := userHomeDirFunc
	origShowKubernetesLogs := showKubernetesLogsFn
	defer func() {
		lookPathFunc = origLookPath
		userHomeDirFunc = origUserHomeDir
		showKubernetesLogsFn = origShowKubernetesLogs
	}()

	dir := t.TempDir()
	kubeconfigDir := dir + "/.kube"
	if err := os.MkdirAll(kubeconfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	kubeconfigPath := kubeconfigDir + "/config"
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	userHomeDirFunc = func() (string, error) {
		return dir, nil
	}
	lookPathFunc = func(file string) (string, error) {
		if file == "kubectl" {
			return "/usr/local/bin/kubectl", nil
		}
		return "", os.ErrNotExist
	}

	var receivedService string
	var receivedTail int
	var receivedFollow bool

	showKubernetesLogsFn = func(service string, tail int, follow bool) error {
		receivedService = service
		receivedTail = tail
		receivedFollow = follow
		return nil
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}

	err = RunLogs([]string{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if receivedService != "fiso-flow" {
		t.Errorf("expected service 'fiso-flow', got: %s", receivedService)
	}
	if receivedTail != 100 {
		t.Errorf("expected tail 100, got: %d", receivedTail)
	}
	if receivedFollow {
		t.Error("expected follow to be false by default")
	}
}

// mockExecCmd is a mock implementation of execCmd for testing
type mockExecCmd struct {
	runFunc       func() error
	setStdoutFunc func(w *os.File)
	setStderrFunc func(w *os.File)
	setStdinFunc  func(r *os.File)
}

func (m *mockExecCmd) Run() error {
	if m.runFunc != nil {
		return m.runFunc()
	}
	return nil
}

func (m *mockExecCmd) SetStdout(w *os.File) {
	if m.setStdoutFunc != nil {
		m.setStdoutFunc(w)
	}
}

func (m *mockExecCmd) SetStderr(w *os.File) {
	if m.setStderrFunc != nil {
		m.setStderrFunc(w)
	}
}

func (m *mockExecCmd) SetStdin(r *os.File) {
	if m.setStdinFunc != nil {
		m.setStdinFunc(r)
	}
}
