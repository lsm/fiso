# Fiso

Cloud-native event mediation runtime that decouples application business logic from external infrastructure dependencies. Fiso standardizes inbound events via **Fiso-Flow** and abstracts outbound dependencies via **Fiso-Link**, managed at scale by the **Fiso-Operator**.

## Why Fiso

Most applications spend significant code on things that aren't business logic: connecting to message brokers, calling external APIs, managing auth tokens, implementing retry loops, wiring circuit breakers. This code is tedious, error-prone, and creates tight coupling between your application and the infrastructure it runs on.

Fiso is built on two principles:

**1. Abstract every external dependency.** Your application should never directly interact with anything outside its own process — not Kafka, not Stripe, not Salesforce, not any message broker or third-party API. Every external dependency, whether infrastructure or service, is mediated through a local interface that Fiso provides. Inbound events arrive as transformed CloudEvents on a local endpoint. Outbound requests go through `localhost:3500/link/{target}`. Your app doesn't import broker clients or embed API SDKs.

**2. Invert every integration.** Instead of your application reaching out to external systems, Fiso inverts the relationship. Your app depends on stable local interfaces. The concrete details — which broker, which API endpoint, what auth method, what retry policy — are declared in configuration and managed by the runtime. Swap Kafka for HTTP ingestion, change an API provider, rotate credentials — all through config changes, zero application code touched, no redeployment of your service.

The result: your app talks to localhost, Fiso talks to the world. Infrastructure and external services become pluggable, observable, and independently evolvable.

## Quick Start

### Install

```bash
curl -fsSL https://raw.githubusercontent.com/lsm/fiso/main/install.sh | sh
```

Or with Go:

```bash
go install github.com/lsm/fiso/cmd/fiso@latest
```

### Create a Project

```bash
mkdir my-project && cd my-project
fiso init
```

`fiso init` walks you through an interactive setup:

```
$ fiso init

Source type:
  ▸ 1) HTTP
    2) Kafka
  Choose [1]: 2

Sink type:
  ▸ 1) HTTP
    2) Temporal
  Choose [1]: 1

Transform:
  ▸ 1) None
    2) Field-based transform (CEL expressions)
  Choose [1]: 2

Customize CloudEvents envelope fields? [y/N]: y

Include Kubernetes deployment manifests? [y/N]: n

Fiso initialized with kafka source → http sink.
```

This generates a customized project scaffold. Use `fiso init --defaults` to skip prompts (HTTP source → HTTP sink, no transform).

You can also use flags for non-interactive setup:

```bash
fiso init --source kafka --sink http --transform fields --cloudevents --k8s
```

Default scaffold (HTTP → HTTP):

```
fiso/
├── docker-compose.yml
├── prometheus.yml
├── flows/
│   └── example-flow.yaml
├── link/
│   └── config.yaml
└── user-service/
    ├── main.go
    ├── Dockerfile
    └── go.mod
```

Kafka → Temporal scaffold:

```
fiso/
├── docker-compose.yml        # Includes Kafka + Temporal services
├── prometheus.yml
├── flows/
│   └── kafka-temporal-flow.yaml
├── link/
│   └── config.yaml
└── temporal-worker/           # Temporal workflow worker
    ├── main.go
    ├── workflow.go
    ├── activity.go
    ├── Dockerfile
    └── go.mod
```

### Run

```bash
fiso dev
```

By default, `fiso dev` runs in **hybrid mode**: Fiso infrastructure runs in Docker while your service runs on the host for fast iteration with live reload.

```bash
# Terminal 1: Start Fiso infrastructure
fiso dev

# Terminal 2: Run your service on host
cd fiso/user-service && go run .
```

To run everything in Docker (including your service):

```bash
fiso dev --docker
```

Send a test event:

```bash
curl -X POST http://localhost:8081/ingest \
  -H "Content-Type: application/json" \
  -d '{"order_id": "12345", "amount": 99.99}'
```

The request flows through the full chain:

```
curl → fiso-flow(:8081) → user-service(host:8082) → fiso-link(:3500) → external-api
```

#### Kafka Source

If you selected Kafka, use the `fiso produce` command:

```bash
# Produce a single event
fiso produce --topic orders --json '{"order_id": "12345", "amount": 99.99}'

# Produce from a file
fiso produce --topic orders --file sample-order.json

# Produce multiple events with rate limiting
fiso produce --topic orders --count 10 --rate 100ms --file orders.jsonl
```

#### View Service Logs

View logs from fiso services:

```bash
# Show last 100 lines of fiso-flow logs
fiso logs

# Follow logs in real-time
fiso logs --follow

# Show logs for a specific service
fiso logs --service fiso-link

# Show more lines
fiso logs --tail 500
```

#### Temporal Sink

If you selected Temporal, events are delivered as Temporal workflow executions. The scaffolded `temporal-worker/` contains an example workflow that processes events and calls external services via fiso-link.

View workflow executions in the Temporal UI at `http://localhost:8233`.

### Validate

```bash
fiso validate
```

Checks your flow definitions and link configuration for errors before running.

### Test Transforms

Test your transform configurations without starting the full stack:

```bash
# Test with inline JSON
fiso transform test --flow fiso/flows/order-flow.yaml --input '{"order_id":"TEST-001","customer_id":"CUST-123"}'

# Test with a JSON file
fiso transform test --flow fiso/flows/order-flow.yaml --input sample-order.json
```

### Produce and Consume Kafka Events

```bash
# Produce test events to Kafka
fiso produce --topic orders --json '{"order_id":"12345","amount":99.99}'

# Consume and view events from Kafka
fiso consume --topic orders --max-messages 10

# Follow Kafka topic in real-time
fiso consume --topic orders --follow

# Consume from the beginning
fiso consume --topic orders --from-beginning --max-messages 100
```

### Check Environment Health

```bash
fiso doctor
```

Verifies Docker installation, project structure, config validity, and port availability.

## Architecture

```
                         ┌──────────────────────────────────────────┐
                         │              Fiso-Flow                   │
  HTTP / Kafka / gRPC ──▶│  Source → Transform → CloudEvent → Sink    │──▶ HTTP / gRPC / Temporal / Kafka
                         │                                    ↓    │
                         │                                   DLQ   │
                         └──────────────────────────────────────────┘

                         ┌──────────────────────────────────────────┐
  Application ──────────▶│              Fiso-Link                   │
  localhost:3500/link/…  │  Auth → Circuit Breaker → Retry → Proxy │──▶ External APIs
                         └──────────────────────────────────────────┘

                         ┌──────────────────────────────────────────┐
                         │            Fiso-Operator                 │
                         │  CRD Reconciler + Sidecar Injection      │
                         └──────────────────────────────────────────┘
```

## Components

### Fiso-Flow — Inbound Event Pipeline

Consumes events from sources, optionally transforms them using a unified fields-based transform system, wraps them in [CloudEvents v1.0](https://cloudevents.io/) format, and delivers them to configured sinks.

#### Sources

- **HTTP** — Synchronous request-response ingestion. Listens on a configurable address and path, forwards events to the sink, and returns the sink's response to the caller.
- **Kafka** — Consumer group-based consumption via [franz-go](https://github.com/twmb/franz-go). Supports `earliest`/`latest` start offset. At-least-once delivery with manual offset commits.
- **gRPC** — Streaming gRPC source for push-based event ingestion.

#### Transform

Fiso uses a **unified transform system** that compiles to optimized CEL (Common Expression Language) expressions under the hood. Define transforms using a `fields` map where each value is a CEL expression:

```yaml
transform:
  fields:
    order_id: "data.legacy_id"           # Field mapping
    total: "data.price * data.quantity"  # Arithmetic
    status: '"pending"'                  # Static string (quoted)
    timestamp: "time"                    # CloudEvents variable
```

**Features:**
- Field mapping: `"field": "data.nested.path"`
- Arithmetic: `"total": "data.price * data.quantity"`
- Conditionals: `"category": 'data.type == "premium" ? "gold" : "standard"'`
- String operations: `"fullName": 'data.first + " " + data.last'`
- Nested objects: `"customer": '{"id": data.id, "name": data.name}'`
- Static literals: Strings must be quoted, numbers and booleans are unquoted

**Available variables:** `data`, `time`, `source`, `type`, `id`, `subject`

**Performance:** 60% faster than the previous CEL implementation through compiled optimization and direct evaluation (no per-event goroutines).

#### CloudEvents Customization

CloudEvents envelope fields (`type`, `source`, `subject`) can be customized per flow. Values starting with `$.` are resolved as JSONPath expressions against the original input event (before transforms).

#### Sinks

- **HTTP** — Delivers events via HTTP with exponential backoff retry. Distinguishes retryable errors (5xx, 429) from permanent failures (4xx).
- **gRPC** — Delivers events via gRPC streaming.
- **Temporal** — Starts Temporal workflows for long-running event processing.
- **Kafka** — Produces events to Kafka topics with at-least-once delivery guarantees.

#### Dead Letter Queue

Failed events are published to a DLQ topic (`fiso-dlq-{flowName}`) with structured error metadata:

| Header | Description |
|--------|-------------|
| `fiso-original-topic` | Source topic |
| `fiso-error-code` | `TRANSFORM_FAILED`, `SINK_DELIVERY_FAILED`, etc. |
| `fiso-error-message` | Human-readable error |
| `fiso-retry-count` | Retries attempted |
| `fiso-failed-at` | Failure timestamp |
| `fiso-flow-name` | Flow name |

### Fiso-Link — Outbound Proxy

Reverse proxy sidecar that routes application requests to external services through `localhost:3500/link/{target}/{path}`.

#### Features

- **Routing** — Path-based routing via `/link/{target}/{path}` with configurable allowed paths per target.
- **Authentication** — Automatic credential injection (Bearer, API Key, Basic). Sources: K8s Secrets (file/env), Vault.
- **Circuit Breaker** — Per-target circuit breaker with configurable failure threshold, success threshold, and reset timeout.
- **Retry** — Configurable retry with exponential/constant/linear backoff, jitter, and max interval.
- **Discovery** — DNS-based target resolution.
- **Async Mode** — Publish to Kafka for async delivery via configured brokers.

### Fiso-Operator — Kubernetes Controller

Manages Fiso CRDs and automates sidecar injection. Built with [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime).

- **CRD Reconciliation** — Reconciles `FlowDefinition` and `LinkTarget` custom resources. Validates specs and updates `.status.phase` to `Ready` or `Error`.
- **Sidecar Injection** — Mutating webhook automatically injects fiso-link sidecar when Pod annotation `fiso.io/inject: "true"` is present.
- **Modes** — `controller` (default): full controller + webhook. `webhook-only`: runs only the sidecar injection webhook (`FISO_OPERATOR_MODE=webhook-only`).

## Configuration

### Flow Definition (fiso-flow)

HTTP source example (used by `fiso init`):

```yaml
name: example-flow
source:
  type: http
  config:
    listenAddr: ":8081"
    path: /ingest
sink:
  type: http
  config:
    url: http://user-service:8082
    method: POST
```

Kafka source example:

```yaml
name: order-events
source:
  type: kafka
  config:
    brokers:
      - kafka.infra.svc:9092
    topic: orders
    consumerGroup: fiso-order-flow
    startOffset: latest
transform:
  fields:
    order_id: "data.legacy_id"
    timestamp: "time"
    status: "data.order_status"
sink:
  type: http
  config:
    url: http://order-service:8080/callbacks/order-result
    method: POST
errorHandling:
  deadLetterTopic: fiso-dlq-order-events
  maxRetries: 5
  backoff: exponential
```

Fiso watches the config directory and hot-reloads on changes.

Kafka sink example:

```yaml
name: order-results
source:
  type: http
  config:
    listenAddr: ":8081"
    path: /ingest
transform:
  fields:
    order_id: "data.id"
    result: "data.status"
    timestamp: "time"
sink:
  type: kafka
  config:
    brokers:
      - kafka.infra.svc:9092
    topic: order-results
errorHandling:
  deadLetterTopic: fiso-dlq-order-results
  maxRetries: 3
```

Transform with CloudEvents customization:

```yaml
name: order-pipeline
source:
  type: kafka
  config:
    brokers:
      - kafka.infra.svc:9092
    topic: orders
    consumerGroup: fiso-order-flow
    startOffset: latest
transform:
  fields:
    order_id: "data.legacy_id"
    total: "data.amount"
    customer: "data.customer_name"
    status: '"pending"'
cloudevents:
  type: order.created
  source: order-service
  subject: "$.legacy_id"
sink:
  type: temporal
  config:
    hostPort: temporal:7233
    taskQueue: order-processing
    workflowType: ProcessOrder
errorHandling:
  deadLetterTopic: fiso-dlq-order-pipeline
  maxRetries: 3
```

### Link Targets (fiso-link)

```yaml
listenAddr: ":3500"
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
      initialInterval: "200ms"
      maxInterval: "30s"
      jitter: 0.2
    allowedPaths:
      - /api/v2/**

asyncBrokers:
  - kafka.infra.svc:9092
```

## WASM Interceptors

WASM interceptors enable custom data transformations using WebAssembly modules. They operate as stdin-to-stdout JSON pipelines, reading CloudEvents from stdin and writing transformed CloudEvents to stdout.

### How It Works

1. Fiso-flow receives an event and applies unified transforms (fields-based CEL expressions)
2. Before sending to the sink, the event is piped to the WASM module via stdin
3. The WASM module reads JSON from stdin, transforms it, and writes JSON to stdout
4. Fiso-flow receives the transformed event and delivers it to the sink

### Example: Go WASM Interceptor

Create a simple uppercase transform:

```go
// transform.go
package main

import (
    "encoding/json"
    "fmt"
    "io"
    "os"
    "strings"
)

type Event struct {
    Data map[string]interface{} `json:"data"`
}

func main() {
    input, _ := io.ReadAll(os.Stdin)

    var event Event
    json.Unmarshal(input, &event)

    // Transform: uppercase all string values
    for k, v := range event.Data {
        if s, ok := v.(string); ok {
            event.Data[k] = strings.ToUpper(s)
        }
    }

    output, _ := json.Marshal(event)
    fmt.Println(string(output))
}
```

Compile to WASM:

```bash
GOOS=wasip1 GOARCH=wasm go build -o transform.wasm .
```

### Flow Configuration

Reference the WASM module in your flow definition:

```yaml
name: wasm-transform-flow
source:
  type: http
  config:
    listenAddr: ":8081"
    path: /ingest
interceptors:
  - type: wasm
    config:
      module: /etc/fiso/wasm/transform.wasm
      timeout: "5s"
sink:
  type: http
  config:
    url: http://user-service:8082
    method: POST
```

### Supported Languages

- **Go** — Native support via `GOOS=wasip1 GOARCH=wasm`
- **Rust** — Compile with `wasm32-wasi` target
- **TinyGo** — Smaller binaries: `tinygo build -target=wasi -o transform.wasm .`
- **C** — Compile with `wasi-sdk`

### Local Testing

Test your WASM module before deploying:

```bash
echo '{"data":{"key":"value"}}' | wasmtime transform.wasm
```

Expected output:

```json
{"data":{"key":"VALUE"}}
```

### Environment Variables

#### fiso-flow

| Variable | Default | Description |
|----------|---------|-------------|
| `FISO_CONFIG_DIR` | `/etc/fiso/flows` | Directory containing flow YAML files |
| `FISO_METRICS_ADDR` | `:9090` | Metrics and health HTTP server address |

#### fiso-link

| Variable | Default | Description |
|----------|---------|-------------|
| `FISO_LINK_CONFIG` | `/etc/fiso/link/config.yaml` | Path to link targets config file |

#### fiso-operator

| Variable | Default | Description |
|----------|---------|-------------|
| `FISO_OPERATOR_MODE` | `controller` | `controller` (full) or `webhook-only` |
| `FISO_METRICS_ADDR` | `:8080` | Metrics endpoint (controller mode) |
| `FISO_HEALTH_ADDR` | `:9090` | Health check server address |
| `FISO_ENABLE_LEADER_ELECTION` | `false` | Enable leader election for HA |
| `FISO_LINK_IMAGE` | `ghcr.io/lsm/fiso-link:latest` | Sidecar container image |
| `FISO_WEBHOOK_ADDR` | `:8443` | Mutating webhook server address (webhook-only mode) |
| `FISO_TLS_CERT_FILE` | `/etc/fiso/tls/tls.crt` | TLS certificate for webhook server |
| `FISO_TLS_KEY_FILE` | `/etc/fiso/tls/tls.key` | TLS private key for webhook server |

## Observability

### Fiso-Flow Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `fiso_flow_events_total` | Counter | `flow`, `status` | Total events processed |
| `fiso_flow_event_duration_seconds` | Histogram | `flow`, `phase` | Processing duration |
| `fiso_flow_consumer_lag` | Gauge | `flow`, `partition` | Consumer lag |
| `fiso_flow_transform_errors_total` | Counter | `flow`, `error_type` | Transform failures |
| `fiso_flow_dlq_total` | Counter | `flow` | Events sent to DLQ |
| `fiso_flow_sink_delivery_errors_total` | Counter | `flow` | Sink delivery failures |

### Fiso-Link Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `fiso_link_requests_total` | Counter | `target`, `method`, `status`, `mode` | Total requests proxied |
| `fiso_link_request_duration_seconds` | Histogram | `target`, `method` | Request duration |
| `fiso_link_circuit_state` | Gauge | `target` | Circuit breaker state (0=closed, 1=half-open, 2=open) |
| `fiso_link_retries_total` | Counter | `target`, `attempt` | Total retries per target |
| `fiso_link_auth_refresh_total` | Counter | `target`, `status` | Auth credential refreshes |

### Health Endpoints

All components expose health endpoints on their metrics port:

| Endpoint | Description |
|----------|-------------|
| `GET /healthz` | Liveness probe — always returns `200 OK` |
| `GET /readyz` | Readiness probe — `200 OK` when ready, `503` otherwise |
| `GET /metrics` | Prometheus metrics |

### Logging

Structured JSON logging via Go's `log/slog`.

## Kubernetes

### Prerequisites

- Kubernetes 1.27+
- `kubectl` configured with cluster-admin access (for CRD and ClusterRole installation)

### Install CRDs

```bash
kubectl apply -f deploy/crds/
kubectl wait --for=condition=Established crd/flowdefinitions.fiso.io --timeout=30s
kubectl wait --for=condition=Established crd/linktargets.fiso.io --timeout=30s
```

This installs two CRDs:

| CRD | Group | Kind | Scope |
|-----|-------|------|-------|
| `flowdefinitions.fiso.io` | `fiso.io/v1alpha1` | `FlowDefinition` | Namespaced |
| `linktargets.fiso.io` | `fiso.io/v1alpha1` | `LinkTarget` | Namespaced |

### Deploy the Operator

#### 1. Create Namespace

```bash
kubectl create namespace fiso-system
```

#### 2. Apply RBAC

```bash
kubectl apply -f deploy/rbac/service_account.yaml
kubectl apply -f deploy/rbac/role.yaml
kubectl apply -f deploy/rbac/role_binding.yaml
```

This creates:

| Resource | Name | Scope | Description |
|----------|------|-------|-------------|
| ServiceAccount | `fiso-operator` | `fiso-system` | Identity for the operator pod |
| ClusterRole | `fiso-operator` | Cluster-wide | Permissions for CRD reconciliation, webhook, and leader election |
| ClusterRoleBinding | `fiso-operator` | Cluster-wide | Binds the ServiceAccount to the ClusterRole |

A **ClusterRole** (not a namespaced Role) is required because the operator reconciles CRDs across all namespaces.

#### 3. Create TLS Secret (for webhook)

The mutating webhook requires TLS. Generate a self-signed certificate or use cert-manager:

```bash
openssl req -x509 -newkey rsa:2048 -keyout tls.key -out tls.crt \
    -days 365 -nodes \
    -subj "/CN=fiso-operator.fiso-system.svc" \
    -addext "subjectAltName=DNS:fiso-operator.fiso-system.svc,DNS:fiso-operator.fiso-system.svc.cluster.local"

kubectl create secret tls fiso-operator-tls \
    --cert=tls.crt --key=tls.key -n fiso-system
```

#### 4. Deploy

```bash
kubectl apply -f deploy/operator/deployment.yaml
```

### Operator Permissions

The operator's ClusterRole grants the minimum permissions required at runtime:

#### CRD Reconciliation

| API Group | Resource | Verbs | Purpose |
|-----------|----------|-------|---------|
| `fiso.io` | `flowdefinitions` | get, list, watch | Informer cache (list, watch) and reconciler read (get) |
| `fiso.io` | `flowdefinitions/status` | get, update, patch | Write reconciliation status (`phase: Ready` or `Error`) |
| `fiso.io` | `linktargets` | get, list, watch | Informer cache (list, watch) and reconciler read (get) |
| `fiso.io` | `linktargets/status` | get, update, patch | Write reconciliation status |

The reconciler never creates, updates, or patches the main CRD resources — it only reads them and writes to the `/status` subresource.

#### Infrastructure

| API Group | Resource | Verbs | Purpose |
|-----------|----------|-------|---------|
| _(core)_ | `events` | create, patch | Controller-runtime event recorder |
| `coordination.k8s.io` | `leases` | get, list, watch, create, update, patch, delete | Leader election for HA deployments |

Leader election permissions are only used when `FISO_ENABLE_LEADER_ELECTION=true`. If running a single replica without HA, the `leases` rule can be removed.

#### Webhook Note

The mutating admission webhook does **not** require `pods` or `mutatingwebhookconfigurations` RBAC permissions. The Kubernetes API server sends the Pod object in the `AdmissionReview` request body — the webhook handler never queries the API to read pods.

### Example CRs

#### FlowDefinition

```yaml
apiVersion: fiso.io/v1alpha1
kind: FlowDefinition
metadata:
  name: order-events
spec:
  source:
    type: kafka
    config:
      brokers: "kafka.infra.svc:9092"
      topic: orders
      consumerGroup: fiso-order-flow
  sink:
    type: http
    config:
      url: "http://order-service:8080/callbacks/order-result"
```

#### LinkTarget

```yaml
apiVersion: fiso.io/v1alpha1
kind: LinkTarget
metadata:
  name: crm-api
spec:
  protocol: https
  host: api.salesforce.com
```

The operator validates specs and sets `.status.phase` to `Ready` or `Error` with a descriptive `.status.message`.

### Export Local Config to CRDs

Convert local flow/link YAML files to Kubernetes CRD manifests:

```bash
fiso export                              # Export from default fiso/ directory
fiso export --namespace=my-namespace     # Override namespace (default: fiso-system)
```

This generates `FlowDefinition` and `LinkTarget` CRs that can be applied with `kubectl apply`.

### Sidecar Injection

Add the annotation to any Pod to get fiso-link injected automatically:

```yaml
metadata:
  annotations:
    fiso.io/inject: "true"
```

The webhook injects a `fiso-link` sidecar container with ports `3500` (proxy) and `9090` (metrics). Once injected, it sets `fiso.io/status: "injected"` to prevent duplicate injection.

See `deploy/examples/` for complete examples.

## Troubleshooting

### Docker Not Found or Not Running

**Symptom:** `fiso dev` fails with "Cannot connect to the Docker daemon" or "docker: command not found"

**Solution:**

- Install Docker Desktop: https://www.docker.com/products/docker-desktop
- Start the Docker daemon (Docker Desktop application)
- Verify Docker is running: `docker ps`

### GHCR Permission Denied

**Symptom:** `Error response from daemon: pull access denied for ghcr.io/lsm/fiso-flow`

**Solution:**

```bash
docker logout ghcr.io
```

Fiso images are public and don't require authentication. Cached credentials may cause 403 errors.

### Port Conflicts

**Symptom:** `Bind for 0.0.0.0:8081 failed: port is already allocated`

Common conflicting ports:

- **8081** — fiso-flow HTTP ingestion
- **3500** — fiso-link proxy
- **9090** — fiso-flow metrics

**Solution:**

Find the process using the port:

```bash
lsof -i :8081
```

Kill the process or change the port in your flow/link config:

```yaml
source:
  config:
    listenAddr: ":8082"  # Use a different port
```

### Missing Project Structure

**Symptom:** `fiso dev` fails with "No docker-compose.yml found" or "Config directory not found"

**Solution:**

Run `fiso init` to create the required project scaffold:

```bash
fiso init
```

This generates `fiso/docker-compose.yml`, `fiso/flows/`, and `fiso/link/` directories.

### Config Validation Errors

**Symptom:** `Invalid flow configuration: missing required field 'source.type'`

**Solution:**

Run `fiso validate` to check your flow and link configs:

```bash
fiso validate
```

Fix errors reported and re-run. The validator checks YAML syntax, required fields, and type constraints.

### Service Connectivity Issues

**Symptom:** Events aren't reaching your service, or link proxy fails to connect to external APIs

**Solution:**

1. Check Docker network connectivity:

```bash
docker compose -f fiso/docker-compose.yml logs fiso-flow
docker compose -f fiso/docker-compose.yml logs fiso-link
```

2. Verify service endpoints in flow/link configs match Docker service names
3. For host-based services (hybrid mode), use `host.docker.internal` instead of `localhost` in flow sink URLs
4. Test connectivity from inside the container:

```bash
docker compose -f fiso/docker-compose.yml exec fiso-flow ping user-service
```

### Diagnostic Tool

For automated environment checks, use `fiso doctor`:

```bash
fiso doctor
```

This command:

- Verifies Docker is installed and running
- Checks for required project structure (`fiso/` directory)
- Validates all flow and link configurations
- Detects port conflicts (8081, 3500, 9090)
- Reports actionable error messages

## Development

### Prerequisites

- Go 1.25+
- Docker

### Build

```bash
make build-all    # All binaries (fiso-flow, fiso-link, fiso-operator, fiso CLI)
make build        # fiso-flow only
make build-link   # fiso-link only
make build-cli    # fiso CLI only
```

### Test

```bash
make test                # Unit tests with race detection
make test-integration    # Integration tests (requires Kafka)
make e2e-operator        # Operator E2E tests (requires kind + Docker)
make coverage-check      # Enforce 95% coverage threshold
```

### Lint & Checks

```bash
make lint     # golangci-lint
make checks   # gofmt + go mod tidy + govulncheck
```

### Project Structure

```
cmd/
  fiso/                      CLI entry point (init, dev, validate)
  fiso-flow/                 Flow pipeline entry point
  fiso-link/                 Link proxy entry point
  fiso-operator/             K8s operator entry point
internal/
  cli/                       CLI commands and templates
  config/                    YAML config loading + hot-reload (fsnotify)
  dlq/                       Dead Letter Queue handler
  link/
    auth/                    Auth credential providers
    circuitbreaker/          Circuit breaker implementation
    discovery/               Target discovery (DNS)
    proxy/                   HTTP reverse proxy handler
    retry/                   Retry with backoff
  observability/             Metrics, logging, health endpoints
  operator/
    controller/              FlowDefinition + LinkTarget reconcilers
    webhook/                 Mutating admission webhook
  pipeline/                  Pipeline orchestrator (source → transform → sink)
  sink/
    grpc/                    gRPC sink
    http/                    HTTP sink
    kafka/                   Kafka producer sink
    temporal/                Temporal workflow sink
  source/
    grpc/                    gRPC streaming source
    http/                    HTTP request-response source
    kafka/                   Kafka consumer source
  transform/
    unified/                 Unified fields-based transformer (CEL-compiled)
  jsonpath/                  Shared JSONPath resolver
api/v1alpha1/                CRD type definitions
deploy/
  crds/                      CustomResourceDefinition manifests
  examples/                  Example K8s deployments
test/
  e2e/
    http/                    HTTP flow E2E (Docker Compose)
    kafka/                   Kafka flow E2E (Docker Compose)
    kafka-temporal/          Kafka → Temporal E2E (Docker Compose)
    kafka-temporal-signal/   Kafka → Temporal signal E2E (Docker Compose)
    wasm/                    WASM interceptor E2E (Docker Compose)
    operator/                CRD operator E2E (kind cluster)
  integration/               Integration tests (Kafka)
```

### CI

GitHub Actions runs on every push and PR to `main`:

| Job | Description |
|-----|-------------|
| **test** | `go test -race` with 95% coverage gate |
| **lint** | golangci-lint v2 |
| **checks** | gofmt, go mod tidy, go mod verify, govulncheck |
| **build** | Compile all 4 binaries, upload artifacts |
| **integration** | Kafka integration tests |
| **e2e** | HTTP flow end-to-end test (Docker Compose) |
| **e2e-kafka** | Kafka flow end-to-end test (Docker Compose) |
| **e2e-kafka-temporal** | Kafka → Temporal full pipeline E2E (6-service Docker Compose) |
| **e2e-kafka-temporal-signal** | Kafka → Temporal signal mode E2E (6-service Docker Compose) |
| **e2e-wasm** | WASM interceptor E2E test (Docker Compose) |
| **e2e-operator** | CRD operator E2E test (kind cluster — CRD reconciliation, status updates) |
| **cli-smoke** | `fiso init --defaults` + `fiso validate` smoke test |

### Release

Releases are automated with [GoReleaser](https://goreleaser.com/). Push a version tag to trigger:

```bash
git tag v0.2.0
git push origin v0.2.0
```

This builds cross-platform binaries (linux/darwin, amd64/arm64), multi-arch Docker images pushed to `ghcr.io/lsm/fiso-{flow,link,operator}`, and creates a GitHub release with changelog.

## Migration Guide: v0.8.0

### Breaking Change: Unified Transform System

**v0.8.0** introduces a unified transform system that replaces the previous `cel:` and `mapping:` syntax with a single `fields:` approach. This is a **breaking change** — existing flow configurations must be updated.

The new system:
- Uses `fields:` map instead of `cel:` or `mapping:`
- Compiles all transforms to optimized CEL expressions internally
- Delivers 60% better performance than the old CEL implementation
- Provides all the power of CEL with simpler syntax

### Migrating from CEL Syntax

**Before (v0.7.x):**
```yaml
transform:
  cel: '{"order_id": data.legacy_id, "timestamp": time, "status": data.order_status}'
```

**After (v0.8.0+):**
```yaml
transform:
  fields:
    order_id: "data.legacy_id"
    timestamp: "time"
    status: "data.order_status"
```

### Migrating from Mapping Syntax

**Before (v0.7.x):**
```yaml
transform:
  mapping:
    order_id: "$.legacy_id"
    total: "$.amount"
    customer: "$.customer_name"
    status: "pending"
```

**After (v0.8.0+):**
```yaml
transform:
  fields:
    order_id: "data.legacy_id"
    total: "data.amount"
    customer: "data.customer_name"
    status: '"pending"'  # Static strings must be quoted
```

### Key Differences

1. **Field access:** Use `data.field` instead of `$.field`
2. **Static strings:** Must be double-quoted: `"pending"`, not `pending`
3. **No JSONPath:** The unified system uses CEL expressions only
4. **No mutual exclusivity:** All transforms support the full CEL feature set

### Advanced Examples

The new syntax supports all CEL operations:

```yaml
transform:
  fields:
    # Arithmetic
    total: "data.price * data.quantity"
    discounted: "data.price * data.quantity * 0.9"

    # Conditionals
    category: 'data.type == "premium" ? "gold" : "standard"'

    # String operations
    fullName: 'data.first_name + " " + data.last_name'
    email: 'data.username + "@" + data.domain'

    # Nested objects
    customer: '{"id": data.customer_id, "name": data.customer_name}'

    # Arrays
    tags: "[data.tag1, data.tag2, data.tag3]"

    # Boolean logic
    eligible: "data.age >= 18 && data.verified == true"
```

### Migration Checklist

- [ ] Replace all `cel:` with `fields:`
- [ ] Replace all `mapping:` with `fields:`
- [ ] Change `$.field` references to `data.field`
- [ ] Add quotes around static string literals: `"value"`
- [ ] Test updated configurations with `fiso validate`
- [ ] Run `fiso doctor` to check environment health

### Rollback

If you need to rollback to v0.7.x after upgrading:
1. Uninstall v0.8.0: `rm $(which fiso)`
2. Install v0.7.x: `go install github.com/lsm/fiso/cmd/fiso@v0.7.0`
3. Restore your old YAML configurations from version control

## License

See [LICENSE](LICENSE) for details.
