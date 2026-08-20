# Product Vision

> **Directional document:** This document records Fiso's durable product intent. It is not a current API or runtime specification, does not assert that every described outcome is implemented, and does not approve implementation work. See the [project README](../README.md) and implementation evidence for current behavior.

## The Promise

**Fiso gives every application a stable, contract-defined world, independent of the systems that happen to exist around it.**

An application should be written against the interfaces its business logic needs, as if its surrounding environment were already consistent, reliable, modern, and designed for that application. Broker details, vendor SDKs, legacy payloads, credential mechanisms, retry policies, and migration state belong outside the application boundary.

This is the application's **perfect world**:

- inbound events and calls arrive in the shape the application expects;
- outbound dependencies expose stable, meaningful local interfaces;
- development can begin before real integrations are available;
- environments can bind the same application contract to mocks, legacy systems, or future platforms; and
- replacing an external provider does not require application-code changes.

The application remains a black box in implementation, but its boundary is explicit, versioned, observable, and testable.

## Two Directions of Adaptation

```text
External reality                   Application's stable world

HTTP / Kafka / gRPC ──> Fiso-Flow ──> inbound ports and operations
Legacy payloads                   │
                                  │  business logic
                                  │  application state
                                  │  database / Temporal workflows
                                  │
External APIs / brokers <── Fiso-Link <── outbound ports and operations
Legacy or future providers
```

**Fiso-Flow** is the current foundation for adapting inbound reality. Sources receive external interactions, transforms and interceptors adapt them, CloudEvents provide a standard envelope where applicable, and sinks deliver the result toward the application.

**Fiso-Link** is the current foundation for virtualizing outbound dependencies. Applications call stable local target names while Link handles the concrete endpoint, authentication, discovery, retries, circuit breaking, rate limiting, protocol behavior, and adaptation.

The long-term goal is not to make application code understand more integration mechanisms. It is to make those mechanisms replaceable behind a stable application-facing boundary.

## Principles

### 1. The application boundary is stable

Application code depends on domain-meaningful inputs and dependencies, not on whichever infrastructure or vendor exists today.

### 2. Integrations are inverted

The application declares the world it needs. Fiso binds that world to the environment instead of allowing environmental details to shape business logic.

### 3. Development does not wait for integration

A local or test environment should supply contract-conforming behavior before production systems, topics, credentials, and provider sandboxes exist.

### 4. Adaptation is explicit and observable

Protocol conversion, request and response mapping, event normalization, authentication, resilience, and failure behavior must be declared, tested, and visible—not hidden in application code.

### 5. Provider migration preserves application behavior

Legacy and replacement providers may differ internally, but both must satisfy the same application-facing contract. Cutover and rollback should change a binding, not the application.

### 6. Compatibility is measured from the application outward

Infrastructure uptime alone is not success. A binding is compatible only when it delivers the operations, data, errors, and behavioral expectations promised to the application.

### 7. Contract evolution is deliberate

Changes to application-facing behavior need explicit versions, compatibility rules, evidence, and migration paths.

## Conceptual Model

These terms describe product direction. They are not yet first-class Fiso APIs or custom resources.

### Application Contract

The versioned definition of the stable world an application expects and provides: inbound capabilities, outbound dependencies, event or request shapes, responses, errors, and relevant behavioral expectations.

### Port

A named inbound or outbound capability at the application boundary, such as `order-events`, `customer-profile`, or `payment-authorizer`.

### Operation

An action or event within a port, such as receiving `OrderCreated`, retrieving a customer, or authorizing a payment.

### Binding

An environment-specific implementation of a port or operation. A development binding may use fixtures, initial production may call a legacy system, and a future binding may call a replacement platform.

### Adapter

Provider- or protocol-specific translation used by a binding. An adapter can normalize messages, map requests and responses, bridge protocols, or reconcile provider-specific errors with the application contract.

Current implementation terms such as `Source`, `Sink`, `FlowDefinition`, and `LinkTarget` remain the names of existing constructs. They are foundations for this direction, not automatic equivalents of the conceptual model above.

## Desired Outcomes

The vision is realized when teams can demonstrate outcomes such as these:

### Build before integrations exist

A team defines the application-facing contract, uses mock or fixture-backed bindings, and implements and tests its business logic without waiting for another team, broker, credential, or vendor environment.

### Normalize an imperfect inbound world

A legacy event, webhook, CDC record, or API request is adapted into the same stable inbound operation expected by the application.

### Replace a downstream provider without changing the application

The application continues calling the same local dependency operation while a binding moves from a legacy service to a new platform. Both implementations pass the same conformance checks, and rollback restores the old binding.

### Test the boundary independently

Contract tests validate a binding's inputs, outputs, errors, and adaptation without requiring the full production topology.

### Detect incompatibility before deployment

Validation and CI reject a provider or configuration change that would violate the application-facing contract.

These are desired product outcomes, not claims that Fiso supports every scenario today.

## Current Foundation

Fiso already contains important building blocks for this direction:

- Flow's [`Source`](../internal/source/source.go), [`Transformer`](../internal/transform/transform.go), and [`Sink`](../internal/sink/sink.go) interfaces separate transport, adaptation, and delivery.
- The [pipeline](../internal/pipeline/pipeline.go) orchestrates transformation, interceptors, CloudEvents, correlation, delivery policies, and failure handling.
- Link's [`LinkTarget` and `TargetStore`](../internal/link/config.go) provide stable target names and replaceable target configuration.
- The [Link HTTP proxy](../internal/link/proxy/http.go) applies routing, authentication, discovery, retries, circuit breaking, rate limiting, and request/response handling.
- Link has [per-target interceptor chains](../internal/link/interceptor/registry.go) that can adapt requests and responses.
- The [`internal/schema`](../internal/schema/) package contains schema-registry and codec groundwork.
- Kafka, Temporal, HTTP, gRPC, WASM/Wasmer, CloudEvents, and Kubernetes support prove that the runtime can mediate varied environments.

These components support the direction, but they do not yet form a complete Application Contract system.

## What Does Not Exist Yet

Fiso does not currently provide a first-class, versioned `ApplicationContract`, `Port`, `Operation`, `Binding`, or `Adapter` API, CRD, parser, validator, compatibility engine, or runtime resolver.

The existing schema-registry package is not an application-contract implementation. Existing `FlowDefinition` and `LinkTarget` resources are current deployment/configuration constructs, not the complete conceptual model. This vision does not decide how current configuration evolves or promise that existing concepts will be renamed directly.

Those decisions require focused scenarios, evidence, architectural decisions, compatibility analysis, and separately approved implementation slices.

## How Direction Becomes Work

Fiso uses the [80/20 Iterative Development Method](development-methodology.md) to turn this direction into small, measurable outcomes. Candidate work competes on the bounded [roadmap](roadmap.md); cross-cutting public-contract decisions require an [ADR](adr/README.md).

The vision stays durable. Priorities, experiments, designs, dates, and implementation status live elsewhere so that future work cannot be mistaken for shipped capability.
