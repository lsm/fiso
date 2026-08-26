# ADR 0002: Make Export Fail Closed on Lossy Conversion

- **Status:** Accepted
- **Decision date:** 2026-08-26

## Context

`fiso export` converts local Flow and Link YAML into the checked-in `fiso.io/v1alpha1` Kubernetes resources. The local models contain fields and value shapes that the current API types and CRD schemas cannot represent. Previous conversion silently omitted some of those fields, coerced structured values to strings, treated local file or environment references as Kubernetes Secret names, and emitted resource values outside the checked-in CRD enums.

A successful command therefore appeared to preserve a configuration while changing or discarding its meaning. Because the command's output is intended for `kubectl apply`, silent conversion is a data-loss and operational-truth defect.

## Decision

`fiso export` is a lossless, fail-closed projection into the checked-in served CRD version.

The command must:

1. validate the local source configuration;
2. convert only populated values that have a structurally equivalent representation in both the current Go API and checked-in CRD schema;
3. preserve those values without type coercion or semantic reinterpretation;
4. reject unsupported values with a resource- and field-path-specific error; and
5. complete validation and conversion of every resource before writing any manifest bytes.

Successful output promises structural representability in `fiso.io/v1alpha1`. It does not promise that the current operator activates a runtime for the resource.

## Scope and Non-Decisions

This decision governs the current `fiso export` compatibility and failure contract.

It does **not**:

- expand or version the Go API or CRDs;
- make the local and Kubernetes configuration models canonical or fully equivalent;
- define migration for unsupported local fields;
- add Kubernetes Secret creation or reinterpret local authentication references;
- add operator runtime activation or executable-equivalence guarantees; or
- decide the future Application Contract, Interaction, or Environment Binding representation.

Those changes require separately qualified work and, where they alter public contracts or cross-cutting architecture, another ADR.

## Consequences

- Export can no longer silently discard, flatten, or reinterpret populated configuration.
- A later unsupported resource prevents earlier manifests from being written, so normal conversion failures are all-or-nothing.
- Existing callers that relied on lossy output now receive an actionable error and must remove the unsupported setting or wait for explicit API support.
- Local development scaffolds can be valid for local runtimes without being exportable to the narrower CRD model.
- Maintaining export requires explicit tests whenever either local configuration or the served CRD surface changes.
- Conversion writes the completed manifest stream through one `io.Writer` call, minimizing partial output. A writer can still report a failure after accepting a prefix, so this decision does not claim transactional external I/O.

## Alternatives Considered

### Continue best-effort conversion

Rejected because successful output would continue to conceal data loss and invalid CRD values.

### Serialize unsupported structures into strings or annotations

Rejected because syntax preservation is not semantic preservation when no API or runtime consumes the encoded value.

### Expand the API and CRDs in this repair

Rejected because API design, compatibility, operator behavior, and migration exceed this bounded correctness slice.

### Export the representable fields and warn about omissions

Rejected because warnings are easy to miss in automation and still produce manifests whose meaning differs from the source configuration.

## References

- [Issue #21](https://github.com/lsm/fiso/issues/21)
- [Project README](../../README.md#export-local-config-to-crds)
- [ADR 0001](0001-adopt-documentation-authority-and-evidence-driven-planning.md)
- [FlowDefinition CRD](../../deploy/crds/fiso.io_flowdefinitions.yaml)
- [LinkTarget CRD](../../deploy/crds/fiso.io_linktargets.yaml)
