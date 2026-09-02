# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project does not yet use semantic versioning — entries are grouped by
unreleased work and will be versioned when a release tag is cut.

---

## [Unreleased]

### Added

- **WASM interceptor authentication** — a supported guest module
  (`examples/interceptors/auth`) verifies the `Authorization: Bearer`
  token as a JWT (HS256/RS256/Ed25519, pure Go) and refuses
  unauthenticated traffic with 401 through the rejection contract,
  stripping the credential and any caller-supplied verdict headers and
  setting `X-Authenticated`/`X-Auth-Subject` from verified claims on the
  way through; the body passes byte-identically. Optional audience
  validation (`AUTH_EXPECTED_AUDIENCE`) refuses tokens minted for another
  service. Verification keys reach the guest through the new
  `interceptors[].config.env` map (validated — types and
  WASI-representable names — and delivered on both runtimes and in every
  Flow binary) — see the new "Authenticating Requests" README section and
  [ADR 0008](docs/adr/0008-interceptor-env-configuration.md).

- **Interceptor rejection contract** — a wasm interceptor can refuse an
  event instead of transforming it by returning
  `{"reject": {"status": 400-599, "reason": "..."}}`. A rejection is
  terminal: no retries and no dead-letter (the DLQ no longer absorbs
  unauthenticated traffic); http sources answer the caller with the
  module-chosen status and reason, gRPC sources with the closest status
  code, and Fiso-Link targets respond with it instead of a blanket 500. On
  kafka sources the refused message is logged and acknowledged so it is not
  reprocessed forever. Link's `failOpen` exempts rejections — a refusal is a
  verdict, not a failure. This is the primitive guest-side authentication
  modules build on. See
  [ADR 0007](docs/adr/0007-interceptor-rejection-contract.md).

### Fixed

- **Guest clock and randomness on wazero** — the runtime's sandbox
  defaults froze the guest wall clock (wazero's deterministic default),
  which would make a time-dependent guest such as a JWT verifier silently
  accept expired credentials. Guests now see the real system clock,
  nanosleep, and a crypto-grade random source; pinned by a runtime
  contract test. A failing guest's stderr diagnostic is also embedded in
  the execution error instead of being discarded, so misconfiguration is
  diagnosable.

## [0.21.0] — 2026-09-02

### Added

- **HTTP-calling WASM interceptors** — a WASM module can now make HTTP calls
  through the `fiso.http_call` host function, routed via Fiso-Link so guest
  calls inherit Link's auth, retries, circuit breaker, rate limiting, and
  metrics. Opt-in per interceptor (`http: true`) with a deny-by-default
  `httpTargets` allowlist: a call to any other target is rejected without a
  network request, and a module importing the function without opt-in fails
  to instantiate. wazero runtime only. See
  [ADR 0006](docs/adr/0006-wasm-http-via-host-function.md).

- **gRPC interceptors are now executable** — the `grpc` interceptor type was
  accepted by validation but constructed by no binary; all Flow-capable
  binaries now wire it. The raw response bytes are copied out of gRPC's
  receive buffer (pooled memory could otherwise race across concurrent
  interceptor calls), and headers returned by any interceptor — wasm or
  gRPC — now reach the sink; previously the pipeline kept only the payload
  and silently discarded the documented `headers` return. The sidecar contract is a raw-unary gRPC call to
  `/fiso.v1.InterceptorService/Process` carrying the interceptor JSON
  envelope (`{payload, headers, direction}` in, `{payload, headers}` out) as
  raw bytes, mirroring the Flow gRPC sink's codec convention. `address` is
  required; `timeout` defaults to 5s and must be a positive duration
  (ADR 0003).

### Changed

- **WASM verification hygiene** — wasmer-tagged code is now inside every
  quality gate: CI runs the `internal/wasmer` and `internal/wasm` unit tests
  (the only tests that execute modules through wasmer-go) and the two
  remaining wasmer binaries' tests, lint runs a second pass with
  `--build-tags=wasmer`, and `govulncheck` scans the tagged surface
  (wazero also upgraded to v1.12.0). The blind spot was hiding real
  breakage: the shared WASM test fixture only read stdin and failed under
  the wasmer engine's `--stdin-file` input mechanism — it now supports
  both — and 18 lint findings in the tagged code were fixed.

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

- **Dead Wasmer code** — the unimported `internal/wasmer/unified` package,
  the never-called `Proxy` type, and the unreferenced
  `NewManagerWithLogger`/`SetLogger`/`IsHealthy` manager methods.

- **Advertised-but-unimplemented `wasmer-app` interceptor type** — accepted by
  Flow validation but implemented by no binary. It is now rejected with an
  actionable error until a binary executes it (ADR 0003).

### Fixed

- **wasmer-link E2E now exercises interception** — the end-to-end test
  claimed "proxy with WASM interception" but configured no interceptor on
  the link target and asserted none of the module's effects (its only
  interception-adjacent check grepped for a request header no component
  sets). The `api` target now runs the intercept module on the wasmer
  engine, and the test fails unless the interceptor's header, payload
  marker, and env reach the backend and the transformed payload still
  processes. CI also builds the four wasmer CGO images once per run
  instead of four times.

- **Wasmer manager lifecycle defects** — `StopAll` now terminates every
  app's health-check goroutine (previously they leaked and kept probing the
  shut-down server for the process lifetime in every long-running wasmer
  binary); `StopApp` is safe to retry after a failed stop (previously a
  second call panicked on a double channel close); and an explicitly
  configured `port` is now reserved in the port pool so it is never handed
  to another app and its release on stop is symmetric. The manager's
  `defaultPortRange` setting is now actually honored by `fiso-wasmer`
  (previously the pool was hardcoded to 9000-9999 and the setting was dead).

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

[Unreleased]: https://github.com/lsm/fiso/compare/v0.21.0...HEAD
[0.21.0]: https://github.com/lsm/fiso/compare/v0.20.0...v0.21.0
[0.20.0]: https://github.com/lsm/fiso/compare/v0.19.0...v0.20.0
[0.19.0]: https://github.com/lsm/fiso/compare/v0.18.0...v0.19.0
