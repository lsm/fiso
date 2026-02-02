package cli

import (
	"fmt"
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

func TestHasFlag(t *testing.T) {
	if !hasFlag([]string{"--docker"}, "--docker") {
		t.Error("should find --docker flag")
	}
	if hasFlag([]string{"-h"}, "--docker") {
		t.Error("should not find --docker flag")
	}
	if hasFlag(nil, "--docker") {
		t.Error("should not find flag in nil slice")
	}
}

// Hybrid mode (default): no Dockerfiles → override with extra_hosts + ports only.
func TestWriteDevOverride_Hybrid(t *testing.T) {
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

	if err := writeDevOverride(false); err != nil {
		t.Fatalf("writeDevOverride: %v", err)
	}

	data, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("override file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "extra_hosts") {
		t.Error("hybrid override should contain extra_hosts")
	}
	if !strings.Contains(content, "host-gateway") {
		t.Error("hybrid override should contain host-gateway")
	}
	if !strings.Contains(content, "3500:3500") {
		t.Error("hybrid override should expose fiso-link port 3500")
	}
	if strings.Contains(content, "Dockerfile") {
		t.Error("hybrid override should not contain build directives")
	}
}

// Docker mode: no Dockerfiles → no override file needed.
func TestWriteDevOverride_Docker(t *testing.T) {
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

	if err := writeDevOverride(true); err != nil {
		t.Fatalf("writeDevOverride: %v", err)
	}

	if _, err := os.Stat(overridePath); !os.IsNotExist(err) {
		t.Error("override file should not exist in docker mode without Dockerfiles")
	}
}

// Maintainer + hybrid: Dockerfiles present, hybrid mode → build directives + extra_hosts + ports.
func TestWriteDevOverride_MaintainerHybrid(t *testing.T) {
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
	if err := os.WriteFile("Dockerfile.link", []byte("FROM scratch\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeDevOverride(false); err != nil {
		t.Fatalf("writeDevOverride: %v", err)
	}

	data, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("override file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Dockerfile.flow") {
		t.Error("override should reference Dockerfile.flow")
	}
	if !strings.Contains(content, "Dockerfile.link") {
		t.Error("override should reference Dockerfile.link")
	}
	if !strings.Contains(content, "extra_hosts") {
		t.Error("override should contain extra_hosts in hybrid mode")
	}
	if !strings.Contains(content, "3500:3500") {
		t.Error("override should expose fiso-link port in hybrid mode")
	}
}

// Maintainer + docker: Dockerfiles present, docker mode → build directives only.
func TestWriteDevOverride_MaintainerDocker(t *testing.T) {
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
	if err := os.WriteFile("Dockerfile.link", []byte("FROM scratch\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeDevOverride(true); err != nil {
		t.Fatalf("writeDevOverride: %v", err)
	}

	data, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("override file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Dockerfile.flow") {
		t.Error("override should reference Dockerfile.flow")
	}
	if !strings.Contains(content, "Dockerfile.link") {
		t.Error("override should reference Dockerfile.link")
	}
	if strings.Contains(content, "extra_hosts") {
		t.Error("override should not contain extra_hosts in docker mode")
	}
	if strings.Contains(content, "3500:3500") {
		t.Error("override should not expose ports in docker mode")
	}
}

// Maintainer + hybrid with only Dockerfile.flow.
func TestWriteDevOverride_FlowOnlyHybrid(t *testing.T) {
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

	if err := writeDevOverride(false); err != nil {
		t.Fatalf("writeDevOverride: %v", err)
	}

	data, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatalf("override file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Dockerfile.flow") {
		t.Error("override should reference Dockerfile.flow")
	}
	if !strings.Contains(content, "extra_hosts") {
		t.Error("override should contain extra_hosts for fiso-flow")
	}
	// fiso-link should still get ports exposed (no Dockerfile.link, but hybrid mode)
	if !strings.Contains(content, "3500:3500") {
		t.Error("override should expose fiso-link port in hybrid mode")
	}
}

func TestPrintHybridBanner(t *testing.T) {
	// Exercise the function to cover it; output goes to stdout.
	printHybridBanner()
}

func TestPrintDockerBanner(t *testing.T) {
	printDockerBanner()
}

func TestPrintGHCRHint_Matching(t *testing.T) {
	printGHCRHint(fmt.Errorf("Head \"https://ghcr.io/v2/lsm/fiso-flow/manifests/latest\": denied: denied"))
}

func TestPrintGHCRHint_NonMatching(t *testing.T) {
	// Should not panic or print anything for unrelated errors.
	printGHCRHint(fmt.Errorf("connection refused"))
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
