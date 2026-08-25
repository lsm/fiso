# Fiso Documentation

This page explains where Fiso's product direction, current behavior, decisions, work priorities, and release history are recorded. When documents appear to disagree, use the authority and lifecycle rules below rather than treating every page as equally current.

## Start Here

- [Project README](../README.md) — current user-facing overview, quick start, and supported behavior
- [Product Vision](product-vision.md) — durable direction for the application boundary Fiso aims to provide
- [80/20 Iterative Development Method](development-methodology.md) — how work is framed, selected, and evaluated
- [Roadmap](roadmap.md) — bounded, evidence-ranked candidates and explicitly selected slices
- [Contributing](../CONTRIBUTING.md) — how to propose, implement, and verify a change
- [Architecture Decision Records](adr/README.md) — accepted cross-cutting decisions and their history
- [Changelog](../CHANGELOG.md) — pending `[Unreleased]` entries and versioned release history

## Document Authority

| Subject | Authority | Lifecycle |
|---|---|---|
| Current behavior and supported public surface | Code, public API/configuration definitions, tests, the [project README](../README.md), and current topic guides | Updated with behavior |
| Product purpose and directional vocabulary | [Product Vision](product-vision.md) | Durable direction; not evidence of availability |
| Prioritization and delivery process | [80/20 Iterative Development Method](development-methodology.md) | Durable working method |
| Current ranked hypotheses and selected slices | [Roadmap](roadmap.md) | Dynamic and deliberately bounded |
| Cross-cutting architecture and public-contract decisions | [ADRs](adr/README.md) | Durable; superseded explicitly |
| Pending and shipped user-visible deltas | [Changelog](../CHANGELOG.md) | `[Unreleased]` is pending; versioned sections are shipped history |
| Superseded design context | Historical documents such as the [v1.3 HLD draft](hld-specification.md) | Preserved, non-authoritative |

Implementation evidence has the final say about what the current software actually does. Prose should make that behavior understandable, but prose cannot create a capability that the code does not implement.

## Conflict-Resolution Rules

1. A statement in the product vision describes direction; it does not prove that a capability or API exists.
2. A roadmap candidate is not approval. Only the exact slice marked `approved` or `in progress` has been selected, and it still carries no release-date promise.
3. An accepted ADR approves only the decision within its stated scope. It does not prove that implementation is complete.
4. Historical documents never override current behavior documentation or implementation evidence.
5. If a current guide conflicts with code, public API definitions, configuration schemas, or tests, treat the conflict as a defect to reconcile. Do not silently choose the more convenient claim.
6. A change to public behavior must update the corresponding current documentation.
7. Record a merged user-visible delta under `[Unreleased]`; it becomes shipped release history only when moved into a versioned section as part of a release.

## Document Lifecycles

- **Current** documents describe behavior users can rely on now and change alongside that behavior.
- **Directional** documents describe durable product intent and must clearly distinguish desired outcomes from shipped capabilities.
- **Dynamic** documents, such as the roadmap, are working decision surfaces and change as evidence changes.
- **Decision records** preserve the context, choice, and consequences of an accepted decision. They are superseded rather than rewritten.
- **Historical** documents are retained for context but begin with a visible non-authoritative warning.

## When to Write an ADR

Write an ADR before implementing a decision that materially changes:

- a public contract, API, compatibility rule, or configuration model;
- architecture across multiple components;
- security, delivery, lifecycle, or operational semantics; or
- this documentation-authority or development methodology.

An ADR is normally unnecessary for a routine bug fix, an isolated implementation detail already governed by an accepted decision, a prose correction, or roadmap ranking.

## Writing Conventions

- Use lowercase kebab-case filenames under `docs/`.
- Use repository-relative links.
- Describe implemented behavior in present tense. Label future behavior as direction, a hypothesis, or a proposal.
- Use current implementation names such as `Source`, `Sink`, and `LinkTarget` when documenting existing code.
- Use **Application Contract**, **Interaction**, and **Environment Binding** as the core directional concepts. An Interaction is exactly one of **Command**, **Query**, or **Event**.
- Use **provides** and **requires** from the application's viewpoint: the application provides Interactions to and requires Interactions from its environment.
- Reserve **inbound** and **outbound** for runtime traffic through Fiso-Flow and Fiso-Link; do not use them as substitutes for provides and requires.
- Treat connectors, transformations, authentication, resilience, and other policies as technical details inside an Environment Binding.
- Do not elevate Port, Operation, Adapter, standalone Binding, or Capability to core directional concepts. Ordinary network ports and current implementation symbols remain valid technical language.

## Current Guides

- [Wasmer Integration Guide](wasmer-integration.md)
- [WASM Deployment Guide](wasm-deployment.md)
- [Debezium CDC Guide](debezium-cdc.md)

## Historical Documents

- [High-Level Design Specification v1.3 draft](hld-specification.md) — preserved design context with obsolete phase and capability statements
