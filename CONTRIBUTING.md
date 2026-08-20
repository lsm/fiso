# Contributing to Fiso

Thank you for improving Fiso. The project uses a lightweight, evidence-driven process so that limited effort goes to small changes with disproportionate verified value.

## Start Here

Read these before proposing substantial work:

- [Documentation map and authority](docs/README.md)
- [Product vision](docs/product-vision.md)
- [80/20 iterative development method](docs/development-methodology.md)
- [Current roadmap](docs/roadmap.md)
- [Architecture decision records](docs/adr/README.md)

The vision describes direction, not current capability. The roadmap is a bounded decision surface, not a release promise.

## Propose Work

1. Search existing issues and the roadmap for the same problem or evidence.
2. For normal work, open an **Outcome or feature proposal** and provide the affected user, desired outcome, evidence, score inputs, smallest slice, verification, rollback, and dependencies.
3. Do not disclose sensitive vulnerability details in a public issue. Use the repository owner's private contact or GitHub private vulnerability reporting when available.
4. Maintainers validate score inputs and select the exact slice. Submitting or ranking a proposal does not approve implementation.

Confirmed security, correctness, data-loss, and production blockers can bypass normal ROI ordering, but still need the smallest safe repair and objective verification.

## Before Implementation

- Obtain approval for the exact independently verifiable slice, not just its broader theme.
- Keep a normal slice within roughly two to three focused days; split larger work.
- Write an ADR first when the change affects a public contract, compatibility, cross-component architecture, security or delivery semantics, or project governance.
- Reuse existing abstractions and patterns before introducing parallel mechanisms.

## Verification

The [Makefile](Makefile) exposes current local build and test commands, and the [CI workflow](.github/workflows/ci.yml) is the complete remote gate.

There is currently no single local command equivalent to the entire CI gate. In particular, do not describe `make checks` or `make e2e-all` as canonical or exhaustive. Run the commands relevant to the affected capability and report exactly what ran and what was skipped.

Common starting points include:

```bash
make test
make coverage-check
make fmt-check mod-check lint vulncheck
make test-integration
make build-all
```

Run focused unit, integration, E2E, smoke, or benchmark evidence appropriate to the observable claim. A documentation-only change should prioritize link, schema, rendering, and consistency checks rather than treating unrelated Go tests as its primary proof.

## Pull Request Evidence

A pull request should state:

- linked issue or urgent rationale;
- the one observable claim;
- verification performed and its result;
- anything not tested;
- rollback or disable path;
- public documentation and examples changed; and
- ADR changes, when required.

If the hypothesis fails, record the result and stop or re-scope. Do not automatically promote follow-up work; submit it as a newly evidenced and scored slice.

## Documentation Responsibilities

Follow the [documentation authority policy](docs/README.md):

- update current behavior documentation with public behavior changes;
- keep future direction out of current capability claims;
- preserve accepted decisions through ADR supersession rather than rewriting history; and
- update the changelog only for user-visible changes that ship.
