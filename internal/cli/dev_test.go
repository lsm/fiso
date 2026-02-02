package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDev_Help(t *testing.T) {
	if err := RunDev([]string{"-h"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteDevOverride_WithDockerfiles(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create fiso/ dir and both Dockerfiles
	if err := os.MkdirAll("fiso", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("Dockerfile.flow", []byte("FROM scratch\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("Dockerfile.link", []byte("FROM scratch\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeDevOverride(); err != nil {
		t.Fatalf("writeDevOverride: %v", err)
	}

	data, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("override file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "fiso-flow") {
		t.Error("override should contain fiso-flow service")
	}
	if !strings.Contains(content, "fiso-link") {
		t.Error("override should contain fiso-link service")
	}
	if !strings.Contains(content, "Dockerfile.flow") {
		t.Error("override should reference Dockerfile.flow")
	}
	if !strings.Contains(content, "Dockerfile.link") {
		t.Error("override should reference Dockerfile.link")
	}
}

func TestWriteDevOverride_NoDockerfiles(t *testing.T) {
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

	if err := writeDevOverride(); err != nil {
		t.Fatalf("writeDevOverride: %v", err)
	}

	if _, err := os.Stat(overridePath); !os.IsNotExist(err) {
		t.Error("override file should not exist when no Dockerfiles present")
	}
}

func TestWriteDevOverride_FlowOnly(t *testing.T) {
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
	if err := os.WriteFile("Dockerfile.flow", []byte("FROM scratch\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeDevOverride(); err != nil {
		t.Fatalf("writeDevOverride: %v", err)
	}

	data, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("override file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "fiso-flow") {
		t.Error("override should contain fiso-flow service")
	}
	if strings.Contains(content, "fiso-link") {
		t.Error("override should not contain fiso-link when only Dockerfile.flow exists")
	}
}

func TestBuildComposeFileArgs(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	args := buildComposeFileArgs()
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}
	if args[0] != "compose" || args[1] != "-f" || args[2] != "fiso/docker-compose.yml" {
		t.Errorf("unexpected args: %v", args)
	}

	// Create override file and verify it's included
	if err := os.MkdirAll("fiso", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("fiso", "docker-compose.override.yml"), []byte("services:\n"), 0644); err != nil {
		t.Fatal(err)
	}

	args = buildComposeFileArgs()
	if len(args) != 5 {
		t.Fatalf("expected 5 args with override, got %d: %v", len(args), args)
	}
	if args[3] != "-f" || args[4] != overridePath {
		t.Errorf("expected override file args, got: %v", args[3:])
	}
}
