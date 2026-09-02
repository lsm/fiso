# ADR 0008: Interceptor environment configuration

- Status: accepted
- Date: 2026-09-02
- Deciders: maintainers
- References: [ADR 0006](0006-wasm-http-via-host-function.md) (host capability opt-in),
  [ADR 0007](0007-interceptor-rejection-contract.md) (rejection ABI)

## Context

WASM interceptor guests receive their event on stdin as a JSON envelope.
Anything a guest needs *besides the event* — most importantly the key
material an authentication module verifies against — had no delivery
channel: `internal/wasm.Config` had an `Env` field that only the wasmer
runtime applied, no Flow binary populated it, and `fiso-flow`'s wazero
constructors accepted no env at all. A guest could read `os.Getenv`, but
the host never set anything, so a JWT-verification module was impossible
to configure.

Separately, wazero's sandbox defaults a guest's wall clock to a frozen
fake value and its random source to a deterministic sequence. A
time-dependent guest (JWT `exp`/`nbf` checks) would silently accept
expired credentials against the frozen clock.

## Decision

1. **`interceptors[].config.env` is the delivery channel.** A map with
   string values, validated by Flow config and by every Flow-capable
   binary's builder; non-string values fail construction instead of being
   silently dropped (a dropped verification key would silently disable an
   authentication module's allow path). Null is treated as omitted,
   matching the existing `runtime` key.
2. **Env reaches the guest as environment variables at instantiation**, on
   both runtimes and in every Flow binary (`fiso-flow`, `fiso-flow-wasmer`,
   `fiso-wasmer-aio`). Values are passed verbatim; the host performs no
   interpolation or secret indirection.
3. **The wazero runtime configures real system facilities for guests**:
   wall clock, monotonic clock, nanosleep, and a crypto-grade random
   source. Guests are trusted code executing inside the process; the
   sandbox's determinism defaults are inappropriate for verification
   logic.
4. **Misconfiguration is an error, not a verdict.** A guest that cannot
   load its key material exits non-zero, which the host surfaces as an
   interceptor error (500-path, `failOpen` policy applies) — never as a
   rejection that would masquerade as an authentication decision. The
   wazero runtime embeds the failing guest's stderr diagnostic in the
   execution error so the operator can see which setting is wrong.
5. **Secret delivery is deploy-time rendering.** `env` values are plain
   configuration; operators template them from their secret store when
   rendering flow configs. No host-side secret references in this
   contract.
6. **Env entries must be WASI-representable.** Empty names, `=` or NUL in
   a key, and NUL in a value are rejected at validation and construction
   time — a KEY=VALUE environment cannot carry them, and failing at load
   time beats failing on every event.

## Consequences

- The supported auth guest (`examples/interceptors/auth`) configures its
  verification keys entirely through `env`; its contract tests pin the
  failure-mode split (bad key material → error; bad token → 401
  rejection).
- Env values are visible to the guest by design; only deploy trusted
  modules. The trust boundary is the module binary, same as ADR 0006's
  host-function capability.
- Link's interceptor configuration has no config map and therefore no env
  delivery; extending it is a separate decision if outbound credential
  enrichment is wanted.
- JWKS/remote key fetching remains out of scope; it is now *possible* via
  the ADR 0006 host function but is a latency/availability trade-off that
  deserves its own decision.
