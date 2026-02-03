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
    2) CEL expression
    3) YAML field mapping
  Choose [1]: 3

Customize CloudEvents envelope fields? [y/N]: y

Fiso initialized with kafka source → http sink.
```

This generates a customized project scaffold. Use `fiso init --defaults` to skip prompts (HTTP source → HTTP sink, no transform).

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

If you selected Kafka, publish events directly:

```bash
echo '{"order_id": "12345", "amount": 99.99}' | \
  docker compose -f fiso/docker-compose.yml exec -T kafka \
  /opt/kafka/bin/kafka-console-producer.sh \
  --bootstrap-server localhost:9092 --topic my-events
```

#### Temporal Sink

If you selected Temporal, events are delivered as Temporal workflow executions. The scaffolded `temporal-worker/` contains an example workflow that processes events and calls external services via fiso-link.

View workflow executions in the Temporal UI at `http://localhost:8233`.

### Validate

```bash
fiso validate
```

Checks your flow definitions and link configuration for errors before running.

## Architecture

```
                         ┌──────────────────────────────────────────┐
                         │              Fiso-Flow                   │
  HTTP / Kafka / gRPC ──▶│  Source → Transform → CloudEvent → Sink    │──▶ HTTP / gRPC / Temporal
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

Consumes events from sources, optionally transforms them using CEL expressions, wraps them in [CloudEvents v1.0](https://cloudevents.io/) format, and delivers them to configured sinks.

#### Sources

- **HTTP** — Synchronous request-response ingestion. Listens on a configurable address and path, forwards events to the sink, and returns the sink's response to the caller.
- **Kafka** — Consumer group-based consumption via [franz-go](https://github.com/twmb/franz-go). Supports `earliest`/`latest` start offset. At-least-once delivery with manual offset commits.
- **gRPC** — Streaming gRPC source for push-based event ingestion.

#### Transform

- **CEL** — [Common Expression Language](https://github.com/google/cel-go) from Google. Type-checked at compile time, sandboxed execution with configurable timeout (default 5s) and max output size (default 1MB). Available variables: `data`, `time`, `source`, `type`, `id`, `subject`.
- **Mapping** — Declarative YAML field mapping using JSONPath dot-notation (`$.field.nested`). Maps input fields to output fields with static values and dynamic path resolution. Mutually exclusive with CEL.

#### CloudEvents Customization

CloudEvents envelope fields (`type`, `source`, `subject`) can be customized per flow. Values starting with `$.` are resolved as JSONPath expressions against the original input event (before transforms).

#### Sinks

- **HTTP** — Delivers events via HTTP with exponential backoff retry. Distinguishes retryable errors (5xx, 429) from permanent failures (4xx).
- **gRPC** — Delivers events via gRPC streaming.
- **Temporal** — Starts Temporal workflows for long-running event processing.

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
  cel: '{"order_id": data.legacy_id, "timestamp": time, "status": data.order_status}'
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

Mapping transform with CloudEvents customization:

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
  mapping:
    order_id: "$.legacy_id"
    total: "$.amount"
    customer: "$.customer_name"
    status: "pending"
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
    temporal/                Temporal workflow sink
  source/
    grpc/                    gRPC streaming source
    http/                    HTTP request-response source
    kafka/                   Kafka consumer source
  transform/
    cel/                     CEL expression transformer
    mapping/                 YAML field mapping transformer
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

## License

See [LICENSE](LICENSE) for details.
