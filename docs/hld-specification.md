# Project Fiso: High-Level Design Specification

**Version:** 1.3.0
**Status:** DRAFT
**Core Philosophy:** "Infrastructure as an Implementation Detail."

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [System Architecture](#2-system-architecture)
3. [Data Flow Specifications](#3-data-flow-specifications)
4. [Configuration & Control Plane](#4-configuration--control-plane)
5. [Security Model](#5-security-model)
6. [Error Handling & Delivery Guarantees](#6-error-handling--delivery-guarantees)
7. [State Management](#7-state-management)
8. [Schema Evolution & Versioning](#8-schema-evolution--versioning)
9. [Integration Features ("The Imperfect World")](#9-integration-features-the-imperfect-world)
10. [Deployment Topology](#10-deployment-topology)
11. [Observability & Standards](#11-observability--standards)
12. [Phase 1 Implementation Plan](#12-phase-1-implementation-plan)
  - [12.3 CLI Commands](#123-cli-commands)
  - [12.4 Core Interfaces](#124-core-interfaces)
  - [12.5 Key Library Choices](#125-key-library-choices)
  - [12.6 Definition of Done](#126-definition-of-done-phase-1)

---

## 1. Executive Summary

Fiso is a cloud-native mediation runtime designed to isolate application business
logic from external infrastructure dependencies. It acts as a bidirectional buffer,
standardizing inbound events via **Fiso-Flow** and abstracting outbound dependencies
via **Fiso-Link**.

### Key Objectives

- **Decoupling:** Applications communicate only with local Fiso interfaces
  (HTTP/gRPC/SDK), never direct vendor SDKs.
- **Standardization:** All data in transit is normalized to CloudEvents (v1.0).
- **Resiliency:** Native support for retries, circuit breaking, and Temporal
  workflow triggers.
- **Adaptability:** Supports "Imperfect World" scenarios via declarative
  transformations and sidecar interceptors.
- **Flexibility:** Operates with or without Kubernetes CRDs (Custom Resource
  Definitions).

---

## 2. System Architecture

The system is composed of two primary data-plane components and a control plane.

### 2.1 Component Overview

```
┌──────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                        │
│                                                                  │
│  ┌─────────────────────────────────┐                             │
│  │           App Pod               │                             │
│  │  ┌───────────┐  ┌───────────┐  │                             │
│  │  │    App    │──│ Fiso-Link │──┼──── Sync ──► External APIs  │
│  │  │ Container │  │ (Sidecar) │──┼──── Async ─► Broker         │
│  │  └───────────┘  └───────────┘  │                             │
│  │                  ┌───────────┐  │                             │
│  │                  │Interceptor│  │  (optional sidecar)        │
│  │                  └───────────┘  │                             │
│  └─────────────────────────────────┘                             │
│                                                                  │
│  ┌─────────────────────────────────┐                             │
│  │        Fiso-Flow Deployment     │                             │
│  │  ┌───────────┐  ┌───────────┐  │                             │
│  │  │ Connector │──│Normalizer │  │◄── Kafka / NATS / SQS / S3 │
│  │  └───────────┘  └───────────┘  │                             │
│  │  ┌───────────┐  ┌───────────┐  │                             │
│  │  │  Mapper   │──│  Router   │──┼──► HTTP / gRPC / Temporal   │
│  │  └───────────┘  └───────────┘  │                             │
│  └─────────────────────────────────┘                             │
│                                                                  │
│  ┌─────────────────────────────────┐                             │
│  │       Control Plane             │                             │
│  │  CRDs / ConfigMaps / Watcher   │                             │
│  └─────────────────────────────────┘                             │
└──────────────────────────────────────────────────────────────────┘
```

### 2.2 Core Components

#### A. Fiso-Link (The Outbound Gateway)

- **Role:** Acts as the egress proxy for the application.
- **Deployment:** Sidecar (1:1 with app) or Node-level Agent.
- **Protocols:**
  - **Sync Mode:** Forward Proxy for HTTP/gRPC. Handles Auth injection, Service
    Discovery, and Circuit Breaking.
  - **Async Mode:** Accepts local requests, wraps them in CloudEvents, injects
    `trace-id` / `correlation-id`, and publishes to the configured Message Broker
    (Kafka/NATS/SQS).
- **Interception:** Supports WASM or Sidecar calls for complex outbound logic
  (e.g., Request Signing).

#### B. Fiso-Flow (The Inbound Source)

- **Role:** Acts as the event ingestion engine.
- **Deployment:** Standalone Deployment (scalable based on queue depth).
- **Functionality:**
  - **Connector:** Connects to physical infrastructure (Kafka Topic, HTTP Webhook,
    gRPC Streaming).
  - **Normalizer:** Converts proprietary vendor events into standard CloudEvents.
  - **Mapper:** Applies unified fields-based transformations (compiled to optimized CEL) to clean "messy" data.
  - **Router:** Delivers the clean event to the configured Sink.

#### C. Fiso Sinks (The Destinations)

Fiso-Flow does not just POST to an API. It supports multiple "Sink" types:

- **HTTP/gRPC Sink:** Standard push to an internal microservice endpoint.
- **Temporal Sink:** Directly signals or starts a Durable Workflow (removes the
  need for "glue" services). *(Phase 2 — see Section 12)*
- **Kafka Sink:** Republishes transformed events to another Kafka topic (Bridge
  pattern). Useful for event repartitioning, enrichment pipelines, or multi-cluster
  replication scenarios.
- **Event Sink:** Republishes to another broker (Bridge pattern).

---

## 3. Data Flow Specifications

### 3.1 Asynchronous "Loop" (The Standard Pattern)

This pattern decouples the request from the response, allowing for long-running
processes.

```
App ──POST /async/create-order──► Fiso-Link
                                      │
                                      ▼
                                 Wrap CloudEvent
                                 type: order.create
                                 + correlation-id
                                      │
                                      ▼
                                   Broker ──► External System
                                                    │
                                                    ▼
                                              Processes order
                                              Emits: order.success
                                              + correlation-id
                                                    │
                                                    ▼
                                   Broker ◄─────────┘
                                      │
                                      ▼
                                 Fiso-Flow
                                 (consume, transform, route)
                                      │
                                      ▼
App ◄──POST /callbacks/order-result───┘
```

1. **Trigger:** App sends `POST /async/create-order` to Fiso-Link.
2. **Publish:** Fiso-Link wraps payload in CloudEvent (`type: order.create`),
   adds `correlation-id`, publishes to Broker.
3. **Process:** External system processes order, emits `type: order.success`
   with same `correlation-id`.
4. **Ingest:** Fiso-Flow consumes `order.success`.
5. **Transform:** Fiso-Flow runs unified transform (compiled to optimized CEL) to normalize payload.
6. **Deliver:** Fiso-Flow delivers result to App `POST /callbacks/order-result`.

#### Correlation Failure Handling

The async loop depends on the external system propagating the `correlation-id`
back. This is not always guaranteed. Fiso handles this with:

- **Correlation TTL:** Each correlation entry has a configurable TTL (default:
  `24h`). After expiry, the correlation is moved to a `correlation.expired` DLQ
  topic and an alert is emitted.
- **Uncorrelated Events:** Inbound events that carry no recognizable
  `correlation-id` are delivered to the sink with a
  `fiso-correlation-status: unmatched` header. The application decides how to
  handle them.
- **Correlation Store:** Lightweight key-value store (embedded bbolt for sidecar
  mode, Redis for shared/node-agent mode) tracks pending correlations.
- **Timeout Callback:** When a correlation expires, Fiso-Flow can optionally
  deliver a synthetic `type: correlation.timeout` event to the app's callback
  endpoint, allowing the app to trigger compensating logic.

### 3.2 Synchronous "Passthrough" (The Legacy Pattern)

Used when an immediate answer is required (e.g., UI read operations).

```
App ──GET /link/crm/customer/123──► Fiso-Link
                                       │
                                       ▼
                                  Resolve "crm" target
                                  Inject OAuth token
                                  Start OTel span
                                       │
                                       ▼
                                  https://api.salesforce.com/...
                                       │
                                       ▼
App ◄──────── Stream response ─────────┘
```

1. **Request:** App calls `http://localhost:3500/link/crm/customer/123`.
2. **Enrich:** Fiso-Link looks up `crm` target, injects OAuth Token, starts
   OTel span.
3. **Forward:** Fiso-Link calls `https://api.salesforce.com/...`.
4. **Response:** Fiso-Link streams response back to App.

---

## 4. Configuration & Control Plane

Fiso is designed to work in locked-down environments. It supports a **Tiered
Configuration Strategy**.

### 4.1 Tier 1: Kubernetes Native (CRDs)

- **Preferred method.**
- **Resources:** `FlowDefinition`, `LinkTarget`.
- **Validation:** Full schema validation via OpenAPI v3 schemas in CRDs.

```yaml
apiVersion: fiso.io/v1alpha1
kind: LinkTarget
metadata:
  name: crm
  namespace: default
spec:
  protocol: https
  host: api.salesforce.com
  auth:
    type: oauth2
    secretRef:
      name: crm-credentials
      namespace: default
  circuitBreaker:
    enabled: true
    failureThreshold: 5
    resetTimeout: 30s
  retry:
    maxAttempts: 3
    backoff: exponential
    initialInterval: 100ms
    maxInterval: 10s
```

```yaml
apiVersion: fiso.io/v1alpha1
kind: FlowDefinition
metadata:
  name: order-events
  namespace: default
spec:
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
      id: "data.legacy_id"
      timestamp: "time"
      status: "data.order_status"
  sink:
    type: http
    config:
      url: http://order-service.default.svc:8080/callbacks/order-result
      method: POST
      headers:
        Content-Type: application/cloudevents+json
  errorHandling:
    deadLetterTopic: fiso-dlq-order-events
    maxRetries: 5
    backoff: exponential
```

#### Kafka Sink Example

```yaml
apiVersion: fiso.io/v1alpha1
kind: FlowDefinition
metadata:
  name: order-events-enriched
  namespace: default
spec:
  source:
    type: kafka
    config:
      brokers:
        - kafka.infra.svc:9092
      topic: orders
      consumerGroup: fiso-enricher
      startOffset: latest
  transform:
    fields:
      order_id: "data.legacy_id"
      timestamp: "time"
      status: "data.order_status"
      enriched: '"true"'
  sink:
    type: kafka
    config:
      brokers:
        - kafka.infra.svc:9092
      topic: orders-enriched
  errorHandling:
    deadLetterTopic: fiso-dlq-enricher
    maxRetries: 3
    backoff: exponential
```

### 4.2 Tier 2: Universal (ConfigMap/File)

- **Fallback method** when CRDs are blocked.
- **Mechanism:** Fiso components watch a mounted volume or K8s ConfigMap.
- **Format:** Standard YAML files following the same schema as CRDs (minus
  `apiVersion`/`kind` wrappers).
- **Discovery:** Fiso scans for file changes and hot-reloads (no restart
  required).
- **File Convention:** `<config-dir>/link-targets/*.yaml`,
  `<config-dir>/flow-definitions/*.yaml`.

### 4.3 Tier 3: Annotation Injection

- **Simple use-cases.**
- Define basic Sinks directly on the Pod Spec metadata.

```yaml
metadata:
  annotations:
    fiso.io/link-target.crm: "https://api.salesforce.com"
    fiso.io/link-auth.crm: "secret:crm-credentials"
    fiso.io/flow-sink: "http://localhost:8080/callbacks"
```

### 4.4 Configuration Precedence

When multiple tiers define the same resource, precedence is:

1. CRD (highest)
2. ConfigMap/File
3. Annotation (lowest)

A warning log is emitted when conflicting definitions are detected.

---

## 5. Security Model

### 5.1 Credential Management

Fiso never stores credentials in its own configuration. All secrets are
referenced indirectly.

| Method | Mechanism | Use Case |
|--------|-----------|----------|
| **K8s Secrets** | `secretRef` in CRD/ConfigMap | Default for all environments |
| **Vault (HashiCorp)** | `vaultRef` with role/path | Enterprises with existing Vault |
| **Mounted Files** | File path reference | Air-gapped / non-K8s environments |

```yaml
# Example: K8s Secret reference
auth:
  type: oauth2
  secretRef:
    name: crm-credentials    # K8s Secret name
    namespace: default
    keys:
      clientId: client_id     # Key within the Secret
      clientSecret: client_secret

# Example: Vault reference (Phase 2)
auth:
  type: oauth2
  vaultRef:
    path: secret/data/crm
    role: fiso-link
```

**Credential Refresh:** Fiso-Link watches the underlying secret source for
changes. OAuth tokens are refreshed proactively before expiry (at 80% of TTL).
Refreshed tokens are cached in-memory only — never written to disk.

### 5.2 Transport Security

- **Fiso-Link ↔ External APIs:** TLS required by default. `tlsSkipVerify` is
  available but emits a persistent warning metric.
- **Fiso-Flow ↔ Brokers:** TLS + SASL/SCRAM or mTLS, configured per-source.
- **Fiso-Link ↔ Interceptors:** Localhost communication within the same pod
  (no TLS required). For cross-pod interceptors, mTLS is mandatory.
- **App ↔ Fiso-Link:** Localhost-only binding by default (`127.0.0.1:3500`).
  Fiso-Link does not accept traffic from outside the pod unless explicitly
  configured.

### 5.3 Authorization

- **RBAC for CRDs:** Standard Kubernetes RBAC controls who can create/modify
  `FlowDefinition`, `LinkTarget`, and `TransformationRule` resources.
- **LinkTarget Scoping:** A `LinkTarget` can be scoped to specific namespaces
  via `spec.allowedNamespaces`. If unset, it is available only in its own
  namespace.
- **Request Allowlisting:** Fiso-Link supports an optional `allowedPaths` list
  per `LinkTarget`, restricting which URL paths the application can access
  through the proxy.

```yaml
spec:
  allowedNamespaces:
    - orders
    - payments
  allowedPaths:
    - /api/v2/customers/**
    - /api/v2/orders/**
```

### 5.4 Network Policy

Fiso provides optional `NetworkPolicy` templates that restrict:

- Fiso-Link egress to only declared `LinkTarget` hosts.
- Fiso-Flow ingress to only declared broker endpoints.
- Fiso-Link ingress to only `127.0.0.1` (pod-local traffic).

These are generated from CRD definitions and applied automatically when
`spec.networkPolicy.enabled: true`.

---

## 6. Error Handling & Delivery Guarantees

### 6.1 Delivery Semantics

| Component | Guarantee | Mechanism |
|-----------|-----------|-----------|
| **Fiso-Link (Async)** | At-least-once | Broker ack before returning 202 to app |
| **Fiso-Link (Sync)** | Best-effort | HTTP semantics; retries are transparent |
| **Fiso-Flow** | At-least-once | Consumer offset committed after sink ack |

**Exactly-once is not a goal.** Fiso provides at-least-once delivery and
expects sinks to be idempotent. Fiso assists with idempotency by:

- Including a unique `fiso-event-id` (UUID v7) in every CloudEvent.
- Propagating the original `correlation-id` through all hops.
- Sinks can deduplicate on `fiso-event-id` using their own store.

### 6.2 Retry Policy

All retries are configurable per-resource. Defaults:

```yaml
retry:
  maxAttempts: 3
  backoff: exponential       # constant | linear | exponential
  initialInterval: 200ms
  maxInterval: 30s
  jitter: 0.2                # ±20% randomization
  retryableStatusCodes:      # HTTP codes that trigger retry
    - 429
    - 502
    - 503
    - 504
```

**Error Classification:**

| Category | Examples | Action |
|----------|----------|--------|
| **Transient** | 429, 502, 503, 504, TCP timeout, DNS failure | Retry with backoff |
| **Permanent** | 400, 401, 403, 404, 422 | No retry, send to DLQ immediately |
| **Unknown** | 500, unclassified exceptions | Retry up to `maxAttempts`, then DLQ |

### 6.3 Dead Letter Queue (DLQ)

Every Fiso-Flow pipeline has an associated DLQ. Events land in the DLQ when:

- All retry attempts are exhausted.
- A permanent error is received.
- A transformation fails (malformed data).

**DLQ Structure:**

```
Topic: fiso-dlq-<flow-name>
```

Each DLQ message includes:

| Header | Description |
|--------|-------------|
| `fiso-original-topic` | Source topic the event came from |
| `fiso-error-code` | HTTP status or internal error code |
| `fiso-error-message` | Human-readable error description |
| `fiso-retry-count` | Number of retries attempted |
| `fiso-failed-at` | ISO 8601 timestamp of final failure |
| `fiso-flow-name` | FlowDefinition that was processing it |

**DLQ Reprocessing:** A CLI tool (`fiso dlq replay --flow order-events`) and an
optional controller can replay DLQ messages back to the original pipeline after
the root cause is fixed.

### 6.4 Circuit Breaking (Fiso-Link)

Fiso-Link implements a three-state circuit breaker per `LinkTarget`:

```
CLOSED ──(failures >= threshold)──► OPEN
   ▲                                  │
   │                            (resetTimeout)
   │                                  ▼
   └──────(probe succeeds)─────── HALF-OPEN
```

| Parameter | Default | Description |
|-----------|---------|-------------|
| `failureThreshold` | `5` | Consecutive failures to trip |
| `resetTimeout` | `30s` | Time before trying a probe request |
| `halfOpenRequests` | `1` | Probe requests allowed in HALF-OPEN |
| `successThreshold` | `3` | Successes in HALF-OPEN to close |

**State Storage:** Circuit breaker state is stored **in-memory per Fiso-Link
instance**. In sidecar mode this is inherently per-app. In node-agent mode,
state is keyed by `LinkTarget` name + requesting namespace.

When the circuit is OPEN, Fiso-Link returns `503 Service Unavailable` with a
`Retry-After` header to the application immediately, without making an
outbound call.

### 6.6 Performance Characteristics

Fiso-Flow is optimized for high-throughput event processing:

| Metric | Value | Notes |
|--------|-------|-------|
| **Transform throughput** | ~60% faster than previous implementation | Achieved through compiled CEL optimization and removal of per-event goroutines |
| **Memory efficiency** | Reduced goroutine overhead | Direct CEL evaluation eliminates goroutine-per-event pattern |
| **Latency** | Sub-millisecond per event | For simple field mappings |
| **Throughput** | 100K+ events/second per instance | Depending on transform complexity |

**Key Optimizations:**
1. **Compiled CEL:** Transform expressions are compiled once at startup, not per-event
2. **Direct Evaluation:** No per-event goroutines for transform execution
3. **Context-based Timeouts:** Uses context cancellation instead of goroutine+timeout pattern
4. **Type Conversion Optimization:** Efficient CEL-to-Go type conversion pipeline

**Benchmark Results** (from `internal/transform/unified/unified_benchmark_test.go`):
- Simple field mapping: ~500 ns/op
- With arithmetic: ~800 ns/op
- With nested structures: ~1.2 µs/op
- Large payload (10+ fields): ~2 µs/op

### 6.5 Backpressure

- **Fiso-Flow:** If a sink is slow or unavailable, Fiso-Flow pauses consumer
  polling (Kafka `pause()` / NATS backoff). This prevents unbounded memory
  growth and respects broker rebalance timeouts.
- **Fiso-Link (Async):** If the broker is unreachable, Fiso-Link returns `503`
  to the application. It does **not** buffer messages locally — the application
  is responsible for retry at the request level.

---

## 7. State Management

Fiso is designed to be **stateless where possible** and **state-minimal where
necessary**.

### 7.1 State Inventory

| State | Owner | Storage | Lifecycle |
|-------|-------|---------|-----------|
| Circuit breaker state | Fiso-Link | In-memory | Ephemeral (reset on restart) |
| OAuth token cache | Fiso-Link | In-memory | Ephemeral (re-fetched on restart) |
| Consumer offsets | Fiso-Flow | Broker-managed (Kafka `__consumer_offsets`) | Persistent |
| Correlation entries | Fiso-Link | Embedded bbolt (sidecar) or Redis (node-agent) | TTL-based |
| Configuration cache | Both | In-memory (from CRD/ConfigMap watch) | Ephemeral |

### 7.2 Persistence Requirements

- **Phase 1:** No external state stores required. Consumer offsets are
  broker-managed. Circuit breaker and token caches are in-memory.
- **Phase 2:** Correlation tracking requires either embedded bbolt (sidecar
  mode, stored on an `emptyDir` volume) or a shared Redis instance (node-agent
  mode).

### 7.3 Restart Behavior

| Component | On Restart |
|-----------|------------|
| Fiso-Link | Circuit breakers reset to CLOSED. OAuth tokens re-fetched. In-flight sync requests fail with 502. Async publishes not affected (already acked by broker). |
| Fiso-Flow | Resumes from last committed consumer offset. No message loss. May cause brief redelivery of uncommitted messages (at-least-once). |

---

## 8. Schema Evolution & Versioning

### 8.1 CloudEvent Type Versioning

All CloudEvent `type` fields follow a dotted naming convention with an optional
version suffix:

```
<domain>.<entity>.<action>[.v<N>]
```

Examples:
- `order.create.v1`
- `order.create.v2`
- `payment.refund.v1`

When no version suffix is present, `v1` is implied.

### 8.2 Compatibility Contract

- **Backward compatible changes** (adding optional fields) do not require a
  version bump.
- **Breaking changes** (removing fields, changing types, renaming fields)
  require a new version suffix.
- **Fiso-Flow routing** can match on type prefix (`order.create.*`) to process
  multiple versions with the same pipeline, or on exact type to use
  version-specific transforms.

### 8.3 Transform Validation

Unified transforms do not inherently validate output schema. To catch issues early:

- **`spec.sink.validate`** (optional): A JSON Schema that the transform output
  is validated against before delivery. Validation failures route the event to
  the DLQ with error code `TRANSFORM_VALIDATION_FAILED`.
- **Dry-run mode:** `fiso transform test --flow order-events --input sample.json`
  runs the transform locally and prints the output without publishing.

```yaml
spec:
  transform:
    fields:
      id: "data.legacy_id"
      timestamp: "time"
  sink:
    type: http
    validate:
      jsonSchema:
        type: object
        required: [id, timestamp]
        properties:
          id:
            type: string
          timestamp:
            type: string
            format: date-time
```

### 8.4 Schema Registry (Phase 2)

In Phase 2, Fiso-Flow can optionally integrate with a Confluent-compatible
Schema Registry for:

- Automatic Avro/Protobuf deserialization of source events.
- Schema validation of outbound CloudEvents before publishing.
- Schema evolution enforcement (backward/forward/full compatibility).

---

## 9. Integration Features ("The Imperfect World")

### 9.1 Declarative Transformation

- **Engine:** Unified fields-based transform system that compiles to optimized CEL (Common Expression Language).
- **Performance:** 60% faster than the previous CEL implementation through compiled optimization and direct evaluation (no per-event goroutines).
- **Usage:** Defined inline in YAML config or CRD using a `fields` map.
- **Architecture:** The unified transform system replaces the old dual-system approach (separate CEL and mapping transformers). All transformations now use the `fields:` syntax exclusively.

#### Transform Syntax

Transforms are defined as a map of output field names to CEL expressions:

```yaml
transform:
  fields:
    id: "data.legacy_id"
    timestamp: "time"
    status: "data.order_status"
    total: "data.price * data.quantity"
    customer_tier: 'data.type == "premium" ? "gold" : "standard"'
    full_name: 'data.first + " " + data.last'
    order: '{"id": data.order_id, "total": data.subtotal + data.tax}'
```

**Important:** Static string literals in CEL must be quoted. Use double quotes for CEL strings wrapped in single quotes: `'"value"'`

#### Transform Features

The unified transform system supports all CEL operations:

| Feature | Example |
|---------|---------|
| Field mapping | `"field": "data.nested.path"` |
| Arithmetic | `"total": "data.price * data.quantity"` |
| Conditionals | `'data.type == "premium" ? "gold" : "standard"'` |
| String operations | `'data.first + " " + data.last'` |
| Nested objects | `'{"id": data.id, "name": data.name}'` |
| Static literals | Strings: `'"value"'`, Numbers: `42`, Booleans: `true` |
| List construction | `"[data.id, data.name, data.type]"` |
| Boolean logic | `"data.age >= 18 && data.verified == true"` |

**Available variables:** `data`, `time`, `source`, `type`, `id`, `subject`

#### Transform Safety

Unified transforms execute in a sandboxed CEL context with the following constraints:

| Constraint | Default | Configurable |
|------------|---------|--------------|
| Execution timeout | `5s` | Yes, via `spec.transform.timeout` |
| Max output size | `1MB` | Yes, via `spec.transform.maxOutputBytes` |
| Max recursion depth | `256` | No |

Transform failures (timeout, invalid expression, output too large) route the
original event to the DLQ with error code `TRANSFORM_FAILED`.

**Safety model:** Uses context cancellation for timeout enforcement instead of goroutine+timeout, eliminating goroutine leaks and reducing memory overhead.

#### Transform Testing

Transforms can be tested before deployment using the CLI:

```bash
# Test a transform against a sample event using a flow file
fiso transform test \
  --flow fiso/flows/order-flow.yaml \
  --input sample-event.json

# Test with inline JSON
fiso transform test \
  --flow fiso/flows/order-flow.yaml \
  --input '{"order_id":"TEST-001","customer_id":"CUST-123"}'

# Test with JSONL file (uses first line only)
fiso transform test \
  --flow fiso/flows/order-flow.yaml \
  --input sample-orders.jsonl
```

### 9.2 Programmable Interceptors

- **Purpose:** Complex logic that cannot be expressed in YAML (e.g., checksums,
  DB lookups, request signing).
- **Protocol:** gRPC (Fiso calls out to the interceptor).
- **Implementation:**
  - **Sidecar:** A container in the same pod running user logic.
  - **WASM:** *(Phase 3 Roadmap)* Compiled modules running in-process.

#### Interceptor Contract

```protobuf
service FisoInterceptor {
  // Called before Fiso-Link sends an outbound request
  rpc OnOutboundRequest(OutboundRequest) returns (OutboundRequest);

  // Called before Fiso-Flow delivers to a sink
  rpc OnInboundEvent(CloudEvent) returns (CloudEvent);
}
```

Interceptors can:
- Modify headers, body, or metadata.
- Reject a request/event by returning an error status.
- Add latency (subject to a configurable timeout, default `2s`).

#### Interceptor Configuration

```yaml
spec:
  interceptors:
    - name: request-signer
      type: grpc
      address: localhost:50051       # Sidecar in same pod
      timeout: 2s
      failOpen: false                # If interceptor is down, block traffic
      phase: pre-request             # pre-request | post-response
```

---

## 10. Deployment Topology

### 10.1 Fiso-Link Deployment Modes

#### Sidecar Mode (Recommended)

- **Deployment:** Injected as a container in the application pod, either
  manually or via a mutating admission webhook.
- **Pros:** Process isolation per app, independent scaling, fault isolation.
- **Cons:** Higher aggregate resource usage (one Fiso-Link per pod).
- **Resource Defaults:** `cpu: 50m / 200m`, `memory: 64Mi / 128Mi`.
- **Port Binding:** `127.0.0.1:3500` (HTTP) and `127.0.0.1:3501` (gRPC).

#### Node-Agent Mode

- **Deployment:** DaemonSet, one per node.
- **Pros:** Lower resource overhead for high-density nodes.
- **Cons:** Shared failure domain across all pods on the node. State (circuit
  breakers) is shared and must be keyed per-namespace/app.
- **Port Binding:** `0.0.0.0:3500` with request routing based on source pod IP
  (obtained from `X-Forwarded-For` or PROXY protocol).
- **Isolation:** Each request is tagged with the source pod's namespace and
  service account. `LinkTarget` access is enforced per-namespace.

### 10.2 Fiso-Flow Deployment

- **Deployment:** Standard Kubernetes `Deployment` (not DaemonSet).
- **Scaling:** Horizontal Pod Autoscaler based on consumer lag metric
  (`fiso_flow_consumer_lag`). One Fiso-Flow deployment per `FlowDefinition`.
- **Consumer Groups:** Each FlowDefinition maps to a unique Kafka consumer
  group (`fiso-flow-<flow-name>`). Scaling beyond the partition count has no
  effect.
- **Multiple Sources:** A single Fiso-Flow deployment handles exactly one
  source. To consume from multiple topics, create multiple `FlowDefinition`
  resources.

### 10.3 Service Discovery

Fiso-Link resolves `LinkTarget` hosts using the following chain:

1. **Static:** If `spec.host` is a full URL, use it directly.
2. **Kubernetes DNS:** If `spec.host` matches `<service>.<namespace>.svc`,
   resolve via cluster DNS.
3. **External DNS:** All other hosts resolve via the node's DNS configuration.

Fiso-Link caches DNS results with a configurable TTL (default: `30s`).

### 10.4 Sidecar Injection

For environments using the sidecar mode, Fiso provides a **mutating admission
webhook** that automatically injects the Fiso-Link container into pods with the
annotation:

```yaml
metadata:
  annotations:
    fiso.io/inject: "true"
```

The webhook injects:
- The Fiso-Link sidecar container with default resource limits.
- A shared `emptyDir` volume for correlation storage (if enabled).
- Environment variables for the app to discover Fiso-Link
  (`FISO_LINK_HTTP=http://127.0.0.1:3500`).

---

## 11. Observability & Standards

### 11.1 Tracing

- **Standard:** OpenTelemetry (OTel) native.
- **Propagation:** `traceparent` (W3C Trace Context) headers are propagated
  automatically through Fiso-Link and Fiso-Flow.
- **Span Creation:**
  - Fiso-Link creates a span for each outbound request (sync and async).
  - Fiso-Flow creates a span for each event processed (consume → transform →
    deliver).
- **Exporter:** Configurable OTLP exporter (default: `localhost:4317`).

### 11.2 Metrics

Prometheus endpoints exposed on `:9090/metrics`.

#### Fiso-Link Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `fiso_link_requests_total` | Counter | `target`, `method`, `status`, `mode` | Total outbound requests |
| `fiso_link_request_duration_seconds` | Histogram | `target`, `method` | Outbound request latency |
| `fiso_link_circuit_state` | Gauge | `target` | Circuit breaker state (0=closed, 1=half-open, 2=open) |
| `fiso_link_retries_total` | Counter | `target`, `attempt` | Retry attempts |
| `fiso_link_auth_refresh_total` | Counter | `target`, `status` | Credential refresh attempts |

#### Fiso-Flow Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `fiso_flow_events_total` | Counter | `flow`, `status` | Events processed |
| `fiso_flow_event_duration_seconds` | Histogram | `flow`, `phase` | Processing time per phase |
| `fiso_flow_consumer_lag` | Gauge | `flow`, `partition` | Consumer lag per partition |
| `fiso_flow_transform_errors_total` | Counter | `flow`, `error_type` | Transform failures |
| `fiso_flow_dlq_total` | Counter | `flow` | Events sent to DLQ |

### 11.3 Logs

- **Format:** Structured JSON logging.
- **Levels:** `debug`, `info`, `warn`, `error`.
- **Standard Fields:** Every log line includes `trace_id`, `correlation_id`,
  `component` (link/flow), `flow_name` or `target_name`.
- **Sensitive Data:** Request/response bodies are **never** logged by default.
  `spec.debug.logBody: true` enables body logging at `debug` level (with a
  configurable max size of 4KB).

### 11.4 Health Endpoints

| Endpoint | Purpose |
|----------|---------|
| `:9090/healthz` | Liveness probe — process is running |
| `:9090/readyz` | Readiness probe — connected to broker / config loaded |

---

## 12. Phase 1 Implementation Plan

### 12.1 Scope

Phase 1 delivers a **minimal but functional Fiso-Flow** that demonstrates the
core value proposition: consume from Kafka, transform via unified fields-based system (compiled to optimized CEL), deliver to HTTP
sink, with DLQ support.

**In Scope:**
- Fiso-Flow with Kafka source, unified transform, HTTP sink.
- YAML file-based configuration (Tier 2).
- At-least-once delivery with DLQ.
- Structured JSON logging.
- Prometheus metrics.
- CLI for operations (produce, consume, logs, transform test).

**Out of Scope (Phase 2+):**
- Fiso-Link (outbound gateway).
- CRD-based configuration (Tier 1).
- Temporal sink.
- WASM interceptors.
- Vault integration.
- Schema Registry integration.
- gRPC sink/source.
- Sidecar injection webhook.

### 12.2 Repository Structure

```
github.com/org/fiso/
├── cmd/
│   └── fiso-flow/           # Main entrypoint for Fiso-Flow binary
│       └── main.go
├── internal/
│   ├── source/              # Source interface + implementations
│   │   ├── source.go        # Source interface definition
│   │   ├── kafka/           # Kafka source implementation
│   │   │   └── kafka.go
│   │   ├── http/            # HTTP source implementation
│   │   │   └── http.go
│   │   └── grpc/            # gRPC source implementation
│   │       └── grpc.go
│   ├── sink/                # Sink interface + implementations
│   │   ├── sink.go          # Sink interface definition
│   │   ├── http/            # HTTP sink implementation
│   │   │   └── http.go
│   │   ├── kafka/           # Kafka sink implementation
│   │   │   └── kafka.go
│   │   └── temporal/        # Temporal sink implementation
│   │       └── temporal.go
│   ├── transform/           # Transformer interface + implementations
│   │   ├── transform.go     # Transformer interface definition
│   │   └── unified/         # Unified fields-based transformer (CEL-compiled)
│   │       ├── unified.go
│   │       └── unified_benchmark_test.go
│   ├── pipeline/            # Pipeline orchestrator (source → transform → sink)
│   │   └── pipeline.go
│   ├── dlq/                 # Dead Letter Queue handler
│   │   └── dlq.go
│   ├── interceptor/         # Interceptor interface and implementations
│   │   ├── interceptor.go
│   │   └── wasm/            # WASM interceptor support
│   │       └── wasm.go
│   ├── config/              # Configuration loading and watching
│   │   └── config.go
│   ├── cli/                 # CLI commands
│   │   ├── init.go
│   │   ├── validate.go
│   │   ├── transform.go
│   │   ├── produce.go
│   │   ├── consume.go
│   │   └── logs.go
│   └── observability/       # Metrics, logging, health checks
│       ├── metrics.go
│       ├── logging.go
│       └── health.go
├── pkg/
│   └── cloudevents/         # CloudEvents helpers (wrapping, validation)
│       └── cloudevents.go
├── configs/
│   ├── compose/             # Docker Compose configurations
│   │   └── order-events.yaml
│   └── examples/            # Example FlowDefinition YAML files
│       ├── order-events.yaml
│       └── unified-transform.yaml
├── docs/
│   └── hld-specification.md # This document
├── test/
│   ├── e2e/                # End-to-end tests
│   └── integration/        # Integration tests
├── api/
│   └── v1alpha1/           # Kubernetes API types
│       └── types.go
├── deploy/
│   ├── crds/               # Kubernetes Custom Resource Definitions
│   └── examples/           # Deployment examples
├── go.mod
├── go.sum
├── Dockerfile
└── Makefile
```

### 12.3 CLI Commands

Fiso provides several CLI commands for development and operational tasks:

#### Transform Testing

```bash
fiso transform test --flow <path> --input <json|file>
```

Test a transform configuration without starting the full stack. Supports inline JSON, JSON files, and JSONL files.

#### Event Production

```bash
fiso produce --topic <name> [--file <path>] [--json <data>] [--count <n>] [--rate <duration>]
```

Produces test events to a Kafka topic. Useful for testing flows without Docker commands.

**Examples:**
```bash
# Produce single event from file
fiso produce --topic orders --file sample-order.json

# Produce inline JSON
fiso produce --topic orders --json '{"order_id":"TEST-001"}'

# Produce multiple events from JSONL file with rate limiting
fiso produce --topic orders --count 10 --rate 100ms --file orders.jsonl
```

#### Event Consumption

```bash
fiso consume --topic <name> [--from-beginning] [--max-messages <n>] [--follow]
```

Consumes and displays events from a Kafka topic for debugging. Auto-discovers Kafka configuration from flow files.

**Examples:**
```bash
# Consume last 10 messages
fiso consume --topic orders --max-messages 10

# Consume from beginning
fiso consume --topic orders --from-beginning --max-messages 100

# Follow mode (like tail -f)
fiso consume --topic orders --follow
```

#### Log Viewing

```bash
fiso logs [--service <name>] [--tail <number>] [--follow]
```

Shows logs from fiso services without needing docker-compose commands. Auto-detects Docker Compose or Kubernetes environments.

**Examples:**
```bash
# Show last 100 lines of fiso-flow logs
fiso logs

# Follow fiso-flow logs
fiso logs --follow

# Show last 500 lines
fiso logs --tail 500

# Show logs for specific service
fiso logs --service fiso-link
```

### 12.4 Core Interfaces

```go
package source

import "context"

// Event represents a raw event consumed from a source.
type Event struct {
    Key     []byte
    Value   []byte
    Headers map[string]string
    Offset  int64
    Topic   string
}

// Source consumes events from an external system.
type Source interface {
    // Start begins consuming events. Blocks until ctx is cancelled.
    // Events are delivered to the handler function.
    Start(ctx context.Context, handler func(context.Context, Event) error) error

    // Close performs graceful shutdown.
    Close() error
}
```

```go
package sink

import "context"

// Sink delivers processed events to a destination.
type Sink interface {
    // Deliver sends a processed event to the destination.
    // Returns nil on success. The pipeline commits the source offset
    // only after Deliver returns nil.
    Deliver(ctx context.Context, event []byte, headers map[string]string) error

    // Close performs graceful shutdown.
    Close() error
}
```

```go
package transform

import "context"

// Transformer applies a transformation to an event payload.
type Transformer interface {
    // Transform takes a raw event payload and returns the transformed payload.
    // Returns an error if the transformation fails (routes to DLQ).
    Transform(ctx context.Context, input []byte) ([]byte, error)
}
```

### 12.5 Key Library Choices

| Dependency | Library | Rationale |
|------------|---------|-----------|
| Kafka client | `github.com/twmb/franz-go` | Pure Go, high performance, no CGo, good consumer group support |
| CEL engine | `github.com/google/cel-go` | Google-maintained, type-checked, built-in sandboxing, used by K8s/Envoy |
| CloudEvents | `github.com/cloudevents/sdk-go/v2` | Official SDK |
| HTTP framework | `net/http` (stdlib) | Minimal dependencies for sink + health endpoints |
| Metrics | `github.com/prometheus/client_golang` | Standard Prometheus client |
| Logging | `log/slog` (stdlib) | Structured logging, Go 1.21+ |
| Config | `gopkg.in/yaml.v3` + `fsnotify` | YAML parsing + file watching |

### 12.6 Definition of Done (Phase 1)

- [ ] Fiso-Flow binary consumes from a Kafka topic.
- [ ] Incoming messages are wrapped in CloudEvents format.
- [ ] Unified transform (fields-based, compiled to optimized CEL) is applied to event payload.
- [ ] Transformed event is POSTed to a configurable HTTP endpoint.
- [ ] Failed events (after retries) are published to a DLQ topic.
- [ ] Configuration is loaded from YAML files with hot-reload.
- [ ] Prometheus metrics are exposed on `:9090/metrics`.
- [ ] Structured JSON logs with `trace_id` and `flow_name`.
- [ ] Health endpoints (`/healthz`, `/readyz`) are functional.
- [ ] `fiso transform test` CLI subcommand works.
- [ ] Unit tests for source, sink, transform, and pipeline packages.
- [ ] Integration test using Testcontainers (Kafka + HTTP echo server).
- [ ] Dockerfile and Makefile for build/test/run.
- [ ] Example configuration in `configs/examples/`.

---

## Appendix A: Glossary

| Term | Definition |
|------|------------|
| **CloudEvent** | A specification for describing event data in a common way (CNCF CloudEvents v1.0) |
| **Correlation ID** | A unique identifier that links an outbound request to its eventual inbound response |
| **DLQ** | Dead Letter Queue — a holding area for events that cannot be processed |
| **FlowDefinition** | A Fiso CRD/config that defines a complete inbound pipeline (source → transform → sink) |
| **LinkTarget** | A Fiso CRD/config that defines an external API endpoint and its auth/retry/circuit-breaker settings |
| **Sink** | The destination for a processed event (HTTP endpoint, Kafka topic, Temporal workflow, another broker) |
| **Source** | The origin of an event (Kafka topic, S3 bucket, webhook) |

## Appendix B: Configuration Reference

### Supported Source Types

| Type | Description | Configuration |
|------|-------------|---------------|
| **kafka** | Apache Kafka topic consumption | `brokers`, `topic`, `consumerGroup`, `startOffset` |
| **http** | HTTP webhook ingestion | `listenAddr`, `path` |
| **grpc** | gRPC streaming ingestion | `listenAddr` |

### Supported Sink Types

| Type | Description | Configuration |
|------|-------------|---------------|
| **http** | HTTP POST delivery | `url`, `method`, `headers` |
| **grpc** | gRPC streaming delivery | `target`, `service`, `method` |
| **temporal** | Temporal workflow execution | `hostPort`, `namespace`, `taskQueue`, `workflowType`, `mode`, `signalName` |
| **kafka** | Kafka topic publishing | `brokers`, `topic` |

### Transform Configuration

All transforms use the unified `fields:` syntax. Each field value is a CEL expression:

```yaml
transform:
  fields:
    output_field: "cel.expression.here"
```

**Available Variables in CEL:**
- `data` - The event payload (parsed JSON)
- `time` - Event timestamp
- `source` - Event source identifier
- `type` - Event type
- `id` - Event ID
- `subject` - Event subject

### Error Handling Configuration

```yaml
errorHandling:
  deadLetterTopic: "fiso-dlq-<flow-name>"  # Kafka topic for failed events
  maxRetries: 3                            # Retry attempts before DLQ
  backoff: "exponential"                    # Backoff strategy: constant|linear|exponential
```

See individual CRD schemas in `deploy/crds/` (Phase 2) or example YAML files in
`configs/examples/` for the full configuration reference.
