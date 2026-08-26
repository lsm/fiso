package cli

import (
	"bytes"
	"errors"
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
  type: grpc
  config:
    listenAddr: ":8081"
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
    brokers: broker1:9092,broker2:9092
    topic: events
    consumerGroup: fiso-cel
transform:
  fields: {id: data.legacy_id}
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
	if !strings.Contains(out, `fields:`) {
		t.Error("output should contain fields transform")
	}
	if !strings.Contains(out, "broker1:9092,broker2:9092") {
		t.Error("output should preserve string-valued brokers")
	}
}

func TestRunExport_LinkTargets(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	linkDir := filepath.Join(fisoDir, "link")
	if err := os.MkdirAll(linkDir, 0755); err != nil {
		t.Fatal(err)
	}

	linkYAML := `targets:
  - name: crm
    protocol: https
    host: api.salesforce.com
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
  type: grpc
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
  type: grpc
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

func TestRunExport_WritesManifestStreamOnce(t *testing.T) {
	flowYAML := `name: flow
source:
  type: grpc
  config: {}
sink:
  type: http
  config: {}
`
	linkYAML := `targets:
  - name: api
    protocol: https
    host: api.example.com
`
	fisoDir := writeExportFixture(t, flowYAML, linkYAML)
	writer := &countingWriter{}

	if err := RunExport([]string{fisoDir}, writer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if writer.writes != 1 {
		t.Fatalf("expected one manifest write, got %d", writer.writes)
	}
	if !strings.Contains(writer.String(), "---") {
		t.Fatalf("expected multi-document output, got %q", writer.String())
	}
}

func TestRunExport_ReturnsWriterError(t *testing.T) {
	flowYAML := `name: flow
source:
  type: grpc
  config: {}
sink:
  type: http
  config: {}
`
	fisoDir := writeExportFixture(t, flowYAML, "")
	wantErr := errors.New("write failed")

	err := RunExport([]string{fisoDir}, errorWriter{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected writer error, got %v", err)
	}
}

func TestRunExport_HelpReturnsWriterError(t *testing.T) {
	wantErr := errors.New("write failed")
	if err := RunExport([]string{"--help"}, errorWriter{err: wantErr}); !errors.Is(err, wantErr) {
		t.Fatalf("expected writer error, got %v", err)
	}
}

type countingWriter struct {
	bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
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
	var buf bytes.Buffer
	if err := RunExport([]string{"--help"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "losslessly representable subset") {
		t.Fatalf("help should describe the bounded export contract, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "before any YAML is written") {
		t.Fatalf("help should describe fail-closed output, got %q", buf.String())
	}
}

func TestRunExport_MalformedFlowYAML(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	flowsDir := filepath.Join(fisoDir, "flows")
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(flowsDir, "bad.yaml"), []byte("{{{invalid"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := RunExport([]string{fisoDir}, &buf)
	if err == nil {
		t.Fatal("expected parse error for malformed flow YAML")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestRunExport_MalformedLinkYAML(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	linkDir := filepath.Join(fisoDir, "link")
	if err := os.MkdirAll(linkDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(linkDir, "config.yaml"), []byte("{{{invalid"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := RunExport([]string{fisoDir}, &buf)
	if err == nil {
		t.Fatal("expected parse error for malformed link YAML")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestRunExport_NonYAMLFilesIgnored(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	flowsDir := filepath.Join(fisoDir, "flows")
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a .txt file
	if err := os.WriteFile(filepath.Join(flowsDir, "readme.txt"), []byte("not yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a valid .yaml file
	flowYAML := `name: test-flow
source:
  type: grpc
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://api:8080
`
	if err := os.WriteFile(filepath.Join(flowsDir, "test.yaml"), []byte(flowYAML), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RunExport([]string{fisoDir}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// Should only export the .yaml file
	if strings.Count(out, "kind: FlowDefinition") != 1 {
		t.Errorf("expected exactly 1 FlowDefinition, output: %s", out)
	}
}

func TestExportFlows_ReadDirError(t *testing.T) {
	nonExistentDir := filepath.Join(t.TempDir(), "nonexistent")

	_, err := exportFlows(nonExistentDir, "fiso-system")
	if err == nil {
		t.Fatal("expected error when reading non-existent directory")
	}
	if !strings.Contains(err.Error(), "read dir") {
		t.Errorf("expected error about reading directory, got: %v", err)
	}
}

func TestExportFlows_DirectorySkipped(t *testing.T) {
	dir := t.TempDir()
	flowsDir := filepath.Join(dir, "flows")
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory inside flows/
	subdir := filepath.Join(flowsDir, "subdir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a valid flow file
	flowYAML := `name: test-flow
source:
  type: grpc
  config: {}
sink:
  type: http
  config: {}
`
	if err := os.WriteFile(filepath.Join(flowsDir, "test.yaml"), []byte(flowYAML), 0644); err != nil {
		t.Fatal(err)
	}

	docs, err := exportFlows(flowsDir, "fiso-system")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 1 document (the subdirectory should be skipped)
	if len(docs) != 1 {
		t.Errorf("expected 1 document, got %d", len(docs))
	}
}

func TestExportFlows_ReadFileError(t *testing.T) {
	// Skip on Windows/CI where permissions might not work as expected
	if os.Getenv("CI") != "" || strings.Contains(strings.ToLower(os.Getenv("OS")), "windows") {
		t.Skip("Skipping permission test on CI/Windows")
	}

	dir := t.TempDir()
	flowsDir := filepath.Join(dir, "flows")
	if err := os.MkdirAll(flowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a file with no read permissions
	unreadableFile := filepath.Join(flowsDir, "unreadable.yaml")
	if err := os.WriteFile(unreadableFile, []byte("data"), 0000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(unreadableFile, 0644) }() // cleanup

	_, err := exportFlows(flowsDir, "fiso-system")
	if err == nil {
		t.Fatal("expected error when reading unreadable file")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Errorf("expected error about reading file, got: %v", err)
	}
}

func TestExportLinks_ReadFileError(t *testing.T) {
	// Skip on Windows/CI where permissions might not work as expected
	if os.Getenv("CI") != "" || strings.Contains(strings.ToLower(os.Getenv("OS")), "windows") {
		t.Skip("Skipping permission test on CI/Windows")
	}

	dir := t.TempDir()
	linkPath := filepath.Join(dir, "config.yaml")

	// Create a file with no read permissions
	if err := os.WriteFile(linkPath, []byte("data"), 0000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(linkPath, 0644) }() // cleanup

	_, err := exportLinks(linkPath, "fiso-system")
	if err == nil {
		t.Fatal("expected error when reading unreadable file")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Errorf("expected error about reading file, got: %v", err)
	}
}

func TestRunExport_DefaultsOmittedLinkProtocol(t *testing.T) {
	linkYAML := `targets:
  - name: api
    host: api.example.com
`
	fisoDir := writeExportFixture(t, "", linkYAML)

	var buf bytes.Buffer
	if err := RunExport([]string{fisoDir}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "protocol: https") {
		t.Fatalf("expected omitted local protocol to default to https, got %q", buf.String())
	}
}

func TestRunExport_RejectsInvalidNamespaceWithoutOutput(t *testing.T) {
	flowYAML := `name: flow
source:
  type: grpc
  config: {}
sink:
  type: http
  config: {}
`
	fisoDir := writeExportFixture(t, flowYAML, "")

	var buf bytes.Buffer
	err := RunExport([]string{fisoDir, "--namespace=Invalid_Name"}, &buf)
	if err == nil {
		t.Fatal("expected invalid namespace to fail")
	}
	if !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("expected namespace validation path, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestRunExport_RejectsInvalidSourceWithoutOutput(t *testing.T) {
	flowYAML := `name: invalid-flow
source:
  type: invalid
  config: {}
sink:
  type: http
  config: {}
`
	fisoDir := writeExportFixture(t, flowYAML, "")

	var buf bytes.Buffer
	err := RunExport([]string{fisoDir}, &buf)
	if err == nil {
		t.Fatal("expected invalid source to fail")
	}
	if !strings.Contains(err.Error(), "source.type") {
		t.Fatalf("expected source validation path, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestRunExport_LinkWithNoneAuth(t *testing.T) {
	dir := t.TempDir()
	fisoDir := filepath.Join(dir, "fiso")
	linkDir := filepath.Join(fisoDir, "link")
	if err := os.MkdirAll(linkDir, 0755); err != nil {
		t.Fatal(err)
	}

	linkYAML := `targets:
  - name: api
    protocol: http
    host: api.example.com
    auth:
      type: none
`
	if err := os.WriteFile(filepath.Join(linkDir, "config.yaml"), []byte(linkYAML), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := RunExport([]string{fisoDir}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// Should not have auth spec when type is "none"
	if strings.Contains(out, "auth:") {
		t.Error("output should not contain auth spec for type 'none'")
	}
}

func TestRunExport_RejectsCoercedFlowScalars(t *testing.T) {
	tests := []struct {
		name     string
		flowYAML string
		wantPath string
	}{
		{
			name: "numeric name",
			flowYAML: `name: 123
source:
  type: grpc
  config: {}
sink:
  type: http
  config: {}
`,
			wantPath: "name",
		},
		{
			name: "numeric transform expression",
			flowYAML: `name: flow
source:
  type: grpc
  config: {}
transform:
  fields:
    id: 123
sink:
  type: http
  config: {}
`,
			wantPath: "transform.fields.id",
		},
		{
			name: "numeric transform key",
			flowYAML: `name: flow
source:
  type: grpc
  config: {}
transform:
  fields:
    123: data.id
sink:
  type: http
  config: {}
`,
			wantPath: "transform.fields.123",
		},
		{
			name: "numeric dead-letter topic",
			flowYAML: `name: flow
source:
  type: grpc
  config: {}
sink:
  type: http
  config: {}
errorHandling:
  deadLetterTopic: 123
`,
			wantPath: "errorHandling.deadLetterTopic",
		},
		{
			name: "fractional max retries",
			flowYAML: `name: flow
source:
  type: grpc
  config: {}
sink:
  type: http
  config: {}
errorHandling:
  maxRetries: 2.9
`,
			wantPath: "errorHandling.maxRetries",
		},
		{
			name: "boolean CloudEvents field",
			flowYAML: `name: flow
source:
  type: grpc
  config: {}
cloudevents:
  type: true
sink:
  type: http
  config: {}
`,
			wantPath: "cloudevents.type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fisoDir := writeExportFixture(t, tt.flowYAML, "")
			var buf bytes.Buffer
			err := RunExport([]string{fisoDir}, &buf)
			if err == nil {
				t.Fatal("expected scalar coercion to fail")
			}
			if !strings.Contains(err.Error(), tt.wantPath) {
				t.Fatalf("expected error to name %q, got %v", tt.wantPath, err)
			}
			if buf.Len() != 0 {
				t.Fatalf("expected no output, got %q", buf.String())
			}
		})
	}
}

func TestRunExport_RejectsInvalidTransformExpression(t *testing.T) {
	flowYAML := `name: flow
source:
  type: grpc
  config: {}
transform:
  fields:
    id: "data.?"
sink:
  type: http
  config: {}
`
	fisoDir := writeExportFixture(t, flowYAML, "")

	var buf bytes.Buffer
	err := RunExport([]string{fisoDir}, &buf)
	if err == nil {
		t.Fatal("expected invalid transform to fail")
	}
	if !strings.Contains(err.Error(), "transform.fields") {
		t.Fatalf("expected transform validation path, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestRunExport_RejectsLossyFlowConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		flowName string
		fragment string
		wantPath string
	}{
		{
			name:     "invalid Kubernetes resource name",
			flowName: "Invalid_Name",
			fragment: `source:
  type: grpc
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://api:8080
`,
			wantPath: "name",
		},
		{
			name: "HTTP source excluded by FlowDefinition CRD",
			fragment: `source:
  type: http
  config:
    listenAddr: ":8081"
    path: /ingest
sink:
  type: http
  config:
    url: http://api:8080
`,
			wantPath: "source.type",
		},
		{
			name: "named Kafka clusters",
			fragment: `kafka:
  clusters:
    main:
      brokers: [broker:9092]
source:
  type: grpc
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://api:8080
`,
			wantPath: "kafka",
		},
		{
			name: "CloudEvents overrides",
			fragment: `source:
  type: grpc
  config:
    listenAddr: ":8081"
cloudevents:
  type: com.example.event
sink:
  type: http
  config:
    url: http://api:8080
`,
			wantPath: "cloudevents",
		},
		{
			name: "interceptors",
			fragment: `source:
  type: grpc
  config:
    listenAddr: ":8081"
interceptors:
  - type: wasm
    config:
      module: enrich.wasm
sink:
  type: http
  config:
    url: http://api:8080
`,
			wantPath: "interceptors",
		},
		{
			name: "retry backoff",
			fragment: `source:
  type: grpc
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://api:8080
errorHandling:
  backoff: exponential
`,
			wantPath: "errorHandling.backoff",
		},
		{
			name: "commit policy",
			fragment: `source:
  type: grpc
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://api:8080
errorHandling:
  commitPolicy: sink
`,
			wantPath: "errorHandling.commitPolicy",
		},
		{
			name: "transactional ID",
			fragment: `source:
  type: grpc
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://api:8080
errorHandling:
  transactionalId: tx-orders
`,
			wantPath: "errorHandling.transactionalId",
		},
		{
			name: "list source config",
			fragment: `source:
  type: grpc
  config:
    listenAddr: ":8081"
    metadata: [one, two]
sink:
  type: http
  config:
    url: http://api:8080
`,
			wantPath: "source.config.metadata",
		},
		{
			name: "list Kafka brokers",
			fragment: `source:
  type: kafka
  config:
    brokers: [broker1:9092, broker2:9092]
    topic: events
    consumerGroup: fiso
sink:
  type: http
  config:
    url: http://api:8080
`,
			wantPath: "source.config.brokers",
		},
		{
			name: "nested sink config",
			fragment: `source:
  type: grpc
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://api:8080
    tls:
      enabled: true
`,
			wantPath: "sink.config.tls",
		},
		{
			name: "scalar type coercion",
			fragment: `source:
  type: grpc
  config:
    listenAddr: ":8081"
    enabled: true
sink:
  type: http
  config:
    url: http://api:8080
`,
			wantPath: "source.config.enabled",
		},
		{
			name: "non-string source config key",
			fragment: `source:
  type: grpc
  config:
    123: abc
sink:
  type: http
  config:
    url: http://api:8080
`,
			wantPath: "source.config.123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flowName := tt.flowName
			if flowName == "" {
				flowName = "lossy-flow"
			}
			fisoDir := writeExportFixture(t, "name: "+flowName+"\n"+tt.fragment, "")
			var buf bytes.Buffer
			err := RunExport([]string{fisoDir}, &buf)
			if err == nil {
				t.Fatal("expected lossy export to fail")
			}
			if !strings.Contains(err.Error(), tt.wantPath) {
				t.Fatalf("expected error to name %q, got %v", tt.wantPath, err)
			}
			if buf.Len() != 0 {
				t.Fatalf("expected no partial output, got %q", buf.String())
			}
		})
	}
}

func TestRunExport_RejectsCoercedLinkScalars(t *testing.T) {
	tests := []struct {
		name     string
		linkYAML string
		wantPath string
	}{
		{
			name: "numeric name",
			linkYAML: `targets:
  - name: 123
    protocol: https
    host: api.example.com
`,
			wantPath: "targets[0].name",
		},
		{
			name: "numeric host",
			linkYAML: `targets:
  - name: api
    protocol: https
    host: 456
`,
			wantPath: "targets[0].host",
		},
		{
			name: "non-string allowed path",
			linkYAML: `targets:
  - name: api
    protocol: https
    host: api.example.com
    allowedPaths: [1, true]
`,
			wantPath: "targets[0].allowedPaths[0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fisoDir := writeExportFixture(t, "", tt.linkYAML)
			var buf bytes.Buffer
			err := RunExport([]string{fisoDir}, &buf)
			if err == nil {
				t.Fatal("expected scalar coercion to fail")
			}
			if !strings.Contains(err.Error(), tt.wantPath) {
				t.Fatalf("expected error to name %q, got %v", tt.wantPath, err)
			}
			if buf.Len() != 0 {
				t.Fatalf("expected no output, got %q", buf.String())
			}
		})
	}
}

func TestRunExport_RejectsUnknownFields(t *testing.T) {
	tests := []struct {
		name     string
		flowYAML string
		linkYAML string
		wantPath string
	}{
		{
			name: "Flow field",
			flowYAML: `name: flow
source:
  type: grpc
  config: {}
sink:
  type: http
  config: {}
errorHandling:
  maxRetry: 5
`,
			wantPath: "maxRetry",
		},
		{
			name: "circuit breaker field",
			linkYAML: `targets:
  - name: api
    protocol: https
    host: api.example.com
    circuitBreaker:
      failThreshold: 3
`,
			wantPath: "targets[0].circuitBreaker.failThreshold",
		},
		{
			name: "retry field",
			linkYAML: `targets:
  - name: api
    protocol: https
    host: api.example.com
    retry:
      attempts: 3
`,
			wantPath: "targets[0].retry.attempts",
		},
		{
			name: "rate limit field",
			linkYAML: `targets:
  - name: api
    protocol: https
    host: api.example.com
    rateLimit:
      requests: 10
`,
			wantPath: "targets[0].rateLimit.requests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fisoDir := writeExportFixture(t, tt.flowYAML, tt.linkYAML)
			var buf bytes.Buffer
			err := RunExport([]string{fisoDir}, &buf)
			if err == nil {
				t.Fatal("expected unknown field to fail")
			}
			if !strings.Contains(err.Error(), tt.wantPath) {
				t.Fatalf("expected error to name %q, got %v", tt.wantPath, err)
			}
			if buf.Len() != 0 {
				t.Fatalf("expected no output, got %q", buf.String())
			}
		})
	}
}

func TestRunExport_RejectsLossyLinkConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		targetName string
		fragment   string
		wantPath   string
	}{
		{name: "listen address", fragment: "listenAddr: :4500\n", wantPath: "listenAddr"},
		{name: "metrics address", fragment: "metricsAddr: :9191\n", wantPath: "metricsAddr"},
		{name: "invalid Kubernetes resource name", targetName: "Invalid_Name", wantPath: "targets[0].name"},
		{name: "invalid circuit breaker enabled type", fragment: "    circuitBreaker:\n      enabled: \"true\"\n", wantPath: "targets[0].circuitBreaker.enabled"},
		{name: "invalid circuit breaker threshold type", fragment: "    circuitBreaker:\n      enabled: true\n      failureThreshold: \"3\"\n", wantPath: "targets[0].circuitBreaker.failureThreshold"},
		{name: "fractional circuit breaker threshold", fragment: "    circuitBreaker:\n      enabled: true\n      failureThreshold: 2.9\n", wantPath: "targets[0].circuitBreaker.failureThreshold"},
		{name: "invalid retry attempts type", fragment: "    retry:\n      maxAttempts: \"3\"\n", wantPath: "targets[0].retry.maxAttempts"},
		{name: "fractional retry attempts", fragment: "    retry:\n      maxAttempts: 3.7\n", wantPath: "targets[0].retry.maxAttempts"},
		{name: "invalid rate limit type", fragment: "    rateLimit:\n      requestsPerSecond: \"10\"\n", wantPath: "targets[0].rateLimit.requestsPerSecond"},
		{name: "fractional rate limit burst", fragment: "    rateLimit:\n      burst: 0.5\n", wantPath: "targets[0].rateLimit.burst"},
		{name: "port", fragment: "    port: 8443\n", wantPath: "targets[0].port"},
		{name: "base path", fragment: "    basePath: /v2\n", wantPath: "targets[0].basePath"},
		{
			name: "local authentication reference",
			fragment: `    auth:
      type: bearer
      secretRef:
        envVar: API_TOKEN
`,
			wantPath: "targets[0].auth",
		},
		{
			name: "circuit breaker success threshold",
			fragment: `    circuitBreaker:
      enabled: true
      failureThreshold: 3
      successThreshold: 2
      resetTimeout: 30s
`,
			wantPath: "targets[0].circuitBreaker.successThreshold",
		},
		{
			name: "disabled circuit breaker settings",
			fragment: `    circuitBreaker:
      enabled: false
      failureThreshold: 3
`,
			wantPath: "targets[0].circuitBreaker.failureThreshold",
		},
		{
			name: "enabled circuit breaker without threshold",
			fragment: `    circuitBreaker:
      enabled: true
`,
			wantPath: "targets[0].circuitBreaker.failureThreshold",
		},
		{
			name: "retry backoff without attempts",
			fragment: `    retry:
      backoff: exponential
`,
			wantPath: "targets[0].retry.backoff",
		},
		{
			name: "retry timing",
			fragment: `    retry:
      maxAttempts: 3
      backoff: exponential
      initialInterval: 200ms
`,
			wantPath: "targets[0].retry.initialInterval",
		},
		{
			name: "rate limit",
			fragment: `    rateLimit:
      requestsPerSecond: 10
      burst: 20
`,
			wantPath: "targets[0].rateLimit",
		},
		{
			name: "interceptors",
			fragment: `    interceptors:
      - type: wasm
        config:
          module: auth.wasm
`,
			wantPath: "targets[0].interceptors",
		},
		{
			name: "Kafka target configuration",
			fragment: `    kafka:
      cluster: main
      topic: events
kafka:
  clusters:
    main:
      brokers: [broker:9092]
`,
			wantPath: "targets[0].kafka",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetName := tt.targetName
			if targetName == "" {
				targetName = "api"
			}
			linkYAML := "targets:\n  - name: " + targetName + "\n    protocol: https\n    host: api.example.com\n" + tt.fragment
			fisoDir := writeExportFixture(t, "", linkYAML)
			var buf bytes.Buffer
			err := RunExport([]string{fisoDir}, &buf)
			if err == nil {
				t.Fatal("expected lossy export to fail")
			}
			if !strings.Contains(err.Error(), tt.wantPath) {
				t.Fatalf("expected error to name %q, got %v", tt.wantPath, err)
			}
			if buf.Len() != 0 {
				t.Fatalf("expected no partial output, got %q", buf.String())
			}
		})
	}
}

func TestRunExport_ExportsAlternateLinkConfigPaths(t *testing.T) {
	for _, name := range []string{"link-config.yaml", "links.yaml"} {
		t.Run(name, func(t *testing.T) {
			fisoDir := filepath.Join(t.TempDir(), "fiso")
			if err := os.MkdirAll(fisoDir, 0755); err != nil {
				t.Fatal(err)
			}
			linkYAML := `targets:
  - name: api
    protocol: https
    host: api.example.com
`
			if err := os.WriteFile(filepath.Join(fisoDir, name), []byte(linkYAML), 0644); err != nil {
				t.Fatal(err)
			}

			var buf bytes.Buffer
			if err := RunExport([]string{fisoDir}, &buf); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(buf.String(), "kind: LinkTarget") {
				t.Fatalf("expected alternate Link config to export, got %q", buf.String())
			}
		})
	}
}

func TestRunExport_RejectsMultipleLinkConfigPaths(t *testing.T) {
	fisoDir := filepath.Join(t.TempDir(), "fiso")
	linkDir := filepath.Join(fisoDir, "link")
	if err := os.MkdirAll(linkDir, 0755); err != nil {
		t.Fatal(err)
	}
	linkYAML := []byte("targets:\n  - name: api\n    protocol: https\n    host: api.example.com\n")
	if err := os.WriteFile(filepath.Join(linkDir, "config.yaml"), linkYAML, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fisoDir, "links.yaml"), linkYAML, 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := RunExport([]string{fisoDir}, &buf)
	if err == nil {
		t.Fatal("expected ambiguous Link configs to fail")
	}
	if !strings.Contains(err.Error(), "multiple link configs") {
		t.Fatalf("expected ambiguous Link config error, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestRunExport_LossyResourceProducesNoPartialOutput(t *testing.T) {
	flowYAML := `name: representable-flow
source:
  type: grpc
  config:
    listenAddr: ":8081"
sink:
  type: http
  config:
    url: http://api:8080
`
	linkYAML := `targets:
  - name: lossy-link
    protocol: https
    host: api.example.com
    port: 8443
`
	fisoDir := writeExportFixture(t, flowYAML, linkYAML)

	var buf bytes.Buffer
	err := RunExport([]string{fisoDir}, &buf)
	if err == nil {
		t.Fatal("expected lossy link to fail the combined export")
	}
	if !strings.Contains(err.Error(), "targets[0].port") {
		t.Fatalf("expected error to name targets[0].port, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected all-or-nothing output, got %q", buf.String())
	}
}

func writeExportFixture(t *testing.T, flowYAML, linkYAML string) string {
	t.Helper()

	fisoDir := filepath.Join(t.TempDir(), "fiso")
	if flowYAML != "" {
		flowsDir := filepath.Join(fisoDir, "flows")
		if err := os.MkdirAll(flowsDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(flowsDir, "flow.yaml"), []byte(flowYAML), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if linkYAML != "" {
		linkDir := filepath.Join(fisoDir, "link")
		if err := os.MkdirAll(linkDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(linkDir, "config.yaml"), []byte(linkYAML), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return fisoDir
}
