# Architecture Decision Records

Architecture Decision Records (ADRs) preserve the context, choice, scope, and consequences of durable cross-cutting decisions. They make later changes easier to evaluate without turning current implementation details into permanent policy.

See the [documentation authority policy](../README.md#when-to-write-an-adr) for when a decision needs an ADR.

## When to Use an ADR

Write an ADR for decisions that materially affect public contracts or compatibility, architecture across components, security or delivery semantics, or Fiso's governance model.

Do not require an ADR for routine bug fixes, isolated implementation choices governed by an existing decision, documentation corrections, or roadmap ranking.

## Naming and Status

Use zero-padded, lowercase kebab-case filenames:

```text
0001-short-decision-title.md
```

Supported statuses are:

- **Proposed** — under review; not adopted
- **Accepted** — adopted and authoritative within its scope
- **Rejected** — considered but not adopted
- **Superseded** — replaced by a later ADR, which must be linked

Once accepted, do not substantively rewrite an ADR. Correct trivial errors or add supersession metadata, but record a changed decision in a new ADR.

## Index

| ADR | Status | Decision |
|---|---|---|
| [0001](0001-adopt-documentation-authority-and-evidence-driven-planning.md) | Accepted | Adopt documentation authority and evidence-driven planning |
| [0002](0002-make-export-fail-closed-on-lossy-conversion.md) | Accepted | Make export fail closed on lossy conversion |
| [0003](0003-define-supported-integration-values-as-executable.md) | Accepted | Define supported integration values as executable |
| [0004](0004-report-static-validation-as-validated.md) | Accepted | Report static validation as Validated |
| [0005](0005-drop-readiness-on-required-pipeline-termination.md) | Accepted | Drop readiness on required pipeline termination |
| [0006](0006-wasm-http-via-host-function.md) | Accepted | WASM HTTP calls via a host function through Fiso-Link |
| [0007](0007-interceptor-rejection-contract.md) | Accepted | Interceptor rejection contract |
| [0008](0008-interceptor-env-configuration.md) | Accepted | Interceptor environment configuration |

## Template

Copy this template into the next numbered file:

```markdown
# ADR NNNN: Decision Title

- **Status:** Proposed
- **Decision date:** YYYY-MM-DD

## Context

What problem, evidence, constraints, and forces require a decision?

## Decision

What is being decided?

## Scope and Non-Decisions

What does this decision govern, and what remains explicitly undecided?

## Consequences

What becomes easier, harder, required, or constrained?

## Alternatives Considered

What credible alternatives were considered, and why were they not selected?

## References

Links to issues, evidence, specifications, or related ADRs.
```
