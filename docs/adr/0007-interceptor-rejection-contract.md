# ADR 0007: Interceptor rejection contract

- **Status:** Accepted
- **Decision date:** 2026-09-02

## Context

Fiso's interceptor chain (wasm guests in Flow pipelines and on Link targets)
could transform an event or fail — it could not **refuse** one. Every
interceptor error followed the failure path: in a Flow pipeline,
`INTERCEPTOR_FAILED` retries and dead-letters the event before the error
surfaces; the http source then answers every handler error with 500; the
gRPC source returns an unmapped stream error. In Fiso-Link an interceptor
error returns 500, and `failOpen` downgrades it to pass-through.

This blocks the primary authentication pattern for wasm interceptors
(issue #48): a module that verifies credentials must be able to deny the
request. Under the failure-only contract, a forged credential would be
retried, copied into the dead-letter topic (absorbing unauthenticated
traffic — and attacker-controlled payloads — into a usually less-guarded
queue), and answered with a generic 500 rather than 401.

## Decision

Interceptors gain a first-class **rejection** verdict, distinct from failure:

1. **ABI.** A wasm guest signals refusal by returning
   `{"reject": {"status": <400-599>, "reason": "..."}}` instead of
   `{payload, headers}`. The host surfaces it as the typed
   `interceptor.RejectedError{Status, Reason}`. A status outside 400–599 is
   a contract violation and follows the ordinary failure path — it is never
   silently rewritten. An absent `reject` field keeps the transformation
   contract unchanged (the ABI is additive; existing modules are unaffected).

2. **Rejection is terminal.** No retries, no dead-letter. The DLQ is for
   events that failed delivery, not traffic that was refused admission.
   Request-response sources answer the caller:
   - http (plain and pooled): `reason` as the body, `status` as the code —
     every other handler error remains 500.
   - gRPC: the status translates to the closest code (401 Unauthenticated,
     403 PermissionDenied, …; unmapped statuses fall back to
     PermissionDenied so a refusal never degrades to an internal error),
     with the reason preserved.
   - kafka: there is no caller to answer; the rejection is logged and the
     offset acknowledged so a refused message is not reprocessed forever.

3. **Fiso-Link.** An outbound or inbound interceptor rejection responds with
   the guest-chosen status and reason instead of 500.

4. **`failOpen` exempts rejections.** `failOpen` expresses "continue when the
   module *fails*". A rejection is a verdict, not a failure; forwarding what
   the module refused would invert the operator's intent. Rejections
   propagate even when `failOpen` is true.

5. **Reason hygiene.** The reason is caller-facing by design, so modules must
   not echo credentials or secrets into it.

## Scope and Non-Decisions

- gRPC sidecar interceptors keep their raw-bytes `{payload, headers}`
  contract for now; a rejection field for them is future work with its own
  executable evidence.
- No metrics facade beyond existing hooks; rejections are observable via the
  `event rejected by interceptor` log line today.
- Rejection does not interact with commit policies: it is decided before
  delivery, so `sink`/`sink_or_dlq`/`kafka_transaction` distinctions do not
  apply to refused events.
- This ADR does not decide authentication itself (key delivery, JWKS
  caching, claim schemas) — only the denial primitive it builds on.

## Consequences

- An authentication module becomes expressible as a plain wasm interceptor:
  verify, rewrite headers, or refuse with a status.
- Operators must expect refused traffic to be **absent** from the DLQ;
  rejection counts live in logs (and later metrics), not DLQ depth.
- Guests choosing statuses outside 400–599 fail loudly at development time.
- The interceptor ABI grows by one optional field; both engines deliver it
  through the same envelope.

## Alternatives Considered

- **Map refusals onto the existing failure path with a status-code
  convention** (e.g. wrapped errors): rejected and failed events remain
  indistinguishable to the DLQ/retry machinery, and every caller of
  `Process` re-derives the classification. Rejected as fragile.
- **Rejection at the source only** (an http middleware before ingestion):
  duplicates credential logic outside the interceptor chain and leaves kafka
  and Link without the primitive. Rejected as incoherent.
- **A separate `auth` interceptor type**: a new config surface and lifecycle
  for what is semantically "inspect, transform, or refuse" — the existing
  chain already composes. Rejected as premature; revisit if auth modules
  need host capabilities the ABI cannot express.

## References

- Issue #48 — Interceptors cannot reject
- ADR 0003 — supported integrations are executable
- ADR 0006 — wasm host HTTP capability (the companion building block for
  guest-side auth modules)
