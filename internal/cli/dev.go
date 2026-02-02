package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

const overridePath = "fiso/docker-compose.override.yml"

// RunDev starts the local Fiso development environment.
func RunDev(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("Usage: fiso dev\n\nStarts the local Fiso development environment using Docker Compose.\nRequires Docker to be installed.")
		return nil
	}

	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found in PATH: install Docker Desktop or Docker Engine")
	}

	// Verify fiso/ directory exists (created by fiso init)
	if _, err := os.Stat("fiso/docker-compose.yml"); err != nil {
		return fmt.Errorf("fiso/docker-compose.yml not found: run 'fiso init' first")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Maintainer mode: generate override to build from local Dockerfiles
	if err := writeDevOverride(); err != nil {
		return fmt.Errorf("write dev override: %w", err)
	}

	upArgs := []string{"up", "--remove-orphans"}
	if fileExists("Dockerfile.flow") || fileExists("Dockerfile.link") {
		upArgs = append(upArgs, "--build")
	}

	fmt.Println("Starting Fiso development environment...")
	fmt.Println("")
	fmt.Println("Once services are up, send a test event:")
	fmt.Println("")
	fmt.Println(`  curl -X POST http://localhost:8081/ingest \`)
	fmt.Println(`    -H "Content-Type: application/json" \`)
	fmt.Println(`    -d '{"order_id": "12345", "amount": 99.99}'`)
	fmt.Println("")
	fmt.Println("Flow: curl → fiso-flow(:8081) → user-service → fiso-link → external-api")
	fmt.Println("")

	upDone := make(chan error, 1)
	go func() {
		upDone <- runCompose(ctx, upArgs...)
	}()

	select {
	case err := <-upDone:
		composeDown()
		return err
	case <-ctx.Done():
		fmt.Println("\nShutting down...")
		composeDown()
		return nil
	}
}

func buildComposeFileArgs() []string {
	args := []string{"compose", "-f", "fiso/docker-compose.yml"}
	if _, err := os.Stat(overridePath); err == nil {
		args = append(args, "-f", overridePath)
	}
	return args
}

func runCompose(ctx context.Context, args ...string) error {
	fileArgs := buildComposeFileArgs()
	fullArgs := append(append([]string{}, fileArgs...), args...)
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func composeDown() {
	fileArgs := buildComposeFileArgs()
	fullArgs := append(append([]string{}, fileArgs...), "down", "--remove-orphans")
	cmd := exec.Command("docker", fullArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	_ = os.Remove(overridePath)
}

// writeDevOverride generates a docker-compose override file when developing
// fiso itself (Dockerfile.flow or Dockerfile.link present in CWD). This
// replaces the GHCR image references with local builds.
func writeDevOverride() error {
	hasFlow := fileExists("Dockerfile.flow")
	hasLink := fileExists("Dockerfile.link")

	if !hasFlow && !hasLink {
		_ = os.Remove(overridePath)
		return nil
	}

	content := "services:\n"
	if hasFlow {
		content += `  fiso-flow:
    image: fiso-flow:dev
    build:
      context: ..
      dockerfile: Dockerfile.flow
`
	}
	if hasLink {
		content += `  fiso-link:
    image: fiso-link:dev
    build:
      context: ..
      dockerfile: Dockerfile.link
`
	}

	return os.WriteFile(overridePath, []byte(content), 0644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
