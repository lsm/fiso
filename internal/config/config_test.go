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
	if _, err := loader.Load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

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
	if _, err := loader.Load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	changed := make(chan map[string]*FlowDefinition, 1)
	loader.OnChange(func(flows map[string]*FlowDefinition) {
		changed <- flows
	})

	done := make(chan struct{})
	go func() {
		if err := loader.Watch(done); err != nil {
			t.Errorf("watch error: %v", err)
		}
	}()

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

func TestWatch_StopCleanly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "flow.yaml", `
name: test
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)
	loader := NewLoader(dir, nil)
	_, _ = loader.Load()

	done := make(chan struct{})
	errCh := make(chan error, 1)
	go func() { errCh <- loader.Watch(done) }()

	time.Sleep(50 * time.Millisecond)
	close(done)

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not stop")
	}
}

func TestWatch_FileRemoval(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "flow.yaml", `
name: removable
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)
	loader := NewLoader(dir, nil)
	_, _ = loader.Load()

	changed := make(chan map[string]*FlowDefinition, 1)
	loader.OnChange(func(flows map[string]*FlowDefinition) {
		changed <- flows
	})

	done := make(chan struct{})
	go func() { _ = loader.Watch(done) }()
	time.Sleep(100 * time.Millisecond)

	_ = os.Remove(filepath.Join(dir, "flow.yaml"))

	select {
	case flows := <-changed:
		if len(flows) != 0 {
			t.Errorf("expected 0 flows after removal, got %d", len(flows))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for removal notification")
	}
	close(done)
}

func TestWatch_InvalidDir(t *testing.T) {
	loader := NewLoader("/nonexistent/watch/dir", nil)
	err := loader.Watch(make(chan struct{}))
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestWatch_CreateEvent(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir, nil)
	_, _ = loader.Load()

	changed := make(chan map[string]*FlowDefinition, 1)
	loader.OnChange(func(flows map[string]*FlowDefinition) {
		changed <- flows
	})

	done := make(chan struct{})
	go func() { _ = loader.Watch(done) }()
	time.Sleep(100 * time.Millisecond)

	// Create a new file
	writeFile(t, dir, "new-flow.yaml", `
name: new-flow
source:
  type: kafka
  config: {}
sink:
  type: http
  config: {}
`)

	select {
	case flows := <-changed:
		if _, ok := flows["new-flow"]; !ok {
			t.Error("expected new-flow in reloaded config")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for create notification")
	}
	close(done)
}

func TestOnChange_Callback(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir, nil)

	called := false
	loader.OnChange(func(flows map[string]*FlowDefinition) {
		called = true
	})

	// OnChange just registers the callback; verify it's set
	if called {
		t.Error("callback should not be called yet")
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
}
