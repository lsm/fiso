# ADR 0006: WASM HTTP Calls via a Host Function Through Fiso-Link

- **Status:** Accepted
- **Decision date:** 2026-09-01

## Context

WASM interceptors in Fiso are pure functions: the pipeline sends the JSON envelope, the module returns the transformed envelope. Guests have no network access — a deliberate property of both runtimes (wazero has no sockets; wasmer-go exposes none), and the reason the WASM capability contract (#34) forbids guest networking claims.

But pure functions cannot enrich. A fraud-scoring interceptor, a customer-lookup interceptor, a geo-ip interceptor all need one outbound HTTP call. The corrected debezium guide had to remove exactly such an example because it could never execute. So Fiso needs a way for a WASM module to make HTTP calls **without giving WASM networking**.

The server-WASM ecosystem standardized the answer (Extism, Spin both run it in production): the guest declares an import, the **host** implements it, and the host enforces an allowlist before performing any request. Capability-based security: deny by default.

## Decision

1. **A single host function, `fiso.http_call`.** The wazero runtime instantiates a `fiso` host module when (and only when) the interceptor opts in with `http: true`. The guest calls `http_call(req_ptr, req_len, resp_ptr, resp_cap) -> i32`: it passes a request JSON `{target, method, path, headers, body}` and a guest-allocated response buffer; the host writes `{status, headers, body}` into that buffer and returns the byte count (or a negative error code). Memory stays guest-owned; the host never allocates in guest space.

2. **Routing through Fiso-Link.** The host side performs the call against `http://<linkAddr>/link/<target><path>`. Link is Fiso's outbound mediation runtime — its auth injection, retry with backoff, circuit breaker, rate limiting, and metrics therefore apply to every guest call, and guest HTTP becomes observable policy-governed traffic instead of an invisible side channel.

3. **Deny-by-default, enforced twice.** The interceptor declares `httpTargets: [...]`; a call to any other target is rejected by the host **without a network call**. Link independently only serves its own configured targets. A module without `http: true` has no `fiso` import at all — instantiation of an importing module fails, so unused capability is absent, not merely unchecked.

4. **wazero first.** The wasmer engine's different input ABI (stdin-file, no host-module seam wired for interceptors) means the host function ships for wazero; wasmer support would follow the same contract if demand appears.

## Consequences

- Interceptors gain enrichment capability while guests remain network-less sandboxes.
- The security boundary is declarative and reviewable in flow config: reading `httpTargets` tells a reviewer exactly which external systems a module may reach.
- Response size is bounded by the guest-provided buffer; oversized responses return a distinct error code the module handles.
- This does not introduce raw sockets, DNS, or any other guest networking, and does not change the interceptor envelope ABI.

## Scope and Non-Decisions

This decision covers the host-function contract and its Link routing. It does **not**: give the wasmer engine the same import; add streaming, async, or multiple concurrent calls; change Link's own API; or make `http: true` a default anywhere.
