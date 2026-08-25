# Product Vision

> **Directional document:** This document records Fiso's durable product intent. It is not a current API or runtime specification, does not assert that every described outcome is implemented, and does not approve implementation work. See the [project README](../README.md) and implementation evidence for current behavior.

## The Promise

**Fiso gives every application a stable, contract-defined world, independent of the systems that happen to exist around it.**

An application should be written against the interactions its business logic provides and requires, as if its surrounding environment were already consistent, reliable, modern, and designed for that application. Broker details, vendor SDKs, legacy payloads, credential mechanisms, retry policies, and migration state belong outside the application boundary.

This is the application's **perfect world**:

- Commands arrive in the form the application accepts;
- Queries return the information the application expects;
- Events express facts in stable, meaningful shapes;
- development can begin before real integrations are available;
- each environment can realize the same contract through mocks, legacy systems, or future platforms; and
- replacing an external provider does not require application-code changes.

The application remains a black box in implementation, but its boundary is explicit, versioned, observable, and testable.

## The Contract and the Runtime

The conceptual model describes relationships from the application's viewpoint. These arrows do **not** describe network traffic direction.

```text
                         Application Contract
                         /                  \
        provides Interaction              requires Interaction
       Command | Query | Event            Command | Query | Event
                         \                  /
                          \                /
                   realized for an environment by
                         Environment Binding
               [connectors | transformations | policies]
```

Runtime traffic direction is separate:

```text
External systems ── inbound via Fiso-Flow ──> Application
Application ── outbound via Fiso-Link ──> External systems
```

**Fiso-Flow** is the current foundation for adapting inbound traffic. Sources receive external interactions, transforms and interceptors adapt them, CloudEvents provide a standard envelope where applicable, and sinks deliver the result toward the application.

**Fiso-Link** is the current foundation for realizing outbound dependencies. Applications call stable local target names while Link handles concrete endpoints, authentication, discovery, retries, circuit breaking, rate limiting, protocol behavior, and adaptation.

The long-term goal is not to make application code understand more integration mechanisms. It is to make those mechanisms replaceable behind a stable application-facing contract.

## Principles

### 1. The application boundary is stable

Application code depends on domain-meaningful Interactions, not on whichever infrastructure or vendor exists today.

### 2. Integrations are inverted

The Application Contract declares what the application provides and requires. Environment Bindings realize that contract instead of allowing environmental details to shape business logic.

### 3. Development does not wait for integration

A local or test Environment Binding should supply contract-conforming behavior before production systems, topics, credentials, and provider sandboxes exist.

### 4. Adaptation is explicit and observable

Connectors, transformations, authentication, routing, resilience, and failure policies belong inside Environment Bindings. They must be declared, tested, and visible—not hidden in application code.

### 5. Provider migration preserves application behavior

Legacy and replacement providers may differ internally, but their Environment Bindings must satisfy the same Application Contract. Cutover and rollback should change an Environment Binding, not the application.

### 6. Compatibility is measured from the application outward

Infrastructure uptime alone is not success. An Environment Binding is compatible only when it fulfills the data, responses, errors, and behavioral expectations of the contract's Interactions.

### 7. Contract evolution is deliberate

Changes to application-facing behavior need explicit versions, compatibility rules, evidence, and migration paths.

## Conceptual Model

These three concepts describe product direction. They are not yet first-class Fiso APIs or custom resources.

### Application Contract

The versioned definition of the stable boundary an application provides to and requires from its environment. It names the Interactions, data shapes, responses, errors, and relevant behavioral expectations on both sides of that boundary.

Direction is always stated from the application's viewpoint:

- **provides:** the application accepts a Command, answers a Query, or emits an Event;
- **requires:** the application sends a Command, asks a Query, or consumes an Event.

Provides and requires describe contract relationships, not network traffic direction.

### Interaction

One named exchange across the Application Contract. Every Interaction has exactly one kind.

#### Command

A request expressing an intention to change state: **please make something happen**. A Command can succeed, be rejected, fail, or return a result. Command names should normally be imperative, such as `CreateOrder`, `ReserveInventory`, or `CancelPayment`.

#### Query

A request for information without an intended business-state change: **please tell me something**. A Query normally returns data or an error. Query names should communicate the information requested, such as `GetOrder`, `FindCustomer`, or `CheckInventory`.

#### Event

A fact that has already happened: **something happened**. Delivery can be acknowledged, but a consumer cannot semantically reject the historical fact described by the Event. Event names should normally use the past tense, such as `OrderCreated`, `PaymentAuthorized`, or `CustomerUpdated`.

“Action” is not a formal Interaction kind because it can ambiguously mean a Command, an internal step, or an Event.

### Environment Binding

The environment-specific realization of one or more contract Interactions.

For a required Query named `GetCustomer`:

```text
Development         GetCustomer ──> fixture-backed mock
Production today    GetCustomer ──> legacy CRM over SOAP and mTLS
Future production   GetCustomer ──> Customer Platform v2 over HTTP and OAuth
```

The Application Contract and application code remain stable. The Environment Binding contains technical details such as connectors, request and response transformations, authentication, routing, retries, circuit breaking, and other policies.

### Intentionally Not Core Concepts

- **Port** is excluded because it is easily confused with a network port and carries framework-specific meaning from ports-and-adapters architecture.
- **Operation** is excluded because it does not clearly distinguish a Command, a Query, and an Event.
- **Adapter** is a useful implementation word, but connectors and transformations are technical details within an Environment Binding rather than a concept every application team must model.
- **Capability** may later become optional grouping for large contracts, but it is not core vocabulary without concrete evidence that the grouping is necessary.

Current implementation terms such as `Source`, `Transformer`, `Sink`, `FlowDefinition`, and `LinkTarget` remain the names of existing constructs. They are foundations that can help realize this direction, not one-to-one equivalents of the three conceptual concepts.

## Desired Outcomes

The vision is realized when teams can demonstrate outcomes such as these.

### Build before integrations exist

A team defines the application's provided and required Interactions, selects mock or fixture-backed Environment Bindings, and implements and tests business logic without waiting for another team, broker, credential, or vendor environment.

### Normalize an imperfect inbound world

Fiso-Flow adapts a legacy event, webhook, CDC record, or API call arriving through inbound runtime traffic into a contract-conforming input at the application boundary. Depending on its kind and business direction, that input can be a provided Command or Query, or a required Event.

### Replace a downstream provider without changing the application

The application continues using the same required Interaction while its Environment Binding moves from a legacy service to a new platform. Both bindings pass the same conformance checks, and rollback restores the old binding.

### Test the boundary independently

Contract tests validate that an Environment Binding fulfills the inputs, outputs, errors, and behavior promised by its Interactions without requiring the full production topology.

### Detect incompatibility before deployment

Validation and CI reject an Environment Binding or provider change that would violate the Application Contract.

These are desired product outcomes, not claims that Fiso supports every scenario today.

## Current Foundation

Fiso already contains important building blocks for this direction:

- Flow's [`Source`](../internal/source/source.go), [`Transformer`](../internal/transform/transform.go), and [`Sink`](../internal/sink/sink.go) interfaces separate transport, adaptation, and delivery for inbound runtime traffic.
- The [pipeline](../internal/pipeline/pipeline.go) orchestrates transformation, interceptors, CloudEvents, correlation, delivery policies, and failure handling.
- Link's [`LinkTarget` and `TargetStore`](../internal/link/config.go) provide stable target names and replaceable configuration for outbound runtime traffic.
- The [Link HTTP proxy](../internal/link/proxy/http.go) applies routing, authentication, discovery, retries, circuit breaking, rate limiting, and request/response handling.
- Link has [per-target interceptor chains](../internal/link/interceptor/registry.go) that can transform requests and responses.
- The [`internal/schema`](../internal/schema/) package contains schema-registry and codec groundwork.
- Kafka, Temporal, HTTP, gRPC, WASM/Wasmer, CloudEvents, and Kubernetes support show that the runtime can mediate varied environments.

These are technical foundations from which future Environment Bindings could be built. They do not yet form a complete Application Contract system.

## What Does Not Exist Yet

Fiso does not currently provide first-class, versioned Application Contract, Interaction, or Environment Binding APIs, CRDs, parsers, validators, compatibility engines, conformance tooling, or runtime resolvers.

The existing schema-registry package is not an Application Contract implementation. Existing `FlowDefinition` and `LinkTarget` resources are current deployment and configuration constructs, not the conceptual model. This vision does not decide how current configuration evolves or promise that existing constructs will be renamed directly.

Those decisions require focused scenarios, evidence, architectural decisions, compatibility analysis, and separately approved implementation slices.

## How Direction Becomes Work

Fiso uses the [80/20 Iterative Development Method](development-methodology.md) to turn this direction into small, measurable outcomes. Candidate work competes on the bounded [roadmap](roadmap.md); cross-cutting public-contract decisions require an [ADR](adr/README.md).

The vision stays durable. Priorities, experiments, designs, dates, and implementation status live elsewhere so that future work cannot be mistaken for shipped capability.
