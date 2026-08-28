# ADR 0004: Report Static Validation as Validated

- **Status:** Accepted
- **Decision date:** 2026-08-28

## Context

The Fiso operator's controllers perform static spec validation only: they parse `FlowDefinition` and `LinkTarget` resources, validate the spec, and write `.status.phase`. They do not create Deployments, Services, or ConfigMaps, do not start or configure a runtime, and never observe runtime health.

Despite that, successful reconciliation reported phase `Ready` with messages like "Flow definition validated and active". `Ready` and "active" are runtime claims. A user or automation reading `.status.phase: Ready` reasonably concludes a Flow is serving traffic — when in fact nothing outside the API server exists. The phase overstated state the operator never observed, the same operational-truth defect class as advertising unexecutable integrations (ADR 0003).

`.status.phase` is externally visible: scripts, `kubectl` waits, and the operator E2E suite consume the literal value, so changing it is a public contract change that needs an explicit decision.

## Decision

The operator reports exactly two phases, both about static validation:

- `Validated` — static spec validation succeeded. The message is validation-only (`Flow definition validated` / `Link target validated`); it never mentions activation or readiness.
- `Error` — static validation failed; `.status.message` carries the reason.

Phase values come from shared constants in `api/v1alpha1` (`PhaseValidated`, `PhaseError`), used by both the controller-runtime controllers and the lightweight reconciler so the paths cannot drift.

The operator must not emit any phase that asserts runtime state (`Ready`, `Active`, `Available`) until it actually actuates and observes a runtime; introducing such a phase requires a new ADR covering actuation, observation, and the transition rules between static and runtime phases.

## Compatibility

- `FlowDefinitionStatus.Phase` and `LinkTargetStatus.Phase` are plain strings with no CRD enum, so emitting `Validated` requires no CRD or API version change.
- Previously stored `Ready` values remain readable; controllers simply stop writing them. A resource reconciled after this change transitions to `Validated` (or `Error`).
- Scripts and automation waiting on `phase: Ready` must switch to `phase: Validated`. This is the intended tightening: their previous wait was asserting something untrue.

## Scope and Non-Decisions

This decision governs operator status semantics only.

It does **not**:

- add runtime actuation (Deployments, Services, ConfigMaps, pipelines);
- add Kubernetes Conditions, `observedGeneration`, or a new CRD version;
- define a runtime `Available`/`Ready` phase; or
- change the CRD status schema beyond the values written into it.
