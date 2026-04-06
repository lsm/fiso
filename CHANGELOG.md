# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project does not yet use semantic versioning — entries are grouped by
unreleased work and will be versioned when a release tag is cut.

---

## [Unreleased]

_No changes yet._

---

## [0.19.0] — 2026-04-03

### Added

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

[Unreleased]: https://github.com/lsm/fiso/compare/v0.19.0...HEAD
[0.19.0]: https://github.com/lsm/fiso/compare/v0.18.0...v0.19.0
