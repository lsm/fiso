package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInit(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := RunInit(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Infrastructure goes under fiso/
	assertFileExists(t, filepath.Join(dir, "fiso", "docker-compose.yml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "prometheus.yml"))

	// Default flow: example-flow.yaml (http → http)
	assertFileExists(t, filepath.Join(dir, "fiso", "flows", "example-flow.yaml"))

	// Link config under fiso/link/
	assertFileExists(t, filepath.Join(dir, "fiso", "link", "config.yaml"))

	// User service under fiso/user-service/ (default, no temporal)
	assertFileExists(t, filepath.Join(dir, "fiso", "user-service", "main.go"))
	assertFileExists(t, filepath.Join(dir, "fiso", "user-service", "Dockerfile"))
	assertFileExists(t, filepath.Join(dir, "fiso", "user-service", "go.mod"))

	// Gitignore at project root
	assertFileExists(t, filepath.Join(dir, ".gitignore"))

	data, err := os.ReadFile(filepath.Join(dir, "fiso", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "fiso-flow") {
		t.Error("docker-compose.yml should reference fiso-flow")
	}
	if !strings.Contains(content, "fiso-link") {
		t.Error("docker-compose.yml should reference fiso-link")
	}
	if !strings.Contains(content, "user-service") {
		t.Error("docker-compose.yml should reference user-service")
	}
	if !strings.Contains(content, "external-api") {
		t.Error("docker-compose.yml should reference external-api")
	}
	if !strings.Contains(content, "./flows") {
		t.Error("docker-compose.yml should mount ./flows (relative to fiso/ dir)")
	}
	// Default should NOT include kafka or temporal
	if strings.Contains(content, "kafka:") {
		t.Error("default docker-compose.yml should not include kafka")
	}
	if strings.Contains(content, "temporal:") {
		t.Error("default docker-compose.yml should not include temporal")
	}
}

func TestRunInit_Help(t *testing.T) {
	if err := RunInit([]string{"-h"}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInit_DefaultsFlag(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := RunInit([]string{"--defaults"}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertFileExists(t, filepath.Join(dir, "fiso", "flows", "example-flow.yaml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "user-service", "main.go"))
}

func TestRunInit_SampleFlowContent(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := RunInit(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "fiso", "flows", "example-flow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "name: example-flow") {
		t.Error("sample flow should contain name: example-flow")
	}
	if !strings.Contains(content, "type: http") {
		t.Error("sample flow should contain type: http")
	}
}

func TestRunInit_LinkConfigContent(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := RunInit(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "fiso", "link", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "listenAddr") {
		t.Error("link config should contain listenAddr")
	}
	if !strings.Contains(content, "echo") {
		t.Error("link config should contain echo target")
	}
}

func TestRunInit_UserServiceContent(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := RunInit(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "fiso", "user-service", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "package main") {
		t.Error("user-service main.go should contain package main")
	}

	data, err = os.ReadFile(filepath.Join(dir, "fiso", "user-service", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "module user-service") {
		t.Error("user-service go.mod should contain module user-service")
	}
}

func TestRunInit_SkipsExistingGitignore(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("custom\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := RunInit(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "custom\n" {
		t.Error("existing .gitignore should not be overwritten")
	}
}

// --- Interactive prompt tests ---

func TestRunInit_InteractiveKafkaTemporalMapping(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Kafka(2), Temporal(2), Mapping(3), CloudEvents=yes(y)
	input := strings.NewReader("2\n2\n3\ny\n")
	if err := RunInit(nil, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify kafka-temporal-flow.yaml exists
	assertFileExists(t, filepath.Join(dir, "fiso", "flows", "kafka-temporal-flow.yaml"))

	// Verify docker-compose includes kafka and temporal
	dc, err := os.ReadFile(filepath.Join(dir, "fiso", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(dc)
	if !strings.Contains(content, "kafka") {
		t.Error("docker-compose should include kafka")
	}
	if !strings.Contains(content, "temporal") {
		t.Error("docker-compose should include temporal")
	}

	// Verify temporal-worker scaffold
	assertFileExists(t, filepath.Join(dir, "fiso", "temporal-worker", "main.go"))
	assertFileExists(t, filepath.Join(dir, "fiso", "temporal-worker", "workflow.go"))
	assertFileExists(t, filepath.Join(dir, "fiso", "temporal-worker", "activity.go"))
	assertFileExists(t, filepath.Join(dir, "fiso", "temporal-worker", "Dockerfile"))
	assertFileExists(t, filepath.Join(dir, "fiso", "temporal-worker", "go.mod"))

	// Verify no user-service directory
	if _, err := os.Stat(filepath.Join(dir, "fiso", "user-service")); !os.IsNotExist(err) {
		t.Error("user-service should not exist when temporal is selected")
	}

	// Verify flow content
	flow, err := os.ReadFile(filepath.Join(dir, "fiso", "flows", "kafka-temporal-flow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	flowContent := string(flow)
	if !strings.Contains(flowContent, "type: kafka") {
		t.Error("flow should contain kafka source")
	}
	if !strings.Contains(flowContent, "type: temporal") {
		t.Error("flow should contain temporal sink")
	}
	if !strings.Contains(flowContent, "mapping:") {
		t.Error("flow should contain mapping transform")
	}
	if !strings.Contains(flowContent, "cloudevents:") {
		t.Error("flow should contain cloudevents section")
	}
}

func TestRunInit_InteractiveHTTPDefaults(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// All defaults (Enter for each prompt)
	input := strings.NewReader("\n\n\n\n")
	if err := RunInit(nil, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should produce same result as --defaults
	assertFileExists(t, filepath.Join(dir, "fiso", "flows", "example-flow.yaml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "user-service", "main.go"))

	dc, err := os.ReadFile(filepath.Join(dir, "fiso", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(dc), "kafka:") {
		t.Error("default docker-compose should not include kafka")
	}
}

func TestRunInit_InteractiveKafkaHTTPCEL(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Kafka(2), HTTP(1), CEL(2), no CloudEvents(n)
	input := strings.NewReader("2\n1\n2\nn\n")
	if err := RunInit(nil, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertFileExists(t, filepath.Join(dir, "fiso", "flows", "kafka-http-flow.yaml"))

	flow, err := os.ReadFile(filepath.Join(dir, "fiso", "flows", "kafka-http-flow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	flowContent := string(flow)
	if !strings.Contains(flowContent, "type: kafka") {
		t.Error("flow should contain kafka source")
	}
	if !strings.Contains(flowContent, "type: http") {
		t.Error("flow should contain http sink")
	}
	if !strings.Contains(flowContent, "cel:") {
		t.Error("flow should contain cel transform")
	}
	if strings.Contains(flowContent, "cloudevents:") {
		t.Error("flow should NOT contain cloudevents section")
	}

	// Kafka should be in docker-compose
	dc, err := os.ReadFile(filepath.Join(dir, "fiso", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dc), "kafka") {
		t.Error("docker-compose should include kafka")
	}
	// But not temporal
	if strings.Contains(string(dc), "temporal") {
		t.Error("docker-compose should NOT include temporal")
	}
	// user-service should exist (not temporal-worker)
	assertFileExists(t, filepath.Join(dir, "fiso", "user-service", "main.go"))
}

func TestRunInit_InteractiveHTTPTemporal(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// HTTP(1), Temporal(2), None(1), no CloudEvents(n)
	input := strings.NewReader("1\n2\n1\nn\n")
	if err := RunInit(nil, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertFileExists(t, filepath.Join(dir, "fiso", "flows", "http-temporal-flow.yaml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "temporal-worker", "main.go"))

	dc, err := os.ReadFile(filepath.Join(dir, "fiso", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(dc)
	if strings.Contains(content, "kafka") {
		t.Error("docker-compose should NOT include kafka")
	}
	if !strings.Contains(content, "temporal") {
		t.Error("docker-compose should include temporal")
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file %s to exist", path)
	}
}
