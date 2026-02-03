package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitHTTPSinkUserServiceProfile verifies the fix:
// When using HTTP sink, user-service should NOT have profile: docker in docker-compose.yml,
// so it WILL start with plain 'docker compose up'.
// The flow config expects user-service to be reachable.
func TestInitHTTPSinkUserServiceProfile(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Run init with HTTP source and HTTP sink (the default)
	if err := RunInit([]string{"--defaults"}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read docker-compose.yml
	dockerCompose, err := os.ReadFile(filepath.Join(dir, "fiso", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	dcContent := string(dockerCompose)

	// Read flow config
	flow, err := os.ReadFile(filepath.Join(dir, "fiso", "flows", "example-flow.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	flowContent := string(flow)

	// Verify flow expects user-service at port 8082
	if !strings.Contains(flowContent, "url: http://user-service:8082") {
		t.Error("flow should have sink URL http://user-service:8082")
	}

	// Extract user-service section from docker-compose
	userServiceSection := extractUserServiceSection(dcContent)

	// Verify user-service does NOT have profile: docker
	hasDockerProfile := strings.Contains(userServiceSection, "profiles:") &&
		strings.Contains(userServiceSection, "- docker")

	if hasDockerProfile {
		t.Error("user-service should NOT have 'profiles: [docker]' in docker-compose.yml\n" +
			"This prevents it from starting with 'docker compose up'\n" +
			"Expected: user-service should start by default without --profile docker")
	}

	// Verify user-service section exists
	if !strings.Contains(dcContent, "user-service:") {
		t.Error("docker-compose.yml should contain user-service")
	}

	// Verify services that should start with 'docker compose up'
	servicesWithoutProfiles := []string{}
	for _, svc := range []string{"external-api", "fiso-link", "fiso-flow", "user-service"} {
		section := extractServiceSection(dcContent, svc)
		if !strings.Contains(section, "profiles:") {
			servicesWithoutProfiles = append(servicesWithoutProfiles, svc)
		}
	}

	// All these services should start by default
	expectedServices := []string{"external-api", "fiso-link", "fiso-flow", "user-service"}
	for _, expected := range expectedServices {
		found := false
		for _, actual := range servicesWithoutProfiles {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("service %s should NOT have a profile (should start with 'docker compose up')", expected)
		}
	}

	t.Logf("Services that start with 'docker compose up': %v", servicesWithoutProfiles)
	t.Logf("user-service section:\n%s", userServiceSection)
}

func extractUserServiceSection(dockerCompose string) string {
	return extractServiceSection(dockerCompose, "user-service")
}

func extractServiceSection(dockerCompose, serviceName string) string {
	lines := strings.Split(dockerCompose, "\n")
	inService := false
	var section []string
	indent := 0

	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")

		// Found the service
		if strings.HasPrefix(trimmed, serviceName+":") {
			inService = true
			indent = len(line) - len(trimmed)
			section = append(section, line)
			continue
		}

		if inService {
			// Still in this service?
			lineIndent := len(line) - len(strings.TrimLeft(line, " "))

			// Empty line or comment
			if len(strings.TrimSpace(line)) == 0 || strings.HasPrefix(strings.TrimSpace(line), "#") {
				section = append(section, line)
				continue
			}

			// If we hit a line at the same or lower indent level (and it's not empty), we've exited the service
			if lineIndent <= indent && len(strings.TrimSpace(line)) > 0 {
				break
			}

			section = append(section, line)

			// Safety: don't read too far
			if i > 100 {
				break
			}
		}
	}

	return strings.Join(section, "\n")
}
