package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExport_BasicFlow(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	flowsDir := filepath.Join(fisoDir, "flows")
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	flowYAML := `name: test-flow
source:
  type: http
  config:
    listenAddr: ":8081"
    path: /ingest
sink:
  type: http
  config:
    url: http://api:8080
    method: POST
errorHandling:
  deadLetterTopic: dlq-test
  maxRetries: 3
`
	if err := os.WriteFile(filepath.Join(flowsDir, "test-flow.yaml"), []byte(flowYAML), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RunExport([]string{fisoDir}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	// Check CRD structure
	if !strings.Contains(out, "apiVersion: fiso.io/v1alpha1") {
		t.Error("output should contain apiVersion: fiso.io/v1alpha1")
	}
	if !strings.Contains(out, "kind: FlowDefinition") {
		t.Error("output should contain kind: FlowDefinition")
	}
	if !strings.Contains(out, "name: test-flow") {
		t.Error("output should contain name: test-flow")
	}
	if !strings.Contains(out, "namespace: fiso-system") {
		t.Error("output should contain namespace: fiso-system")
	}
	if !strings.Contains(out, "deadLetterTopic: dlq-test") {
		t.Error("output should contain deadLetterTopic")
	}
	if !strings.Contains(out, "maxRetries: 3") {
		t.Error("output should contain maxRetries")
	}

	// Source/sink config should be stringified
	if !strings.Contains(out, "listenAddr:") {
		t.Error("output should contain source config listenAddr")
	}
}

func TestRunExport_FlowWithCELTransform(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	flowsDir := filepath.Join(fisoDir, "flows")
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	flowYAML := `name: cel-flow
source:
  type: kafka
  config:
    brokers:
      - broker1:9092
      - broker2:9092
    topic: events
    consumerGroup: fiso-cel
transform:
  cel: '{"id": data.legacy_id}'
sink:
  type: http
  config:
    url: http://api:8080
    method: POST
errorHandling:
  maxRetries: 1
`
	if err := os.WriteFile(filepath.Join(flowsDir, "cel-flow.yaml"), []byte(flowYAML), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RunExport([]string{fisoDir}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	if !strings.Contains(out, "kind: FlowDefinition") {
		t.Error("output should contain kind: FlowDefinition")
	}
	if !strings.Contains(out, `cel:`) {
		t.Error("output should contain CEL transform")
	}
	// Kafka brokers list should be comma-separated
	if !strings.Contains(out, "broker1:9092,broker2:9092") {
		t.Error("output should contain comma-separated brokers")
	}
}

func TestRunExport_FlowWithCloudEvents(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	flowsDir := filepath.Join(fisoDir, "flows")
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	flowYAML := `name: ce-flow
source:
  type: http
  config:
    listenAddr: ":8081"
cloudevents:
  type: com.example.order
  source: /orders
sink:
  type: http
  config:
    url: http://api:8080
`
	if err := os.WriteFile(filepath.Join(flowsDir, "ce-flow.yaml"), []byte(flowYAML), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RunExport([]string{fisoDir}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	// CloudEvents should be preserved as annotations
	if !strings.Contains(out, "fiso.io/cloudevents-type: com.example.order") {
		t.Error("output should contain cloudevents-type annotation")
	}
	if !strings.Contains(out, "fiso.io/cloudevents-source: /orders") {
		t.Error("output should contain cloudevents-source annotation")
	}
}

func TestRunExport_LinkTargets(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	linkDir := filepath.Join(fisoDir, "link")
	if err := os.MkdirAll(linkDir, 0755); err != nil {
		t.Fatal(err)
	}

	linkYAML := `listenAddr: ":3500"
metricsAddr: ":9091"
targets:
  - name: crm
    protocol: https
    host: api.salesforce.com
    auth:
      type: bearer
      secretRef:
        filePath: /secrets/crm-token
    circuitBreaker:
      enabled: true
      failureThreshold: 5
      resetTimeout: "30s"
    retry:
      maxAttempts: 3
      backoff: exponential
    allowedPaths:
      - /api/v2/**
  - name: payment
    protocol: https
    host: api.stripe.com
`
	if err := os.WriteFile(filepath.Join(linkDir, "config.yaml"), []byte(linkYAML), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RunExport([]string{fisoDir}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	// Should have two LinkTarget documents
	if strings.Count(out, "kind: LinkTarget") != 2 {
		t.Errorf("expected 2 LinkTarget documents, got %d", strings.Count(out, "kind: LinkTarget"))
	}

	// First target
	if !strings.Contains(out, "name: crm") {
		t.Error("output should contain crm target")
	}
	if !strings.Contains(out, "host: api.salesforce.com") {
		t.Error("output should contain salesforce host")
	}
	if !strings.Contains(out, "secretName: /secrets/crm-token") {
		t.Error("output should contain secretName from secretRef.filePath")
	}
	if !strings.Contains(out, "failureThreshold: 5") {
		t.Error("output should contain circuit breaker failureThreshold")
	}
	if !strings.Contains(out, "maxAttempts: 3") {
		t.Error("output should contain retry maxAttempts")
	}

	// Second target
	if !strings.Contains(out, "name: payment") {
		t.Error("output should contain payment target")
	}
}

func TestRunExport_FlowAndLinkCombined(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	flowsDir := filepath.Join(fisoDir, "flows")
	linkDir := filepath.Join(fisoDir, "link")
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(linkDir, 0755); err != nil {
		t.Fatal(err)
	}

	flowYAML := `name: my-flow
source:
  type: http
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://api:8080
`
	linkYAML := `targets:
  - name: api
    protocol: https
    host: api.example.com
`
	if err := os.WriteFile(filepath.Join(flowsDir, "flow.yaml"), []byte(flowYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linkDir, "config.yaml"), []byte(linkYAML), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RunExport([]string{fisoDir}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	// Should have both FlowDefinition and LinkTarget separated by ---
	if !strings.Contains(out, "kind: FlowDefinition") {
		t.Error("output should contain FlowDefinition")
	}
	if !strings.Contains(out, "kind: LinkTarget") {
		t.Error("output should contain LinkTarget")
	}
	if !strings.Contains(out, "---") {
		t.Error("output should contain document separator")
	}
}

func TestRunExport_CustomNamespace(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	flowsDir := filepath.Join(fisoDir, "flows")
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	flowYAML := `name: ns-flow
source:
  type: http
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://api:8080
`
	if err := os.WriteFile(filepath.Join(flowsDir, "flow.yaml"), []byte(flowYAML), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RunExport([]string{fisoDir, "--namespace=production"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "namespace: production") {
		t.Error("output should contain custom namespace")
	}
}

func TestRunExport_NoConfigs(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	if err := os.MkdirAll(fisoDir, 0755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := RunExport([]string{fisoDir}, &buf)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
	if !strings.Contains(err.Error(), "no flow or link configs found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunExport_UnsupportedFormat(t *testing.T) {
	err := RunExport([]string{"--format=helm"}, nil)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunExport_Help(t *testing.T) {
	// Should not error on --help
	err := RunExport([]string{"--help"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
