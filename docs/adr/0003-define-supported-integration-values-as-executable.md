# ADR 0003: Define Supported Integration Values as Executable

- **Status:** Accepted
- **Decision date:** 2026-08-27

## Context

Flow and Link validation advertised integration values their shipped runtime paths could not execute.

`FlowDefinition` validation accepted `sink.type: grpc`, and `internal/sink/grpc` contains a raw-unary implementation, but every Flow-capable binary (`fiso-flow`, `fiso-flow-wasmer`, `fiso-wasmer-aio`) returned `unsupported sink type: grpc` when building the pipeline. A configuration that validated could never start.

Link validation, the operator, and the LinkTarget CRD accepted `protocol: grpc`, but Link has no gRPC transport: the shared proxy treats non-Kafka protocols as HTTP URL schemes, so a `grpc` target could not execute as a gRPC request. The advertised value had no runtime meaning at all.

In both cases the public contract promised more than the shipped binaries deliver, which is the same class of operational-truth defect as a silent data-loss bug: users discover the gap only at runtime.

## Decision

An integration value (source type, sink type, Link protocol, interceptor type) is **supported** only when all of the following hold:

1. validation accepts it;
2. the shipped runtime path for the binaries sharing that validator can construct and execute it;
3. authoritative documentation describes its actual executable contract; and
4. executable evidence (a construction or end-to-end test that runs in CI) pins that contract.

Applying this rule now:

- The existing Flow gRPC sink is wired into all Flow-capable builders. Its executable contract is a raw-codec unary invoke of `/fiso.v1.EventService/Deliver` with non-reserved event headers as gRPC metadata (gRPC reserves `Content-Type` for its transport; the CloudEvents envelope carries `datacontenttype` in the payload); `config.address` is required, `config.timeout` is a positive duration defaulting to 30s when absent, and `config.tls` is rejected until the sink supports credentials. Flow validation rejects grpc sinks without a usable `address` or with a timeout that is not a positive duration.
- Link `protocol: grpc` is removed from local validation, operator validation, and the LinkTarget CRD enum. Reintroducing it requires a Link gRPC routing contract (method, metadata mapping, TLS, load-balancing semantics) with its own ADR and conformance evidence — not merely re-adding the enum value.

## Scope and Non-Decisions

This decision governs which integration values Fiso may advertise as supported.

It does **not**:

- design or implement a Link gRPC transport;
- change the checked-in protobuf/raw-codec contract of the Flow gRPC sink;
- refactor the duplicated Flow builders into a general integration factory;
- commit to gRPC streaming delivery; or
- decide the canonical configuration model.

## Consequences

- Validation, builders, CRDs, docs, and tests must move together when an integration value is added or removed; adding a value to a validator alone now counts as a defect.
- Existing Link configurations using `protocol: grpc` were never executable; they now fail validation with an explicit error instead of appearing valid. Revert this change to restore the previous (unexecutable) advertised contract.
- `fiso export` inherits the rejection through Link validation, so export cannot emit LinkTarget manifests the CRD would reject.
- The Flow gRPC sink's raw-unary convention is now a documented public contract; changing it (for example to protobuf messages or streaming) is a compatibility decision.

## Alternatives Considered

### Reject Flow `sink.type: grpc` as well

Rejected: an implementation exists and is executable; removing it would narrow working capability when wiring it is a bounded change.

### Implement a minimal Link gRPC transport now

Rejected: inventing routing semantics without a qualified contract would replace an honest rejection with an unverified promise — the same defect in a different form.

### Leave validation permissive and document the gap

Rejected: validation is the contract users automate against; a value that validates but cannot run is a false positive regardless of documentation.

## References

- [Issue #22](https://github.com/lsm/fiso/issues/22)
- [ADR 0002](0002-make-export-fail-closed-on-lossy-conversion.md)
- [gRPC sink](../../internal/sink/grpc/grpc.go)
- [LinkTarget CRD](../../deploy/crds/fiso.io_linktargets.yaml)
