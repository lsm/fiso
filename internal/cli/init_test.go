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

	// Gitignore at project root
	assertFileExists(t, filepath.Join(dir, ".gitignore"))

	data, err := os.ReadFile(filepath.Join(dir, "fiso", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "apache/kafka:4.0.0") {
		t.Error("docker-compose.yml should reference apache/kafka:4.0.0")
	}
	if !strings.Contains(content, "fiso-flow") {
		t.Error("docker-compose.yml should reference fiso-flow")
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
	if !strings.Contains(content, "type: kafka") {
		t.Error("sample flow should contain type: kafka")
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
