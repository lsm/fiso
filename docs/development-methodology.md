# 80/20 Iterative Development Method

## Purpose

Fiso improves by repeatedly selecting the smallest work slice likely to produce the greatest verified outcome. The method applies to correctness, security, reliability, contracts, documentation, performance, product features, brand, and community—not only to runtime code.

“80/20” is not permission to leave work unsafe or incomplete. It means investing first where a small, independently verifiable change can remove disproportionate user pain, risk, uncertainty, or adoption friction.

## Operating Principle

Prefer:

- outcomes over output;
- evidence over intuition alone;
- end-to-end slices over horizontal foundations with no user proof;
- reuse over parallel abstractions;
- reversibility over speculative completeness; and
- learning before scaling an approach.

## Triage Precedence

Confirmed security vulnerabilities, correctness defects, data-loss risks, and release or production blockers are triaged before normal ROI-ranked work.

Order urgent work using severity, reproducibility, exposure, and the smallest safe repair. Sensitive security reports must not be submitted through a public feature issue.

## Score Normal Work

For non-urgent candidates, use this comparative score:

```text
confidence × (reach × impact + 2 × risk reduction) ÷ effort
```

| Input | Scale | Meaning |
|---|---:|---|
| Reach | 1–5 | `1`: isolated edge case; `3`: meaningful user segment or component; `5`: project-wide or most target users |
| Impact | 1–5 | `1`: minor improvement; `3`: removes material friction; `5`: blocking, transformational, or essential to the product promise |
| Risk reduction | 0–5 | `0`: no meaningful reduction; `3`: removes significant reliability/adoption risk; `5`: removes critical security, data, operational, or trust risk |
| Confidence | 0.25, 0.5, 0.8, 1.0 | assumption, weak evidence, strong evidence, or measured/reproduced evidence |
| Effort | focused days | Expected implementation, verification, documentation, and rollback effort; a normal slice is about `0.5–3` days |

Scores compare candidates; they are not precise forecasts. Proposal authors provide evidence and an initial estimate. Maintainers validate the inputs, own final ranking, and may request a short measurement spike when confidence is low.

## The Slice Contract

Before a slice starts, it must:

1. fit within roughly two to three focused days or be split again;
2. make one observable claim;
3. state the problem, baseline, and available evidence;
4. define a deterministic verification method before implementation;
5. identify dependencies;
6. define a feasible rollback, stop, or disable path; and
7. produce a result that can be accepted, rejected, or measured independently.

A spike follows the same rules. Its output is a measurement or decision that raises confidence—not unowned production code.

## Portfolio Balance

Over approximately the last ten completed slices, target:

- **70% proven outcomes:** work supported by strong evidence and direct product value;
- **20% reliability and maintenance:** correctness, security, dependency health, operational quality, and debt that limits delivery; and
- **10% measured experiments:** reversible tests of uncertain product, technical, documentation, brand, or community hypotheses.

This is a portfolio signal, not a per-PR quota. Urgent security and correctness work can preempt it, and no repair should be delayed merely to satisfy a percentage.

## Work-in-Progress Limit

At most two selected slices may be active:

- one **foundation** slice, which improves shared correctness, verification, architecture, delivery, or maintainability; and
- one **product** slice, which directly proves a user or adoption outcome.

The foundation/product class and the 70/20/10 portfolio class are independent. A product slice can be an experiment; a foundation slice can be a proven outcome.

## Iteration Loop

1. **Intake** — describe the affected user, problem, desired outcome, evidence, score inputs, and proposed slice.
2. **Validate evidence** — reproduce the problem or establish a baseline. Reduce confidence when evidence is weak.
3. **Triage or score** — apply urgent precedence where justified; otherwise calculate the comparative score.
4. **Admit to the roadmap** — a candidate enters when the roadmap has capacity; once the cap is full, it must displace a less valuable active entry.
5. **Select the exact slice** — approval applies only to the documented slice, not its larger theme or follow-ups.
6. **Implement and verify** — keep scope at the observable claim and run the predetermined acceptance method.
7. **Record the outcome** — capture result, evidence, unexpected cost, and whether rollback was needed.
8. **Stop, expand, or re-score** — close successful work, stop failed hypotheses, or submit the next slice as a newly scored candidate.

## Re-ranking Triggers

Re-rank when evidence changes materially:

- a slice completes;
- user or runtime evidence arrives;
- an incident, vulnerability, or correctness failure occurs;
- a benchmark regresses;
- support friction repeats;
- effort, confidence, or dependencies change materially; or
- an experiment disproves its hypothesis.

Elapsed time alone is not a reason to churn priorities. No standing ceremony is required when no meaningful evidence changed.

## Evidence Record

The issue and pull request should preserve:

- affected user and problem;
- baseline and evidence links;
- score inputs and rationale;
- smallest slice and observable claim;
- verification method and result;
- rollback or stop decision; and
- a separately scored follow-up issue, if one is justified.

## Definition of Done

A slice is done when:

- its acceptance method passes and the result is recorded;
- the observable claim is supported—or the failed hypothesis is explicitly closed;
- relevant public behavior, examples, and decisions are updated;
- rollback remains understood;
- the applicable user-visible change is recorded under `[Unreleased]`; and
- follow-up work is re-evaluated rather than automatically promoted.

## Anti-Patterns

Avoid:

- giant epics described as “one slice”;
- unbounded iceboxes or “someday” lists;
- scoring without evidence or stable scales;
- building frameworks for hypothetical future needs;
- equating roadmap rank with a release promise;
- raising raw test counts without identifying an uncovered outcome;
- continuing a failed experiment because time has already been spent; and
- allowing urgent work to become a permanent excuse for never measuring priorities.

See the bounded [roadmap](roadmap.md) for current candidates and [CONTRIBUTING.md](../CONTRIBUTING.md) for the proposal workflow.
