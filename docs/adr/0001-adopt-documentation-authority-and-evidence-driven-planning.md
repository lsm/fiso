# ADR 0001: Adopt Documentation Authority and Evidence-Driven Planning

- **Status:** Proposed
- **Decision date:** 2026-08-20

## Context

Fiso's root README has grown into the de facto product introduction, architecture overview, configuration reference, deployment guide, operations manual, development guide, and migration record. The draft v1.3 high-level design specification mixes durable rationale with architecture and phase statements that have since been superseded by implemented capabilities such as multi-flow execution, Fiso-Link, Kubernetes resources, Temporal, gRPC, and WASM/Wasmer.

The repository previously had no explicit authority for product direction, prioritization, current roadmap, or cross-cutting decisions. Without clear boundaries, future intent could be mistaken for shipped behavior, stale plans could appear current, and a valuable product discussion could become another document that drifts from implementation.

Fiso also needs a lightweight way to direct limited effort toward small changes with disproportionate verified value across runtime correctness, contracts, documentation, performance, product capabilities, brand, and community.

## Decision

Fiso adopts:

1. a [documentation map and authority policy](../README.md);
2. a canonical [product vision](../product-vision.md);
3. an [80/20 iterative development method](../development-methodology.md);
4. a bounded, evidence-ranked [roadmap](../roadmap.md);
5. structured proposal intake through the repository issue form;
6. this minimal ADR practice for durable cross-cutting decisions; and
7. a concise [contributor workflow](../../CONTRIBUTING.md) that links these authorities rather than duplicating them.

The root README remains the primary user-facing overview of current behavior. Code, public API/configuration definitions, and tests remain the final implementation evidence. The changelog records pending user-visible deltas under `[Unreleased]` and shipped history in versioned sections.

## Scope and Non-Decisions

This decision governs documentation authority and work-selection process only.

It does **not**:

- approve runtime or API implementation of the directional Application Contract, Interaction, or Environment Binding concepts;
- approve a new CRD or API version;
- decide configuration migration or compatibility rules;
- approve a canonical local verification target;
- approve a benchmark framework;
- approve any roadmap candidate, dependency, or follow-up merely because it is recorded; or
- establish a release date or phase plan.

Any such work must be framed as a small slice, supported by evidence, selected separately, and preceded by another ADR when it changes a public contract or cross-cutting architecture.

## Consequences

- Readers can distinguish current capability, durable direction, active hypotheses, accepted decisions, release history, and historical context.
- Proposed work carries explicit evidence, effort, verification, rollback, and scope.
- The roadmap remains small enough to be a decision surface rather than an unbounded backlog.
- The project accepts a small maintenance obligation: canonical documents and their links must remain internally consistent.
- Directional terminology must remain clearly marked until corresponding product contracts are implemented and shipped.

## Alternatives Considered

### Continue using the root README alone

Rejected because one document cannot reliably act as current reference, durable strategy, ranked roadmap, decision history, and contributor process without ambiguity and drift.

### Rewrite the high-level design specification as the master document

Rejected because it already conflates architecture, roadmap, and implementation status. Selective modernization would hide its historical value and make obsolete claims appear authoritative.

### Introduce a comprehensive planning platform and governance process

Rejected because project history and contributor scale favor a lightweight method. The selected approach adds only the artifacts needed to preserve decisions and choose high-value slices.

## References

- [Project README](../../README.md)
- [Documentation authority](../README.md)
- [Product vision](../product-vision.md)
- [80/20 iterative development method](../development-methodology.md)
- [Roadmap](../roadmap.md)
- [Historical HLD draft](../hld-specification.md)
