# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What Fiso is

Go module `github.com/lsm/fiso` (Go 1.25, no CGO by default). Fiso is an application-boundary sidecar made of three runtime components plus a CLI:

- **Fiso-Flow** (`cmd/fiso-flow`) — inbound pipeline: HTTP / Kafka / gRPC source → interceptor chain (runs on the raw event, before any parsing) → transform (CEL-compiled `fields`) → CloudEvent envelope → HTTP / gRPC / Temporal / Kafka sink. Only Kafka-source flows get a real dead-letter topic; every builder gives HTTP and gRPC sources `dlq.NoopPublisher`, which discards the failed event and reports success.
- **Fiso-Link** (`cmd/fiso-link`) — outbound proxy the app calls at `localhost:3500/link/<target>`. Kafka targets branch to the Kafka handler before any credential lookup, so they have no auth-provider step. HTTP targets run circuit-breaker check → fetch provider credentials → outbound interceptors → retry loop, and the credential headers are injected inside each retry attempt after the interceptor has run, so interceptors never see injected credentials. Rate limiting is per target.
- **Fiso-Operator** (`cmd/fiso-operator`) — reconciles the `FlowDefinition` / `LinkTarget` CRDs (`api/v1alpha1`, manifests in `deploy/crds`) by validating the spec and setting status `Validated` or `Error` only (ADR 0004); it never deploys or actuates a runtime, so a `Validated` CR is not a running flow. A separate mutating webhook, gated by the `fiso.io/inject` annotation, injects only a `fiso-link` sidecar.
- **`fiso` CLI** (`cmd/fiso`, code in `internal/cli`) — `init`, `dev`, `validate`, `export`, `doctor`, `produce`, `consume`, `logs`, `transform`. `fiso export` converts local YAML to CRs and must fail closed on any lossy conversion (ADR 0002).

## Commands

Build (pure Go, output under `tmp/`):

```bash
make build-all          # fiso-flow, fiso-link, fiso-operator, fiso
make build              # fiso-flow only; build-link / build-operator / build-cli likewise
make build-wasmer-all   # the four Wasmer binaries; needs CGO + llvm, uses -tags wasmer
```

Test:

```bash
make test                                              # -race + coverage, excludes ./cmd/ and ./test/e2e/
go test -race ./internal/pipeline/... -run TestName     # one test
go test -count=1 ./cmd/fiso-flow/...                   # cmd/ is excluded from make test; today this is only the Temporal credential test
go test -tags wasmer -count=1 ./cmd/fiso-flow-wasmer/... ./cmd/fiso-wasmer-aio/... ./cmd/fiso-wasmer/... ./cmd/fiso-wasmer-link/... ./internal/wasmer/... ./internal/wasm/...
make test-integration                                  # -tags integration; needs Kafka at $KAFKA_BROKERS (default localhost:9092)
make coverage-check                                    # 95% locally (CI gates at 94.5%)
```

Only the Wasmer-tagged binaries have `buildPipeline` construction tests (`cmd/fiso-flow-wasmer/main_test.go`, `cmd/fiso-wasmer-aio/main_test.go`). `cmd/fiso-flow` has no test that calls its builder, so a supported type missing from the default builder still compiles and passes every unit job; only E2E catches it. Add a test alongside the change rather than assuming that command covers you.

Lint and hygiene (CI uses golangci-lint v2.8.0; there is no `.golangci.yml`):

```bash
make lint                          # two passes: default tags, then --build-tags=wasmer on cmd/, internal/wasm, internal/wasmer
make fmt-check mod-check vulncheck # = make checks
```

E2E (Docker Compose per scenario; operator scenario needs `kind`):

```bash
cd test/e2e/<scenario> && bash test.sh   # compose --build from the repo root by default
E2E_BUILD_FLAG=" " bash test.sh          # skip the compose build (what CI does); only works once the scenario's artifact exists
make e2e-operator                        # or e2e-wasmer-standalone, e2e-flow-wasmer, e2e-wasmer-link, e2e-wasmer-aio
```

With the build skipped, the standard scenarios expect `fiso-flow:e2e` / `fiso-link:e2e` images, the four Wasmer scenarios expect their own `fiso-flow-wasmer:e2e`, `fiso-wasmer-link:e2e`, `fiso-wasmer-aio:e2e`, or `fiso-wasmer:e2e` image, and the operator scenario expects `bin/fiso-operator`. `.github/workflows/ci.yml` shows how each is prepared.

Per CONTRIBUTING.md there is no single local command equivalent to the CI gate; `.github/workflows/ci.yml` is the complete gate. Run what is relevant to the change and report exactly what ran and what was skipped. Never describe `make checks` or `make e2e-all` as exhaustive.

## Architecture notes that span files

**Build tags.** Files needing wasmer-go carry `//go:build wasmer`; `internal/wasm/runtime_wasmer_stub.go` (`!wasmer`) supplies stubs that return errors so pure-Go binaries still compile. `internal/wasm/factory.go` selects wazero (default) or Wasmer per `Config.Type`. Because the default lint/test invocation cannot see wasmer-tagged files, both `make lint` and CI run a second tagged pass; do the same when touching that surface.

**Flow builders are duplicated on purpose.** `buildPipeline` in `cmd/fiso-flow/main.go`, `cmd/fiso-flow-wasmer/main.go`, and `cmd/fiso-wasmer-aio/main.go` each contain the full source/sink/interceptor type switch. ADR 0003 says an integration value is supported only when validation, the shipped runtime paths that share that validator, the applicable public schema, the docs, and a CI test all agree; changing one in isolation is a defect. The surfaces differ per value:

- Flow sink type: `internal/config.(*FlowDefinition).Validate`, all three builders, `ValidateFlowSpec` in `internal/operator/reconciler.go`, the `FlowDefinition` CRD sink enum in `deploy/crds/`, README, and the Wasmer `main_test.go` construction tests. All four sink surfaces currently agree on `http`, `grpc`, `temporal`, `kafka`.
- Flow source type: the same surfaces, but only `kafka` and `grpc` are CRD-representable. The HTTP source is deliberately local-only: the CRD source enum omits it and `validateExportableFlow` in `internal/cli/export.go` rejects it as unrepresentable, so a change to the HTTP source must not touch the CRD enum. Note `ValidateFlowSpec` still accepts `http` even though the CRD enum rejects it; reconciling that mismatch in either direction changes the public Kubernetes contract and needs its own ADR.
- Flow interceptor type: the validator, all three builders, and README. The v1alpha1 `FlowDefinition` has no interceptor field, and `internal/cli/export.go` deliberately rejects flows with interceptors as unrepresentable.
- Link protocol: `internal/link/config.go` validation, the Link proxy, and README; Link protocols never pass through the Flow validator or builders. Only `http` and `https` reach Kubernetes, so changing those also means `ValidateLinkSpec` and the `LinkTarget` CRD enum. `kafka` is local-only, like the HTTP Flow source: the operator validator and the CRD enum both omit it, and `validateExportableLink` rejects Kafka targets and named Kafka clusters as unrepresentable, so a Kafka-target change must not touch either.

**Interceptor contract.** `internal/interceptor.Interceptor` processes a `Request` (payload + headers + direction) and may return `*RejectedError` (ADR 0007). A rejection is terminal: no retry, no DLQ. The HTTP source answers with the exact status and reason; the gRPC source translates the status to the nearest gRPC code (for example 401 to `Unauthenticated`) and keeps the reason. Implementations live in `internal/interceptor/wasm` and `internal/interceptor/grpc`; Link has its own registry in `internal/link/interceptor`. Guest env vars arrive through `interceptors[].config.env` (ADR 0008) and must be delivered on both runtimes and in every Flow binary.

**WASM host HTTP.** Guests cannot open sockets; outbound HTTP from a WASM interceptor goes through a host function that routes via Fiso-Link (ADR 0006), and it is wazero-only (`factory.go` rejects `HostHTTP` on Wasmer). `internal/wasmer.Manager` runs "apps" as per-request functions behind an HTTP facade with a port pool; it does not give guests threads, sockets, or persistent state.

**Readiness and lifecycle.** `internal/flowruntime.Gate` treats every startup pipeline as a required runner; if one returns terminally (any error other than context cancellation, or an unexpected nil) readiness drops for the rest of the process lifetime while the process stays alive (ADR 0005). `internal/delivery` holds the Kafka commit policies (`sink`, `sink_or_dlq`, `kafka_transaction`) consumed by `internal/pipeline`.

**Config reload is parse-only.** `internal/config.Loader` watches the flow directory with fsnotify and re-parses, but a restart is required to apply changes. Do not document hot/live reload; see below.

**Documentation contract tests.** `test/contracts` runs under `make test` and greps the README plus exactly the guides listed under "Current Guides" in `docs/README.md` for forbidden claims: any hot/live/auto-reload wording (the README Flow section must instead state that a restart applies config changes), and any affirmative WASM capability claims about sockets, threads, WASIX, database connectivity, or "full applications". It also fails any `- type: wasm` interceptor example that sets `timeout:` (no builder applies it), and requires `docs/wasmer-integration.md` to keep the literal phrases `per-request`, `no network access`, `host-side`, and `--stdin-file`. A doc edit can fail `make test`; run it after editing README or a current guide.

## Documentation and process rules

`docs/README.md` defines document authority. Code, tests, and public config definitions are the truth about current behavior; README and `docs/*.md` current guides must match them. `docs/product-vision.md` and `docs/roadmap.md` are direction, never evidence that a capability exists.

- Write an ADR (`docs/adr/`, template in its README, next zero-padded number) before changing a public contract, configuration model, cross-component architecture, security/delivery/lifecycle semantics, or project governance (including the documentation-authority policy or the development methodology). Accepted ADRs are superseded by a new ADR, not rewritten.
- Record every merged user-visible change under `[Unreleased]` in `CHANGELOG.md` (Keep a Changelog format).
- Docs use present tense for implemented behavior, lowercase kebab-case filenames, repository-relative links, and the implementation names `Source`, `Sink`, `LinkTarget`.
- A PR states the linked issue or urgent rationale, one observable claim, the verification performed and its result, what was not tested, the rollback or disable path, the public docs and examples changed, and ADR changes when required (the full checklist is in CONTRIBUTING.md). Keep a slice to one claim; do not bundle follow-ups.
