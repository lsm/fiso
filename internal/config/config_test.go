package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "order-events.yaml", `
name: order-events
source:
  type: kafka
  config:
    brokers:
      - localhost:9092
    topic: orders
    consumerGroup: fiso-order-flow
transform:
  cel: '{"id": data.legacy_id}'
sink:
  type: http
  config:
    url: http://localhost:8080/callbacks
    method: POST
errorHandling:
  deadLetterTopic: fiso-dlq-order-events
  maxRetries: 5
  backoff: exponential
`)

	loader := NewLoader(dir, nil)
	flows, err := loader.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(flows))
	}

	flow := flows["order-events"]
	if flow == nil {
		t.Fatal("expected flow 'order-events'")
	}
	if flow.Source.Type != "kafka" {
		t.Errorf("expected source type kafka, got %s", flow.Source.Type)
	}
	if flow.Transform == nil || flow.Transform.CEL != `{"id": data.legacy_id}` {
		t.Error("transform CEL expression mismatch")
	}
	if flow.Sink.Type != "http" {
		t.Errorf("expected sink type http, got %s", flow.Sink.Type)
	}
	if flow.ErrorHandling.DeadLetterTopic != "fiso-dlq-order-events" {
		t.Errorf("expected DLQ topic fiso-dlq-order-events, got %s", flow.ErrorHandling.DeadLetterTopic)
	}
}

func TestLoad_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "flow-a.yaml", `
name: flow-a
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)
	writeFile(t, dir, "flow-b.yml", `
name: flow-b
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)
	// Non-YAML file should be ignored
	writeFile(t, dir, "readme.txt", "not a config")

	loader := NewLoader(dir, nil)
	flows, err := loader.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(flows) != 2 {
		t.Fatalf("expected 2 flows, got %d", len(flows))
	}
}

func TestLoad_MissingName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.yaml", `
source:
  type: kafka
sink:
  type: http
`)

	loader := NewLoader(dir, nil)
	flows, err := loader.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	// File with missing name should be skipped (logged as error)
	if len(flows) != 0 {
		t.Fatalf("expected 0 flows, got %d", len(flows))
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.yaml", "{{{{not yaml")

	loader := NewLoader(dir, nil)
	flows, err := loader.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(flows) != 0 {
		t.Fatalf("expected 0 flows for invalid YAML, got %d", len(flows))
	}
}

func TestLoad_NonexistentDir(t *testing.T) {
	loader := NewLoader("/nonexistent/path", nil)
	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestLoad_NoTransform(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "simple.yaml", `
name: simple-flow
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)

	loader := NewLoader(dir, nil)
	flows, err := loader.Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	flow := flows["simple-flow"]
	if flow.Transform != nil {
		t.Error("expected nil transform for config without transform section")
	}
}

func TestGetFlows_ReturnsSnapshot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "flow.yaml", `
name: test-flow
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)

	loader := NewLoader(dir, nil)
	loader.Load()

	flows := loader.GetFlows()
	if len(flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(flows))
	}

	// Mutating the returned map should not affect the loader
	delete(flows, "test-flow")
	if len(loader.GetFlows()) != 1 {
		t.Fatal("mutating returned map affected loader state")
	}
}

func TestWatch_DetectsChanges(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "flow.yaml", `
name: original-flow
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)

	loader := NewLoader(dir, nil)
	loader.Load()

	changed := make(chan map[string]*FlowDefinition, 1)
	loader.OnChange(func(flows map[string]*FlowDefinition) {
		changed <- flows
	})

	done := make(chan struct{})
	go loader.Watch(done)

	// Give watcher time to start
	time.Sleep(100 * time.Millisecond)

	// Modify the file
	writeFile(t, dir, "flow.yaml", `
name: updated-flow
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)

	select {
	case flows := <-changed:
		if _, ok := flows["updated-flow"]; !ok {
			t.Error("expected updated-flow in reloaded config")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for config change notification")
	}

	close(done)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
}
