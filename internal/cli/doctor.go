package cli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunDoctor performs health checks on the environment and project.
func RunDoctor(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println(`Usage: fiso doctor

Checks environment and project health:
  - Docker installation and daemon status
  - Docker Compose availability
  - Project structure (fiso/ directory)
  - Configuration validity
  - Port availability (8081, 3500, 9090)`)
		return nil
	}

	fmt.Println("Fiso Doctor")
	fmt.Println()

	criticalFailures := 0

	// Check 1: Docker installed
	ok, detail := checkDockerInstalled()
	if ok {
		fmt.Printf("  ✓ Docker installed%s\n", detail)
	} else {
		fmt.Fprintf(os.Stderr, "  ✗ Docker not found\n")
		fmt.Fprintf(os.Stderr, "    Hint: Install Docker Desktop or Docker Engine\n")
		criticalFailures++
	}

	// Check 2: Docker daemon running
	ok, detail = checkDockerDaemon()
	if ok {
		fmt.Printf("  ✓ Docker daemon running%s\n", detail)
	} else {
		fmt.Fprintf(os.Stderr, "  ✗ Docker daemon not running\n")
		fmt.Fprintf(os.Stderr, "    Hint: Start Docker Desktop or run 'sudo systemctl start docker'\n")
		criticalFailures++
	}

	// Check 3: Docker Compose available
	ok, detail = checkDockerCompose()
	if ok {
		fmt.Printf("  ✓ Docker Compose available%s\n", detail)
	} else {
		fmt.Fprintf(os.Stderr, "  ✗ Docker Compose not available\n")
		fmt.Fprintf(os.Stderr, "    Hint: Update Docker to a version that includes Docker Compose\n")
		criticalFailures++
	}

	// Check 4: Project structure
	ok, detail = checkProjectStructure()
	projectExists := ok
	if ok {
		fmt.Printf("  ✓ Project structure found%s\n", detail)
	} else {
		fmt.Fprintf(os.Stderr, "  ✗ Project structure not found\n")
		fmt.Fprintf(os.Stderr, "    Hint: Run 'fiso init' to create a project\n")
		criticalFailures++
	}

	// Check 5: Config valid (only if project exists)
	if projectExists {
		ok, detail = checkConfigValidity()
		if ok {
			fmt.Printf("  ✓ Configuration valid%s\n", detail)
		} else {
			fmt.Fprintf(os.Stderr, "  ✗ Configuration invalid\n")
			fmt.Fprintf(os.Stderr, "    Hint: Run 'fiso validate' for details\n")
			criticalFailures++
		}
	}

	// Check 6: Port availability (informational, non-fatal)
	if projectExists {
		ports := []int{8081, 3500, 9090}
		portWarnings := checkPortAvailability(ports)
		if len(portWarnings) == 0 {
			fmt.Printf("  ✓ Ports available (8081, 3500, 9090)\n")
		} else {
			for _, warning := range portWarnings {
				fmt.Printf("  ⚠ %s\n", warning)
			}
		}
	}

	fmt.Println()

	if criticalFailures > 0 {
		return fmt.Errorf("doctor found %d issue(s)", criticalFailures)
	}

	fmt.Println("All checks passed.")
	return nil
}

// checkDockerInstalled checks if docker is in PATH.
func checkDockerInstalled() (bool, string) {
	_, err := exec.LookPath("docker")
	return err == nil, ""
}

// checkDockerDaemon checks if the Docker daemon is running and returns version info.
func checkDockerDaemon() (bool, string) {
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, ""
	}
	version := strings.TrimSpace(string(output))
	if version != "" {
		return true, fmt.Sprintf(" (v%s)", version)
	}
	return true, ""
}

// checkDockerCompose checks if Docker Compose is available.
func checkDockerCompose() (bool, string) {
	cmd := exec.Command("docker", "compose", "version")
	err := cmd.Run()
	return err == nil, ""
}

// checkProjectStructure verifies that the fiso/ directory and essential files exist.
func checkProjectStructure() (bool, string) {
	_, err1 := os.Stat("fiso/docker-compose.yml")
	_, err2 := os.Stat("fiso/flows")
	if err1 != nil || err2 != nil {
		return false, ""
	}
	return true, ""
}

// checkConfigValidity validates flow and link configurations.
func checkConfigValidity() (bool, string) {
	// Count and validate flow files
	entries, err := os.ReadDir("fiso/flows")
	if err != nil {
		return false, ""
	}

	flowCount := 0
	var flowErrors []validationError

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		flowCount++
		path := filepath.Join("fiso/flows", entry.Name())
		flowErrors = append(flowErrors, validateFlowFile(path)...)
	}

	// Check for link config files
	linkSearchPaths := []string{
		filepath.Join("fiso", "link", "config.yaml"),
		filepath.Join("fiso", "link-config.yaml"),
		filepath.Join("fiso", "links.yaml"),
	}
	for _, linkPath := range linkSearchPaths {
		if _, err := os.Stat(linkPath); err == nil {
			linkErrors := validateLinkConfig(linkPath)
			flowErrors = append(flowErrors, linkErrors...)
		}
	}

	if len(flowErrors) > 0 {
		return false, ""
	}

	if flowCount == 0 {
		return true, ""
	}
	if flowCount == 1 {
		return true, " (1 flow)"
	}
	return true, fmt.Sprintf(" (%d flows)", flowCount)
}

// checkPortAvailability tests if required ports are available.
// Returns a list of warning messages for ports that are in use.
func checkPortAvailability(ports []int) []string {
	var warnings []string
	for _, port := range ports {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Port %d is in use", port))
		} else {
			_ = listener.Close()
		}
	}
	return warnings
}
