//go:build !nowasmer

package wasm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	if factory == nil {
		t.Fatal("NewFactory returned nil")
	}
}

func TestDefaultFactory_Create_MissingModule(t *testing.T) {
	factory := NewFactory()
	ctx := context.Background()

	cfg := Config{
		Type:       RuntimeWazero,
		ModulePath: "/nonexistent/path/to/module.wasm",
	}

	_, err := factory.Create(ctx, cfg)
	if err == nil {
		t.Fatal("expected error for missing module")
	}
}

func TestDefaultFactory_Create_UnknownRuntimeType(t *testing.T) {
	factory := NewFactory()
	ctx := context.Background()

	// Create a temp file to satisfy ModulePath check
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "module.wasm")
	if err := os.WriteFile(tmpFile, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	cfg := Config{
		Type:       RuntimeType("unknown"),
		ModulePath: tmpFile,
	}

	_, err := factory.Create(ctx, cfg)
	if err == nil {
		t.Fatal("expected error for unknown runtime type")
	}
}

func TestDefaultFactory_CreateApp_RequiresWasmer(t *testing.T) {
	factory := NewFactory()
	ctx := context.Background()

	// Create a temp file to satisfy ModulePath check
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "module.wasm")
	if err := os.WriteFile(tmpFile, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	cfg := Config{
		Type:       RuntimeWazero,
		ModulePath: tmpFile,
	}

	_, err := factory.CreateApp(ctx, cfg)
	if err == nil {
		t.Fatal("expected error when CreateApp called with non-wasmer runtime")
	}
}

func TestDefaultFactory_CreateApp_MissingModule(t *testing.T) {
	factory := NewFactory()
	ctx := context.Background()

	cfg := Config{
		Type:       RuntimeWasmer,
		ModulePath: "/nonexistent/path/to/module.wasm",
	}

	_, err := factory.CreateApp(ctx, cfg)
	if err == nil {
		t.Fatal("expected error for missing module")
	}
}

func TestDefaultFactory_Create_EmptyRuntimeType(t *testing.T) {
	// Verify that empty runtime type defaults to wazero
	factory := NewFactory()
	ctx := context.Background()

	// Create a temp file with invalid wasm to get past the file read
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "module.wasm")
	if err := os.WriteFile(tmpFile, []byte("invalid wasm"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	cfg := Config{
		Type:       "", // Empty should default to wazero
		ModulePath: tmpFile,
	}

	// Should try to create wazero runtime (will fail on invalid wasm but that's expected)
	_, err := factory.Create(ctx, cfg)
	// The error should be from wazero, not from unknown runtime type
	if err == nil {
		t.Fatal("expected error for invalid wasm")
	}
	// Verify it's not an "unknown runtime type" error
	if err.Error() == "unknown runtime type: " {
		t.Error("empty runtime type should default to wazero, not return unknown type error")
	}
}

func TestFactory_Interface(t *testing.T) {
	// Verify DefaultFactory implements Factory interface
	var _ Factory = NewFactory()
}

func TestDefaultFactory_Create_ValidWazero(t *testing.T) {
	factory := NewFactory()
	ctx := context.Background()

	// Build a valid WASM module for testing
	wasmPath := buildTestWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	// Write to a temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "module.wasm")
	if err := os.WriteFile(tmpFile, wasmBytes, 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cfg := Config{
		Type:       RuntimeWazero,
		ModulePath: tmpFile,
	}

	rt, err := factory.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() { _ = rt.Close() }()

	if rt.Type() != RuntimeWazero {
		t.Errorf("Type() = %q, want %q", rt.Type(), RuntimeWazero)
	}
}

func TestDefaultFactory_Create_WithConfigOptions(t *testing.T) {
	factory := NewFactory()
	ctx := context.Background()

	// Build a valid WASM module for testing
	wasmPath := buildTestWASMModule(t, filepath.Join("../interceptor/wasm/testdata", "enrich"))
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	// Write to a temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "module.wasm")
	if err := os.WriteFile(tmpFile, wasmBytes, 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cfg := Config{
		Type:        RuntimeWazero,
		ModulePath:  tmpFile,
		ModuleType:  ModuleTypeTransform,
		Execution:   ExecutionPerRequest,
		MemoryLimit: 64 * 1024 * 1024,
		Timeout:     30 * time.Second,
		Env:         map[string]string{"FOO": "bar"},
	}

	rt, err := factory.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() { _ = rt.Close() }()

	if rt.Type() != RuntimeWazero {
		t.Errorf("Type() = %q, want %q", rt.Type(), RuntimeWazero)
	}
}

// buildTestWASMModule is a helper that compiles a Go source file to a WASM binary.
func buildTestWASMModule(t *testing.T, srcDir string) string {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "module.wasm")
	cmd := exec.Command("go", "build", "-o", outPath, ".")
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compile wasm module: %v\n%s", err, out)
	}
	return outPath
}
