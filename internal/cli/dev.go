package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

const overridePath = "fiso/docker-compose.override.yml"

// RunDev starts the local Fiso development environment.
func RunDev(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println(`Usage: fiso dev [--docker]

Starts the local Fiso development environment using Docker Compose.
Requires Docker to be installed.

By default, runs in hybrid mode: Fiso services (fiso-flow, fiso-link,
external-api) run in Docker while your service runs on the host for
fast iteration.

Flags:
  --docker    Run all services in Docker, including user-service`)
		return nil
	}

	dockerMode := hasFlag(args, "--docker")

	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found in PATH: install Docker Desktop or Docker Engine")
	}

	// Verify fiso/ directory exists (created by fiso init)
	if _, err := os.Stat("fiso/docker-compose.yml"); err != nil {
		return fmt.Errorf("fiso/docker-compose.yml not found: run 'fiso init' first")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := writeDevOverride(dockerMode); err != nil {
		return fmt.Errorf("write dev override: %w", err)
	}

	upArgs := []string{"up", "--remove-orphans"}
	if fileExists("Dockerfile.flow") || fileExists("Dockerfile.link") {
		upArgs = append(upArgs, "--build")
	}

	if dockerMode {
		printDockerBanner()
	} else {
		printHybridBanner()
	}

	upDone := make(chan error, 1)
	go func() {
		upDone <- runCompose(ctx, upArgs...)
	}()

	select {
	case err := <-upDone:
		composeDown(dockerMode)
		if err != nil {
			printGHCRHint(err)
		}
		return err
	case <-ctx.Done():
		fmt.Println("\nShutting down...")
		composeDown(dockerMode)
		return nil
	}
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func printHybridBanner() {
	fmt.Println("Starting Fiso development environment (hybrid mode)...")
	fmt.Println("")
	fmt.Println("Fiso services run in Docker. Start your service on the host:")
	fmt.Println("")
	fmt.Println("  cd fiso/user-service && go run .")
	fmt.Println("")
	fmt.Println("Then send a test event:")
	fmt.Println("")
	fmt.Println(`  curl -X POST http://localhost:8081/ingest \`)
	fmt.Println(`    -H "Content-Type: application/json" \`)
	fmt.Println(`    -d '{"order_id": "12345", "amount": 99.99}'`)
	fmt.Println("")
	fmt.Println("Your service connects to fiso-link at: http://localhost:3500")
	fmt.Println("Flow: curl → fiso-flow(:8081) → user-service(host:8082) → fiso-link(:3500) → external-api")
	fmt.Println("")
}

func printDockerBanner() {
	fmt.Println("Starting Fiso development environment (docker mode)...")
	fmt.Println("")
	fmt.Println("Once services are up, send a test event:")
	fmt.Println("")
	fmt.Println(`  curl -X POST http://localhost:8081/ingest \`)
	fmt.Println(`    -H "Content-Type: application/json" \`)
	fmt.Println(`    -d '{"order_id": "12345", "amount": 99.99}'`)
	fmt.Println("")
	fmt.Println("Flow: curl → fiso-flow(:8081) → user-service → fiso-link → external-api")
	fmt.Println("")
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

func composeDown(dockerMode bool) {
	fileArgs := buildComposeFileArgs()
	downArgs := append(append([]string{}, fileArgs...), "down", "--remove-orphans")
	cmd := exec.Command("docker", downArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	_ = os.Remove(overridePath)
}

// writeDevOverride generates a docker-compose override file.
//
// In hybrid mode (default): adds extra_hosts so fiso-flow can reach the
// user-service on the host, and exposes fiso-link port 3500 to the host.
//
// In maintainer mode (Dockerfile.flow/Dockerfile.link present): additionally
// adds local build directives to replace GHCR images.
//
// In docker mode with no Dockerfiles: no override needed.
func writeDevOverride(dockerMode bool) error {
	hasFlow := fileExists("Dockerfile.flow")
	hasLink := fileExists("Dockerfile.link")
	hasMaintainer := hasFlow || hasLink

	// Docker mode without maintainer Dockerfiles: no override needed.
	if dockerMode && !hasMaintainer {
		_ = os.Remove(overridePath)
		return nil
	}

	content := "services:\n"

	// Hybrid mode: fiso-flow needs extra_hosts to reach user-service on host.
	// Maintainer mode: also needs build directives.
	if hasFlow {
		content += "  fiso-flow:\n"
		content += "    image: fiso-flow:dev\n"
		content += "    build:\n"
		content += "      context: ..\n"
		content += "      dockerfile: Dockerfile.flow\n"
		if !dockerMode {
			content += "    extra_hosts:\n"
			content += "      - \"user-service:host-gateway\"\n"
		}
	} else if !dockerMode {
		content += "  fiso-flow:\n"
		content += "    extra_hosts:\n"
		content += "      - \"user-service:host-gateway\"\n"
	}

	if hasLink {
		content += "  fiso-link:\n"
		content += "    image: fiso-link:dev\n"
		content += "    build:\n"
		content += "      context: ..\n"
		content += "      dockerfile: Dockerfile.link\n"
		if !dockerMode {
			content += "    ports:\n"
			content += "      - \"3500:3500\"\n"
		}
	} else if !dockerMode {
		content += "  fiso-link:\n"
		content += "    ports:\n"
		content += "      - \"3500:3500\"\n"
	}

	// In hybrid mode, disable app services that run on the host instead.
	if !dockerMode {
		composeBytes, _ := os.ReadFile("fiso/docker-compose.yml")
		composeContent := string(composeBytes)
		if strings.Contains(composeContent, "\n  user-service:\n") || strings.Contains(composeContent, "\n  user-service:") {
			content += "  user-service:\n"
			content += "    profiles:\n"
			content += "      - disabled\n"
		}
		if strings.Contains(composeContent, "\n  temporal-worker:\n") || strings.Contains(composeContent, "\n  temporal-worker:") {
			content += "  temporal-worker:\n"
			content += "    profiles:\n"
			content += "      - disabled\n"
		}
	}

	return os.WriteFile(overridePath, []byte(content), 0644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func printGHCRHint(err error) {
	msg := err.Error()
	if strings.Contains(msg, "ghcr.io") && strings.Contains(msg, "denied") {
		fmt.Println("")
		fmt.Println("Hint: All Fiso images on ghcr.io are public and do not require authentication.")
		fmt.Println("This error usually means Docker is sending expired credentials for ghcr.io.")
		fmt.Println("Fix it by running:")
		fmt.Println("")
		fmt.Println("  docker logout ghcr.io")
		fmt.Println("")
	}
}
