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

	if err := RunInit(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Infrastructure goes under fiso/
	assertFileExists(t, filepath.Join(dir, "fiso", "docker-compose.yml"))
	assertFileExists(t, filepath.Join(dir, "fiso", "prometheus.yml"))

	// Flows under fiso/
	assertFileExists(t, filepath.Join(dir, "fiso", "flows", "example-flow.yaml"))

	// Link config under fiso/link/
	assertFileExists(t, filepath.Join(dir, "fiso", "link", "config.yaml"))

	// User service under fiso/user-service/
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
}

func TestRunInit_Help(t *testing.T) {
	if err := RunInit([]string{"-h"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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

	if err := RunInit(nil); err != nil {
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

	if err := RunInit(nil); err != nil {
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

	if err := RunInit(nil); err != nil {
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

	// Pre-create a .gitignore
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("custom\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := RunInit(nil); err != nil {
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

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file %s to exist", path)
	}
}
