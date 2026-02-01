# Fiso

Cloud-native event mediation runtime that decouples application business logic from external infrastructure dependencies. Fiso standardizes inbound events via **Fiso-Flow** and abstracts outbound dependencies via **Fiso-Link**, managed at scale by the **Fiso-Operator**.

## Why Fiso

Most applications spend significant code on things that aren't business logic: connecting to message brokers, calling external APIs, managing auth tokens, implementing retry loops, wiring circuit breakers. This code is tedious, error-prone, and creates tight coupling between your application and the infrastructure it runs on.

Fiso is built on two principles:

**1. Abstract every external dependency.** Your application should never directly interact with anything outside its own process — not Kafka, not Stripe, not Salesforce, not any message broker or third-party API. Every external dependency, whether infrastructure or service, is mediated through a local interface that Fiso provides. Inbound events arrive as transformed CloudEvents on a local endpoint. Outbound requests go through `localhost:3500/link/{target}`. Your app doesn't import broker clients or embed API SDKs.

**2. Invert every integration.** Instead of your application reaching out to external systems, Fiso inverts the relationship. Your app depends on stable local interfaces. The concrete details — which broker, which API endpoint, what auth method, what retry policy — are declared in configuration and managed by the runtime. Swap Kafka for gRPC ingestion, change an API provider, rotate credentials — all through config changes, zero application code touched, no redeployment of your service.

The result: your app talks to localhost, Fiso talks to the world. Infrastructure and external services become pluggable, observable, and independently evolvable.

## Architecture

```
                         ┌──────────────────────────────────────────┐
                         │              Fiso-Flow                   │
  Kafka / gRPC ─────────▶│  Source → CEL Transform → CloudEvent → Sink │──▶ HTTP / gRPC / Temporal
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

## Quick Start

### Prerequisites

- Go 1.25+
- Docker (for container builds and local dev)

### Build

```bash
# Build all binaries
make build-all

# Or individually
make build           # fiso-flow
make build-link      # fiso-link
make build-operator  # fiso-operator
```

### Run Locally

```bash
# Fiso-Flow
export FISO_CONFIG_DIR=./configs/examples
export FISO_METRICS_ADDR=:9090
./bin/fiso-flow

# Fiso-Link
export FISO_LINK_CONFIG=./configs/examples/link-targets.yaml
./bin/fiso-link

# Fiso-Operator
export FISO_WEBHOOK_ADDR=:8443
export FISO_HEALTH_ADDR=:9090
./bin/fiso-operator
```

### Docker

```bash
# Build all images
make docker-all

# Or individually
make docker-flow
make docker-link
make docker-operator
```

### Docker Compose (Local Dev)

Starts Kafka (KRaft mode), fiso-flow, and an echo server as HTTP sink:

```bash
make compose-up

# With Prometheus monitoring
docker compose --profile monitoring up -d

# Tear down
make compose-down
```

## Components

### Fiso-Flow — Inbound Event Pipeline

Consumes events from message brokers, transforms them using CEL expressions, wraps them in [CloudEvents v1.0](https://cloudevents.io/) format, and delivers them to configured sinks.

#### Sources

- **Kafka** — Consumer group-based consumption via [franz-go](https://github.com/twmb/franz-go). Supports `earliest`/`latest` start offset. At-least-once delivery with manual offset commits.
- **gRPC** — Streaming gRPC source for push-based event ingestion.

#### Transform

- **CEL** — [Common Expression Language](https://github.com/google/cel-go) from Google. Type-checked at compile time, sandboxed execution with configurable timeout (default 5s) and max output size (default 1MB). Available variables: `data`, `time`, `source`, `type`, `id`, `subject`.

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

Manages Fiso CRDs and automates sidecar injection.

- **CRD Management** — Reconciles `FlowDefinition` and `LinkTarget` custom resources.
- **Sidecar Injection** — Mutating webhook automatically injects fiso-link sidecar when Pod annotation `fiso.io/inject: "true"` is present.

## Configuration

### Flow Definition (fiso-flow)

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

### Link Targets (fiso-link)

```yaml
listenAddr: "127.0.0.1:3500"
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
| `FISO_WEBHOOK_ADDR` | `:8443` | Mutating webhook server address |
| `FISO_HEALTH_ADDR` | `:9090` | Health check server address |
| `FISO_LINK_IMAGE` | `ghcr.io/lsm/fiso-link:latest` | Sidecar container image |

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

### Install CRDs

```bash
kubectl apply -f deploy/crds/
```

### Example FlowDefinition CR

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

### Sidecar Injection

Add the annotation to any Pod to get fiso-link injected automatically:

```yaml
metadata:
  annotations:
    fiso.io/inject: "true"
```

See `deploy/examples/` for complete examples.

## Development

### Project Structure

```
api/v1alpha1/               CRD type definitions
cmd/
  fiso-flow/                 Flow pipeline entry point
  fiso-link/                 Link proxy entry point
  fiso-operator/             K8s operator entry point
internal/
  config/                    YAML config loading + hot-reload (fsnotify)
  dlq/                       Dead Letter Queue handler
  interceptor/               Request interceptors (gRPC, WASM)
  link/
    async/                   Async message publishing
    auth/                    Auth credential providers
    circuitbreaker/          Circuit breaker implementation
    discovery/               Target discovery (DNS)
    proxy/                   HTTP reverse proxy handler
    retry/                   Retry with backoff
  observability/             Metrics, logging, health endpoints
  operator/
    webhook/                 Mutating admission webhook
  pipeline/                  Pipeline orchestrator (source → transform → sink)
  schema/                    Schema registry integration
  sink/
    grpc/                    gRPC sink
    http/                    HTTP sink
    temporal/                Temporal workflow sink
  source/
    grpc/                    gRPC streaming source
    kafka/                   Kafka consumer source
  transform/
    cel/                     CEL expression transformer
configs/
  compose/                   Docker Compose flow configs
  examples/                  Example configurations
deploy/
  crds/                      CustomResourceDefinition manifests
  examples/                  Example K8s deployments
```

### Testing

```bash
make test           # Run all tests with race detection
make coverage-check # Enforce 95% coverage threshold
```

### Linting & Checks

```bash
make lint           # golangci-lint
make checks         # gofmt + go mod tidy + govulncheck
```

### CI

GitHub Actions runs on every push and PR to `main`:

| Job | Description |
|-----|-------------|
| **test** | `go test -race` with 95% coverage gate |
| **lint** | golangci-lint v2 |
| **checks** | gofmt, go mod tidy, go mod verify, govulncheck |
| **build** | Compile all 3 binaries |

### Release

Releases are automated with [GoReleaser](https://goreleaser.com/). Push a version tag to trigger:

```bash
git tag v0.1.0
git push origin v0.1.0
```

This builds cross-platform binaries (linux/darwin, amd64/arm64), multi-arch Docker images pushed to `ghcr.io/lsm/fiso-{flow,link,operator}`, and creates a GitHub release with changelog.

## License

See [LICENSE](LICENSE) for details.
