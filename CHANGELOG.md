# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project does not yet use semantic versioning — entries are grouped by
unreleased work and will be versioned when a release tag is cut.

---

## [Unreleased]

### Changed

- **Corrected the WASM/Wasmer capability contract** — authoritative
  documentation no longer claims WASIX sockets, threading, database
  connectivity, or full in-guest applications (Django/FastAPI/Next.js) for
  the Wasmer runtime. The executable contract is now stated explicitly:
  WASM modules are invoked per request over a host-side HTTP facade, with no
  network access, no threads, and no persistent in-memory state between
  invocations (files written through a configured preopen do persist). The
  wasmer engine delivers input via a mapped `--stdin-file` argument rather
  than stdin — a stdin-based module does not work unchanged under wasmer.
  Configuration knobs with no runtime effect (`execution`, `memoryMB` on
  apps; `timeout`/`memoryLimit`/`env`/`preopens` on interceptors) are
  documented as such. A documentation-contract test under `test/contracts`
  now rejects unsupported capability claims in authoritative docs.

### Removed

- **Advertised-but-unimplemented `wasmer-app` interceptor type** — accepted by
  Flow validation but implemented by no binary. It is now rejected with an
  actionable error until a binary executes it (ADR 0003).

### Fixed

- **No more silent WASM downgrades** — the plain `fiso-flow` binary now
  rejects `runtime: wasmer` with an explicit error instead of silently
  running the module under wazero, and `fiso-wasmer-aio` fails construction
  on unsupported interceptor types instead of silently skipping them.

## [0.20.0] — 2026-08-31

### Added

- **Executable Flow gRPC sink** — all Flow-capable binaries (`fiso-flow`,
  `fiso-flow-wasmer`, `fiso-wasmer-aio`) now construct `sink.type: grpc` with
  the shipped raw-unary gRPC sink (`config.address` required; optional
  non-negative `config.timeout` duration), matching what validation already
  accepted. Flow and operator validation now reject grpc sinks without a usable
  `address`, with a non-string, negative, or zero `timeout` (the sink treats
  zero as its default), or with any `tls` setting other than an explicit false —
  TLS is rejected until the sink supports credentials, instead of silently
  downgrading to an insecure connection.

### Changed

- **Corrected the Flow configuration reload contract** — authoritative
  documentation no longer claims Flow configuration "hot-reloads on changes".
  File watching detects and reparses changed files into the Loader's in-memory
  definitions only; running pipelines are not rebuilt, replaced, or stopped.
  Restart the process to apply configuration changes. A documentation-contract
  test under `test/contracts` now rejects hot-reload claims in authoritative
  docs and requires the restart limitation to stay explicit. Live reload
  remains future work with its own contract.

- **Readiness follows required pipeline lifecycle** — all Flow-capable binaries
  (`fiso-flow`, `fiso-flow-wasmer`, `fiso-wasmer-aio`) now drop `/readyz` to 503
  as soon as any configured startup pipeline terminates (an error other than
  context cancellation, or an unexpected silent stop), while `/healthz`, the
  process, and surviving pipelines are unaffected. Previously readiness was set
  once at startup and never revisited, so a flow whose source died (e.g. its
  listener could not bind) left `/readyz` at 200 forever. Recovery requires a
  process restart. See
  [ADR 0005](docs/adr/0005-drop-readiness-on-required-pipeline-termination.md).

- **Truthful operator status phases** — successful operator reconciliation now
  reports `.status.phase: Validated` with a validation-only message
  ("Flow definition validated" / "Link target validated") instead of `Ready`
  and "validated and active". The operator performs static spec validation
  only — it creates no runtime and observes none, so it no longer claims
  readiness or activation. `Error` is unchanged. Previously stored `Ready`
  values remain readable; scripts waiting on `phase: Ready` must wait on
  `phase: Validated` instead. See
  [ADR 0004](docs/adr/0004-report-static-validation-as-validated.md).

### Removed

- **Advertised-but-unexecutable Link gRPC** — Link `protocol: grpc` never had a
  transport; the shared proxy treats non-Kafka protocols as HTTP URL schemes.
  Local validation, operator validation, and the LinkTarget CRD now reject it.
  Reintroduction requires a Link gRPC routing contract with executable evidence
  (ADR 0003).

### Fixed

- **Kafka targets honor retry configuration and request cancellation** — the
  Fiso-Link Kafka publish path now retries through the shared retry engine.
  Previously the between-attempts backoff wait never executed (the per-attempt
  context was cancelled before the wait, so retries ran back-to-back),
  `initialInterval`/`maxInterval`/`jitter` were ignored (only `maxAttempts`
  was read), and cancelling the request did not stop the remaining attempts.
  Retries now follow the documented exponential schedule with jitter, wait on
  the request context so cancellation aborts the sequence promptly, and keep
  a fresh 30-second publish timeout per attempt; `maxAttempts` remains total
  attempts. Operators should expect previously instant retries to actually
  pause between attempts (bounded by `maxAttempts × maxInterval × (1 + jitter)`
— jitter applies after the max cap).
  Documentation now states that only exponential backoff is implemented — the
  `backoff` field is accepted but has no runtime effect — and the Kafka
  optional-fields table documents the `retry.*` settings and their defaults.

- **Fail-closed Kubernetes export** — `fiso export` now rejects local Flow or
  Link configuration that cannot be represented losslessly by the checked-in
  `fiso.io/v1alpha1` CRDs, identifies the unsupported resource field, and emits
  no partial YAML.

### Security

- **Go toolchain and dependency vulnerability remediation** — upgraded the root
  module and CI to Go 1.25.14, OpenTelemetry to 1.44.0 with contrib
  instrumentation 0.69.0, gRPC to 1.82.1, `golang.org/x/net` to 0.55.0,
  and `golang.org/x/text` to 0.39.0. These versions clear the 22 reachable
  vulnerabilities reported by `govulncheck` on `main`.

---

## [0.19.0] — 2026-04-03

### Added

- **Product direction and lightweight project governance** — added a directional product vision built around Application Contracts, Interactions, and Environment Bindings; documented authority and ADR practices; established an evidence-ranked roadmap and 80/20 development method; and added contributor and proposal-intake guidance. These documentation and governance additions do not introduce runtime or API behavior.

- **Configurable Kafka commit policies** (`errorHandling.commitPolicy`).
  Three modes are available for Kafka-source flows:

  | Policy | Offset committed when… |
  |---|---|
  | `sink` | Sink delivery succeeds (strict; pipeline stalls on failure) |
  | `sink_or_dlq` | Sink succeeds **or** DLQ write succeeds (default) |
  | `kafka_transaction` | Atomically with the sink produce (EOS) |

  The default when `commitPolicy` is omitted is `sink_or_dlq`, preserving
  backward-compatible behaviour for existing flows.

- **Transactional exactly-once semantics (EOS)** for Kafka-to-Kafka flows
  via `commitPolicy: kafka_transaction`.  Uses franz-go's
  `GroupTransactSession` to wrap the consumer-offset commit and the sink
  `Produce` in a single Kafka transaction.  Requires:
  - `source.type: kafka` and `sink.type: kafka` on the same cluster.
  - `errorHandling.transactionalId` set to a unique, stable identifier
    per consumer instance.

- **`errorHandling.transactionalId`** config field — required when
  `commitPolicy: kafka_transaction`; validated at startup.

- **`internal/delivery` package** — new package exposing:
  - `CommitPolicy` type with constants `sink`, `sink_or_dlq`,
    `kafka_transaction`.
  - `NormalizeCommitPolicy` / `ValidateCommitPolicy` helpers.
  - `WithKafkaTransactionalProducer` / `KafkaTransactionalProducerFromContext`
    for propagating a transactional producer through the call stack via
    `context.Context`.

- **E2E tests for all commit policy modes** in
  `test/e2e/kafka-commit-policies/`:
  - `sink` — verifies delivery and consumer-group lag reaches zero.
  - `sink_or_dlq` — verifies good messages reach the sink and deliberately
    failed messages land in the DLQ topic.
  - `kafka_transaction` — verifies committed messages appear in the sink
    topic under `read_committed` isolation.

- **`all-tests-passing` CI gate job** — lightweight job that depends on
  every other CI job.  The branch-protection ruleset for `main` now
  requires only this single check, automatically covering all current and
  future jobs.

- **Example flow configs** in `examples/flow/kafka-commit-policies/` with
  thorough inline comments explaining each policy's trade-offs and
  operational requirements.

### Fixed

- **`fiso-wasmer-aio` sink type switch** — added missing `default` branch;
  previously an unrecognised sink type would silently leave the sink as
  `nil`, causing a nil-pointer panic inside `pipeline.New`.

- **`fiso-wasmer-aio` DLQ publisher** — replaced the no-op DLQ publisher
  stub with a real `kafka_source.NewPublisher` for Kafka-source flows,
  matching the behaviour of `fiso-flow` and `fiso-flow-wasmer`.

- **`fiso-wasmer-aio` WASM factory** — moved `wasmruntime.NewFactory()`
  outside the interceptor loop so the factory is created once and reused
  across all WASM interceptors instead of once per interceptor.

- **Lint (SA1012)** — `internal/delivery/tx_context_test.go` nil-context
  guard test now carries a `//nolint:staticcheck` directive; the function
  explicitly handles `nil` as a valid input and the test is intentional.

### Security

- **Go 1.25.8** — upgraded the Go toolchain in `go.mod` and all CI jobs
  from 1.25.7 to 1.25.8, which fixes three standard-library CVEs:
  - `GO-2026-4601`: incorrect parsing of IPv6 host literals in `net/url`.
  - `GO-2026-4602`: `FileInfo` can escape from a `Root` in `os`.
  - `GO-2026-4603`: URLs in `<meta>` content attribute not escaped in
    `html/template`.

### Changed

- **All Go dependencies updated** to their latest available versions
  (`go get -u ./... && go mod tidy`).  Notable upgrades include
  `go.opentelemetry.io/otel` v1.35 → v1.43,
  `google.golang.org/grpc` v1.74-dev → v1.80,
  `go.temporal.io/sdk` v1.39 → v1.41, and
  `sigs.k8s.io/controller-runtime` v0.22 → v0.23.

---

[Unreleased]: https://github.com/lsm/fiso/compare/v0.20.0...HEAD
[0.20.0]: https://github.com/lsm/fiso/compare/v0.19.0...v0.20.0
[0.19.0]: https://github.com/lsm/fiso/compare/v0.18.0...v0.19.0
