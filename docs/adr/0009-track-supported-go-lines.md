# ADR 0009: Track Supported Go Lines and Let Security Fixes Raise the Minimum

- **Status:** Accepted
- **Decision date:** 2026-10-14

## Context

`govulncheck` is a required CI gate. Standard-library advisories are fixed only
in current release lines: the Go team publishes fixes for the two most recent
major releases and does not backport them to an end-of-life line.

The module was pinned to Go 1.25.14, which carried nine advisories reachable
from shipped code — HTTP/2 crashes and resource exhaustion in `net/http` via the
gRPC source, the HTTP sink and Link's proxy, a MIME header memory-limit bypass
in `net/textproto`, an ECH parsing defect in `crypto/tls`, and two
`html/template` context-tracking defects. Every one is fixed in Go 1.26.9 and in
no 1.25 patch.

Clearing them therefore required raising the `go` directive across a minor
boundary, which gates downstream importers and anyone building from source, not
only CI. Earlier bumps (1.25.5, 1.25.8, 1.25.14) stayed inside one line and
raised no compatibility floor worth a decision, so no ADR covers the question of
how far the minimum may move or who decides.

## Decision

The module's `go` directive, the CI and release toolchain pins, the container
base images, and the `fiso init` scaffold templates all track a Go line that
upstream still supports.

A security fix available only in a newer line may raise the minimum Go version,
including across a minor boundary, as part of the ordinary security slice that
applies it. Such a bump does not need its own ADR. It must:

1. move the `go` directive, both workflow pins, every Dockerfile, and the
   scaffold templates together, so a newly scaffolded project does not build on
   the affected line;
2. state the new minimum in the README prerequisites; and
3. record the raised minimum in `CHANGELOG.md` under `Security`, naming the
   advisories it clears.

Raising the minimum for any other reason — a language feature, a dependency's
own floor, tidiness — is not covered here and needs its own decision.

## Scope and Non-Decisions

This decision governs how far and on what grounds Fiso's minimum Go version may
move, and what must move with it.

It does **not**:

- promise adoption of a new Go line on release day, or any upgrade cadence;
- commit to supporting more than one Go line at a time;
- change any configuration model, public API, or integration contract; or
- govern the Go directives of unit-test fixture modules under `testdata`, which
  are compiled by the CI toolchain and ship nothing.

## Consequences

- Importers and source builders on an older Go line must upgrade when a security
  bump lands. The changelog entry and README prerequisites are the notice.
- The `govulncheck` gate stays meaningful instead of accumulating suppressions
  for advisories that are genuinely reachable.
- A toolchain bump stays a normal security slice, so an urgent repair is not
  blocked waiting on a decision record.
- The repository keeps one Go line across the module, images, templates and
  fixtures, so a stale pin is a defect rather than a judgment call.

## Alternatives Considered

### Stay on the old line and suppress the advisories

Rejected: the traces are reachable from the gRPC source, the HTTP sink and
Link's proxy. Suppression would hide live defects, the same operational-truth
failure ADR 0003 rejects for unexecutable integration values.

### Require an ADR for every toolchain bump

Rejected: most bumps are patch-level and carry no compatibility consequence.
Per-bump ceremony would delay security repairs for no decision worth recording.

### Keep the floor at the oldest line users might have and backport

Rejected: upstream does not backport standard-library fixes to end-of-life
lines, so there is nothing to adopt.

## References

- [ADR 0003](0003-define-supported-integration-values-as-executable.md)
- [Documentation authority policy](../README.md#when-to-write-an-adr)
- [GO-2026-6617, GO-2026-6613, GO-2026-6612, GO-2026-6611, GO-2026-6610, GO-2026-6608, GO-2026-6607, GO-2026-6605, GO-2026-6603](https://pkg.go.dev/vuln/)
