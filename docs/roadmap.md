# Roadmap

> **A ranked decision surface, not a release plan.** A candidate is neither approval nor a product promise. Only the exact slice marked `approved` or `in progress` has been selected, and no status promises a release date. Cross-cutting technical design still requires an ADR when the [documentation policy](README.md#when-to-write-an-adr) calls for one.

The roadmap applies the [80/20 Iterative Development Method](development-methodology.md) to the small set of uncertainties currently worth active attention.

## Operating Rules

- Keep at most **10 active entries** across `candidate`, `approved`, and `in progress`.
- Keep at most one foundation and one product slice selected across `approved` and `in progress`; approval reserves that class's WIP slot until the slice completes or returns to `candidate`.
- Maintain no icebox, someday list, or exhaustive phase plan here. Broader ideas remain discoverable in issues.
- Adding an eleventh entry requires removing an existing entry from this page first; preserve the broader idea in its issue if it remains useful.
- `approved` applies only to the exact smallest slice written here, not its theme, dependencies, or likely follow-ups.
- Remove completed work from this page. Preserve its outcome in the issue and PR, an ADR when applicable, and the changelog when a user-visible change ships.
- Re-rank only after a material evidence trigger defined by the methodology.
- Unknown inputs block selection; do not manufacture precision to force a rank.

## Status and Classification

- **Status:** `candidate`, `approved`, or `in progress`
- **WIP class:** `foundation` or `product`
- **Portfolio class:** `proven outcome`, `reliability/maintenance`, or `experiment`

## Active Entries

All seeded entries are unapproved candidates from the August 2026 repository audit. Their ordering reflects dependency and current evidence, not release commitment.

### 1. Inventory local and CI verification truth

- **Status:** candidate
- **WIP class:** foundation
- **Portfolio class:** reliability/maintenance
- **Problem and outcome:** The [`Makefile`](../Makefile) and [CI workflow](../.github/workflows/ci.yml) enumerate different coverage thresholds, checks, and E2E surfaces. Establish an accurate inventory before calling any local command canonical.
- **Evidence and baseline:** `make coverage-check` enforces 95%, CI enforces 94.5%; `make e2e-all` covers fewer suites than CI; `make checks` omits tests and lint.
- **Score inputs:** reach `5`; impact `4`; risk reduction `4`; confidence `1.0`; effort `1` focused day. Provisional score: `28`. Maintainers must validate effort at selection time.
- **Smallest slice:** Map every final-gate CI job to its local command, or explicitly classify it as CI-only, without changing either implementation.
- **Observable claim:** Every job required by CI's final gate has an identified local equivalent or a documented reason it cannot run locally.
- **Verification:** Review the inventory against `.github/workflows/ci.yml`, `Makefile`, and E2E directories; mechanically check for unmatched final-gate dependencies.
- **Rollback:** Revert the inventory document; no build behavior changes.
- **Dependencies:** None.
- **Last material evidence change:** 2026-08-20 repository audit.

### 2. Add one canonical local verification entry point

- **Status:** candidate
- **WIP class:** foundation
- **Portfolio class:** reliability/maintenance
- **Problem and outcome:** Contributors lack one documented command for the agreed local verification subset, increasing local/CI drift.
- **Evidence and baseline:** Same repository audit as entry 1; no current Make target equals the complete CI gate.
- **Score inputs:** reach `5`; impact `4`; risk reduction `4`; confidence `0.8`; effort unknown until entry 1 completes. No score or selection until effort is known.
- **Smallest slice:** After the inventory, add one command that runs the agreed local subset and fails when any constituent check fails. Split it if the work exceeds three focused days.
- **Observable claim:** One documented local command executes every agreed local constituent exactly once and propagates failure.
- **Verification:** Exercise one passing run and representative constituent failures; compare the command with the completed inventory.
- **Rollback:** Remove the entry point and restore its documentation while retaining the inventory.
- **Dependencies:** Entry 1.
- **Last material evidence change:** 2026-08-20 repository audit.

### 3. Trial a public-claim-to-evidence matrix

- **Status:** candidate
- **WIP class:** product
- **Portfolio class:** experiment
- **Problem and outcome:** The README contains many capability claims, but there is no compact way to see which high-value claims have executable evidence. Test whether a small matrix improves trust without creating maintenance-heavy governance.
- **Evidence and baseline:** The repository has broad unit, integration, E2E, and smoke coverage, but examples and claims are not mapped consistently to that evidence.
- **Score inputs:** reach `4`; impact `3`; risk reduction `2`; confidence `0.5`; effort `2` focused days. Provisional score: `4`.
- **Smallest slice:** Select three to five high-value claims across Flow, Link, and Operator and map each to the cheapest deterministic proof plus one deeper test where useful.
- **Observable claim:** Maintainers can identify a real evidence gap or remove redundant documentation effort using the trial matrix.
- **Verification:** Have the selected commands resolve and run where practical; record maintenance cost and a continue/stop decision.
- **Rollback:** Delete the trial matrix if it adds no actionable signal; preserve any discovered gaps as issues.
- **Dependencies:** Entry 1 can improve evidence classification but is not required for the trial.
- **Last material evidence change:** 2026-08-20 repository audit.

## Entry Template

Use this format only when a candidate is strong enough to compete for one of the ten slots:

```markdown
### N. Outcome-oriented title

- **Status:** candidate | approved | in progress
- **WIP class:** foundation | product
- **Portfolio class:** proven outcome | reliability/maintenance | experiment
- **Problem and outcome:**
- **Evidence and baseline:**
- **Score inputs:** reach; impact; risk reduction; confidence; effort; score or urgent rationale
- **Smallest slice:**
- **Observable claim:**
- **Verification:**
- **Rollback:**
- **Dependencies:**
- **Last material evidence change:**
```

The [product vision](product-vision.md) contains long-term direction. Do not copy its possible dependency chain into this active roadmap. The next technical step toward that vision must enter through evidence-based intake and compete as a small slice.
