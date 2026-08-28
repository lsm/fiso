# ADR 0005: Drop Readiness on Required Pipeline Termination

- **Status:** Accepted
- **Decision date:** 2026-08-28

## Context

Flow-capable binaries (`fiso-flow`, `fiso-flow-wasmer`, `fiso-wasmer-aio`) set process readiness once at startup and never revisit it. `health.SetReady(true)` fires after the pipelines are *built* but before any source binds a listener, and each runner goroutine consumes `pipeline.Run`'s return value only for logging. The only `SetReady(false)` is on the signal-triggered shutdown path.

The result: a configured flow whose source dies — for example, its listener cannot bind — leaves `/readyz` at 200 indefinitely. Kubernetes readiness probes (such as `deploy/examples/flow-deployment.yaml`) keep routing traffic to a process that can never serve part of its configured surface. Readiness asserted something the process never observed, the runtime-side counterpart of the operator status defect ADR 0004 corrected.

A subtlety discovered during qualification: `pipeline.Run` returns the source's `Start` error verbatim, and every real source returns a non-nil `ctx.Err()` on graceful cancellation. So terminality cannot be classified by `err != nil` — normal shutdown would be indistinguishable from failure.

## Decision

Every configured startup pipeline is a **required runner** of its process:

1. Readiness becomes true once all required runners have been launched (`Gate.SetRunning`), and stays false if any runner already returned terminally before that point — a failed startup is never overwritten by a late SetRunning.
2. A runner's return is **terminal** when it is an error other than `context.Canceled`/`context.DeadlineExceeded`, or an unexpected `nil` (a pipeline that silently stopped serving). The first terminal return drops readiness for the rest of the process lifetime.
3. Expected cancellation during shutdown is not terminal; the shutdown path owns that readiness transition.
4. On a terminal return the process stays alive, `/healthz` (liveness) is unchanged, and surviving runners are untouched — recovery requires a process restart. With zero required runners (the `fiso-wasmer-aio` shape, where flow-config loading may legitimately yield no flows), readiness holds.

The policy is implemented once in `internal/flowruntime.Gate`, shared by all Flow-capable binaries so their behavior cannot drift.

## Scope and Non-Decisions

This decision governs aggregate process readiness for Flow-capable binaries.

It does **not**:

- restart or supervise terminated pipelines (restart, backoff, and per-flow recovery remain future work with their own design);
- add per-flow readiness granularity or per-flow endpoints;
- exit the process on terminal failure (liveness and orchestration stay with the supervisor);
- supervise the shared HTTP source pool goroutine (same silent-terminal class, separate slice); or
- change the operator's status phases (see ADR 0004).
