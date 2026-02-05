package cli

import (
	"os"
	"path/filepath"
	"runtime"
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

// --- Kubernetes manifest tests ---

func TestRunInit_WithK8sManifests(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// All defaults + K8s=yes: HTTP(1), HTTP(1), None(1), no CE(n), K8s=yes(y)
	input := strings.NewReader("\n\n\n\ny\n")
	if err := RunInit(nil, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify K8s deploy files exist
	assertFileExists(t, filepath.Join(dir, "fiso", "deploy", "kustomization.yaml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "deploy", "namespace.yaml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "deploy", "flow-deployment.yaml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "deploy", "link-deployment.yaml"))

	// kustomization.yaml should reference the correct flow file
	data, err := os.ReadFile(filepath.Join(dir, "fiso", "deploy", "kustomization.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	kustomize := string(data)
	if !strings.Contains(kustomize, "../flows/example-flow.yaml") {
		t.Error("kustomization.yaml should reference ../flows/example-flow.yaml")
	}
	if !strings.Contains(kustomize, "../link/config.yaml") {
		t.Error("kustomization.yaml should reference ../link/config.yaml")
	}
	if !strings.Contains(kustomize, "configMapGenerator") {
		t.Error("kustomization.yaml should contain configMapGenerator")
	}

	// flow-deployment should contain Service (HTTP source)
	flowDeploy, err := os.ReadFile(filepath.Join(dir, "fiso", "deploy", "flow-deployment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(flowDeploy), "kind: Service") {
		t.Error("flow-deployment.yaml should contain Service for HTTP source")
	}
	if !strings.Contains(string(flowDeploy), "containerPort: 8081") {
		t.Error("flow-deployment.yaml should expose ingest port 8081")
	}

	// Docker Compose should still exist (K8s is additive)
	assertFileExists(t, filepath.Join(dir, "fiso", "docker-compose.yml"))
}

func TestRunInit_WithK8sKafkaSource(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Kafka(2), HTTP(1), None(1), no CE(n), K8s=yes(y)
	input := strings.NewReader("2\n1\n1\nn\ny\n")
	if err := RunInit(nil, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// kustomization.yaml should reference kafka-http-flow
	data, err := os.ReadFile(filepath.Join(dir, "fiso", "deploy", "kustomization.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "../flows/kafka-http-flow.yaml") {
		t.Error("kustomization.yaml should reference ../flows/kafka-http-flow.yaml")
	}

	// flow-deployment should NOT contain Service (Kafka source, no HTTP ingress)
	flowDeploy, err := os.ReadFile(filepath.Join(dir, "fiso", "deploy", "flow-deployment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(flowDeploy), "kind: Service") {
		t.Error("flow-deployment.yaml should NOT contain Service for Kafka source")
	}
}

func TestRunInit_DefaultsNoK8s(t *testing.T) {
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

	// deploy/ should NOT exist with --defaults
	if _, err := os.Stat(filepath.Join(dir, "fiso", "deploy")); !os.IsNotExist(err) {
		t.Error("fiso/deploy/ should not exist with --defaults")
	}
}

func TestRunInit_KafkaSource(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Kafka(2), HTTP(1), None(1), no CloudEvents(n), no K8s(n)
	input := strings.NewReader("2\n1\n1\nn\nn\n")
	if err := RunInit(nil, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify kafka-http-flow.yaml is generated
	assertFileExists(t, filepath.Join(dir, "fiso", "flows", "kafka-http-flow.yaml"))

	flow, err := os.ReadFile(filepath.Join(dir, "fiso", "flows", "kafka-http-flow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(flow), "type: kafka") {
		t.Error("flow should contain kafka source")
	}
}

func TestRunInit_TemporalSink(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// HTTP(1), Temporal(2), None(1), no CloudEvents(n), no K8s(n)
	input := strings.NewReader("1\n2\n1\nn\nn\n")
	if err := RunInit(nil, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify temporal-worker directory is created
	assertFileExists(t, filepath.Join(dir, "fiso", "temporal-worker", "main.go"))
	assertFileExists(t, filepath.Join(dir, "fiso", "temporal-worker", "workflow.go"))
	assertFileExists(t, filepath.Join(dir, "fiso", "temporal-worker", "activity.go"))
}

func TestRunInit_KubernetesDeployment(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// HTTP(1), HTTP(1), None(1), no CloudEvents(n), K8s=yes(y)
	input := strings.NewReader("1\n1\n1\nn\ny\n")
	if err := RunInit(nil, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify deploy/ manifests exist
	assertFileExists(t, filepath.Join(dir, "fiso", "deploy", "kustomization.yaml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "deploy", "namespace.yaml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "deploy", "flow-deployment.yaml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "deploy", "link-deployment.yaml"))
}

func TestRunInit_AllCombinations(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Kafka(2), Temporal(2), None(1), no CloudEvents(n), K8s=yes(y)
	input := strings.NewReader("2\n2\n1\nn\ny\n")
	if err := RunInit(nil, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all expected files
	assertFileExists(t, filepath.Join(dir, "fiso", "flows", "kafka-temporal-flow.yaml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "temporal-worker", "main.go"))
	assertFileExists(t, filepath.Join(dir, "fiso", "deploy", "kustomization.yaml"))

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
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file %s to exist", path)
	}
}

func TestScaffold_DirectoryCreationError(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create a regular file named "fiso" so MkdirAll("fiso/...") will fail
	if err := os.WriteFile("fiso", []byte("blocking file"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := initConfig{
		ProjectName: "fiso",
		Source:      "http",
		Sink:        "http",
		Transform:   "none",
	}

	err = scaffold(cfg)
	if err == nil {
		t.Fatal("expected error when creating directory over existing file")
	}
	if !strings.Contains(err.Error(), "create directory") {
		t.Errorf("expected error about creating directory, got: %v", err)
	}
}

func TestWriteTemplate_Error(t *testing.T) {
	dir := t.TempDir()

	cfg := initConfig{
		ProjectName: "test",
		Source:      "http",
		Sink:        "http",
	}

	err := writeTemplate(dir, "test.yml", "templates/nonexistent-template.yml", cfg)
	if err == nil {
		t.Fatal("expected error for non-existent template")
	}
	if !strings.Contains(err.Error(), "read template") {
		t.Errorf("expected error about reading template, got: %v", err)
	}
}

func TestWriteTemplate_WriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory permissions not enforced on Windows")
	}

	dir := t.TempDir()

	// Create a read-only directory to force write error
	readOnlyDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0444); err != nil {
		t.Fatal(err)
	}

	cfg := initConfig{
		ProjectName: "test",
		Source:      "http",
		Sink:        "http",
	}

	// Try to write to read-only directory
	err := writeTemplate(readOnlyDir, "test.yml", "templates/prometheus.yml", cfg)
	if err == nil {
		t.Fatal("expected error for write to read-only directory")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("expected error about writing file, got: %v", err)
	}
}

func TestCopyEmbedded_WriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory permissions not enforced on Windows")
	}

	dir := t.TempDir()

	// Create a read-only directory to force write error
	readOnlyDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0444); err != nil {
		t.Fatal(err)
	}

	// Try to copy to read-only directory
	err := copyEmbedded(readOnlyDir, "config.yaml", "templates/link-config.yaml")
	if err == nil {
		t.Fatal("expected error for write to read-only directory")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("expected error about writing file, got: %v", err)
	}
}

func TestShouldPrompt_WithStdinProvided(t *testing.T) {
	// When stdin is explicitly provided, shouldPrompt returns true
	stdin := strings.NewReader("test")
	if !shouldPrompt([]string{}, stdin) {
		t.Error("expected shouldPrompt to return true when stdin is provided")
	}
}

func TestShouldPrompt_WithDefaultsFlag(t *testing.T) {
	// With --defaults flag, shouldPrompt returns false
	if shouldPrompt([]string{"--defaults"}, strings.NewReader("test")) {
		t.Error("expected shouldPrompt to return false with --defaults flag")
	}
}

func TestShouldPrompt_NilStdin(t *testing.T) {
	// With nil stdin, shouldPrompt checks if real stdin is a terminal
	// This will vary depending on test environment, so we just verify it doesn't panic
	_ = shouldPrompt([]string{}, nil)
}

func TestCopyEmbedded_Error(t *testing.T) {
	dir := t.TempDir()

	err := copyEmbedded(dir, "test.txt", "templates/invalid/path/file.txt")
	if err == nil {
		t.Fatal("expected error for non-existent embedded path")
	}
	if !strings.Contains(err.Error(), "read embedded") {
		t.Errorf("expected error about reading embedded file, got: %v", err)
	}
}

func TestDeriveFlowName_AllCombinations(t *testing.T) {
	tests := []struct {
		source   string
		sink     string
		expected string
	}{
		{"http", "http", "example-flow"},
		{"http", "temporal", "http-temporal-flow"},
		{"kafka", "http", "kafka-http-flow"},
		{"kafka", "temporal", "kafka-temporal-flow"},
	}

	for _, tt := range tests {
		t.Run(tt.source+"-"+tt.sink, func(t *testing.T) {
			got := deriveFlowName(tt.source, tt.sink)
			if got != tt.expected {
				t.Errorf("deriveFlowName(%q, %q) = %q, want %q", tt.source, tt.sink, got, tt.expected)
			}
		})
	}
}
