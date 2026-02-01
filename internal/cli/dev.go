package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

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

	upArgs := []string{"up", "--remove-orphans"}

	if _, err := os.Stat("Dockerfile.flow"); err == nil {
		upArgs = append(upArgs, "--build")
	}

	upDone := make(chan error, 1)
	go func() {
		upDone <- runCompose(ctx, upArgs...)
	}()

	go func() {
		if err := waitForKafkaAndSeed(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "warning: seed failed: %v\n", err)
		}
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

var composeFileArgs = []string{"compose", "-f", "fiso/docker-compose.yml"}

func runCompose(ctx context.Context, args ...string) error {
	fullArgs := append(append([]string{}, composeFileArgs...), args...)
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func composeDown() {
	fullArgs := append(append([]string{}, composeFileArgs...), "down", "--remove-orphans")
	cmd := exec.Command("docker", fullArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func waitForKafkaAndSeed(ctx context.Context) error {
	deadline := time.After(60 * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timed out waiting for Kafka")
		case <-ticker.C:
			if kafkaHealthy(ctx) {
				return seedTestMessage(ctx)
			}
		}
	}
}

func kafkaHealthy(ctx context.Context) bool {
	fullArgs := append(append([]string{}, composeFileArgs...), "ps", "--format", "json", "kafka")
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return containsHealthy(out)
}

func containsHealthy(output []byte) bool {
	if len(output) == 0 {
		return false
	}
	s := bytes.ToLower(output)
	return bytes.Contains(s, []byte("healthy")) && !bytes.Contains(s, []byte("unhealthy"))
}

func seedTestMessage(ctx context.Context) error {
	fmt.Println("Kafka is healthy. Seeding test message...")

	payload := `{"id":"test-001","data":{"legacy_id":"ORD-42","order_status":"created"},"time":"2024-01-01T00:00:00Z"}`

	seedArgs := append(append([]string{}, composeFileArgs...), "exec", "-T", "kafka",
		"/opt/kafka/bin/kafka-console-producer.sh",
		"--bootstrap-server", "localhost:9092",
		"--topic", "events",
	)
	cmd := exec.CommandContext(ctx, "docker", seedArgs...)
	cmd.Stdin = strings.NewReader(payload + "\n")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("seed message: %w", err)
	}

	fmt.Println("Test message seeded to 'events' topic.")
	return nil
}
