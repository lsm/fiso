package cli

import (
	"os"
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
