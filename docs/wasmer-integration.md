# Wasmer Integration Guide

This guide covers the Wasmer runtime integration in Fiso: an alternative
engine for invoking WASM modules per request.

## The executable contract

**WASM modules in Fiso are per-request functions.** Whether the engine is
wazero or wasmer, a module receives one request's payload and returns the
transformed payload. Specifically:

- **No network access.** WASM guests cannot open sockets, make DNS lookups,
  or call HTTP APIs from inside the module. There is no guest networking of
  any kind.
- **No threads.** Guests run single-threaded; there is no pthread support.
- **No in-memory persistent state.** Every invocation runs a fresh instance,
  so module globals, counters, and connection-like state reset on each
  request. Files written through a configured `preopens` directory do
  persist across invocations — only memory resets.
- **HTTP serving is host-side, not in-guest.** A "Wasmer app" is a Go HTTP
  server in the Fiso process that invokes the module per request and
  translates between HTTP and the module's input/output JSON ABI (see
  Building WASM Modules for the per-engine input mechanism). The module
  never accepts a connection itself.

This is the same shape the standardized `wasi:http` interface later
formalized: host-mediated request/response, not a resident in-guest server.

## Overview

Fiso supports two WASM runtimes:

| Runtime | Type | Use Case | CGO Required |
|---------|------|----------|--------------|
| **wazero** | Pure Go | WASM interceptors (JSON-in/JSON-out) — the default | No |
| **wasmer** | CGO | Same per-request model via the wasmer engine | Yes |

**Use wazero unless you have a specific reason not to.** It is the default,
needs no C toolchain, and covers the same executable contract. The wasmer
engine exists for cases where a module behaves better under wasmer's
compiler.

## Deployment Modes

Fiso provides four binaries built with wasmer support:

### 1. fiso-wasmer (Standalone)
Runs a single Wasmer app behind a host-side HTTP server.

```bash
# Build
CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-wasmer ./cmd/fiso-wasmer

# Run
./tmp/fiso-wasmer -config apps.yaml
```

### 2. fiso-flow-wasmer (Flow + Wasmer)
Fiso-flow that can also select the wasmer engine for `wasm` interceptors
via `runtime: wasmer`.

```bash
CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-flow-wasmer ./cmd/fiso-flow-wasmer
FISO_CONFIG_DIR=/etc/fiso/flows ./tmp/fiso-flow-wasmer
```

Note: the plain `fiso-flow` binary has no wasmer support; a flow configured
with `runtime: wasmer` fails to build there with an explicit error instead
of silently running under wazero.

### 3. fiso-wasmer-link (Link + Wasmer)
Fiso-link with embedded Wasmer apps.

```bash
CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-wasmer-link ./cmd/fiso-wasmer-link
FISO_WASMER_CONFIG=/etc/fiso/wasmer/apps.yaml ./tmp/fiso-wasmer-link -config /etc/fiso/link/config.yaml
```

### 4. fiso-wasmer-aio (All-in-One)
Flow + Link + Wasmer apps in a single binary.

```bash
CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-wasmer-aio ./cmd/fiso-wasmer-aio
./tmp/fiso-wasmer-aio -config /etc/fiso/aio/config.yaml
```

## Configuration

### Runtime Configuration

Select the engine for a `wasm` interceptor (wasmer-tagged binaries only):

```yaml
name: transform-flow
source:
  type: http
  config:
    listenAddr: ":8081"
interceptors:
  - type: wasm
    config:
      module: /etc/fiso/modules/transform.wasm
      runtime: wasmer    # optional; wazero is the default
sink:
  type: http
  config:
    url: http://backend:8080
```

Only `module` and `runtime` are honored for interceptors today. Other keys
present in older documentation (`timeout`, `memoryLimit`, `env`, `preopens`)
are not applied by the Flow binaries and should not be relied on.

### App Configuration

A Wasmer app exposes a module over HTTP through the host-side server:

```yaml
# apps.yaml
apps:
  - name: processor
    module: /etc/fiso/wasm/processor.wasm
    port: 8090
    healthCheck: /health
    healthCheckInterval: 10s
```

**App module ABI.** An app module does not receive the raw request payload.
For each HTTP request the host serializes an envelope and the module returns
one:

```json
// input the module receives (via the wasmer input mechanism above)
{ "method": "POST", "path": "/process", "query": "",
  "headers": {"content-type": "application/json"}, "body": { ... } }

// output the module returns (stdout)
{ "status": 200, "headers": {"x-app": "processor"}, "body": { ... } }
```

`bodyText` may substitute for `body` when the response is not JSON. This is
the ABI used by `fiso-wasmer`, `fiso-wasmer-link`, and `fiso-wasmer-aio` app
mode — distinct from the interceptor envelope documented under Building
WASM Modules.

Fields with **no current runtime effect** (accepted for compatibility,
documented here so nobody relies on them): `execution` and `memoryMB`.
There is one execution behavior — per-request invocation — regardless of the
`execution` value.

## Health Checking

For apps, enable health checking:

```yaml
apps:
  - name: my-app
    module: /etc/fiso/wasm/app.wasm
    port: 8090
    healthCheck: /health        # HTTP endpoint to check
    healthCheckInterval: 10s    # Check every 10 seconds
```

The manager sends GET requests to `http://127.0.0.1:8090/health` and marks
the app healthy on 2xx responses, unhealthy otherwise. Note that an app is
considered healthy immediately at startup, before the first check completes.

## Building WASM Modules

**The interceptor ABI.** A `wasm` interceptor does not receive the bare
event payload: the pipeline sends a JSON envelope and expects one back.

```json
// input the module receives
{ "payload": { ...event data... }, "headers": { ... },
  "direction": "request" }

// output the module returns
{ "payload": { ...transformed data... }, "headers": { ... } }
```

**The two engines deliver that envelope differently** — a module written
for one will not read input under the other:

- **wazero**: the envelope JSON is piped to the module's **stdin**; the
  response envelope is read from **stdout**.
- **wasmer**: the envelope is written to a temporary file mapped into the
  guest, and the module receives `--stdin-file <path>` as a **command-line
  argument** — it must parse that argument and read the file. Output is
  still stdout. A stdin-reading module run under wasmer receives no input
  and will fail or produce empty output.

### Go (wasip1)

```bash
GOOS=wasip1 GOARCH=wasm go build -o app.wasm .
```

### TinyGo

```bash
tinygo build -target=wasi -o app.wasm .
```

### Rust

```bash
rustup target add wasm32-wasi
cargo build --target wasm32-wasi --release
```

Because guests have no network access and no in-memory persistent state,
modules that need to call external services should do so through Fiso's own
outbound path (Fiso-Link) rather than inside the module.

## Building Fiso with Wasmer

Wasmer builds require CGO and a C compiler:

```bash
# Install dependencies (Ubuntu/Debian)
sudo apt-get install build-essential llvm-dev libclang-dev clang

# Build with Wasmer support
CGO_ENABLED=1 go build -tags wasmer -o fiso-wasmer ./cmd/fiso-wasmer

# Build all Wasmer binaries
make build-wasmer-all
```

### Docker Builds

```bash
make docker-wasmer
make docker-flow-wasmer
make docker-wasmer-link
make docker-wasmer-aio
```

## E2E Tests

```bash
make e2e-wasmer-standalone
make e2e-flow-wasmer
make e2e-wasmer-link
make e2e-wasmer-aio
make e2e-all
```

## Troubleshooting

### CGO Errors

**Error:** `CGO_ENABLED=0 go build -tags wasmer` fails

**Solution:** Wasmer requires CGO. Always use `CGO_ENABLED=1`:
```bash
CGO_ENABLED=1 go build -tags wasmer ./cmd/fiso-wasmer
```

### Missing LLVM

**Error:** `llvm-config not found`

**Solution:** Install LLVM development packages:
```bash
# Ubuntu/Debian
sudo apt-get install llvm-dev libclang-dev

# macOS
brew install llvm
```

### Module Not Found

**Error:** `module path not accessible: /path/to/app.wasm`

**Solution:** Verify the path exists and is readable:
```bash
ls -la /path/to/app.wasm
file /path/to/app.wasm  # Should show "WebAssembly binary"
```

## Limitations

1. **CGO Required:** Wasmer builds require a C compiler and produce larger
   binaries.
2. **Per-request model:** every invocation runs a fresh instance; in-memory
   state does not carry between requests (files under a preopen do).
3. **No guest networking or threading:** modules cannot open sockets, spawn
   threads, or keep resident processes.
4. **Host-side serving:** app HTTP endpoints are served by the Fiso process,
   not by the module.
5. **Experimental:** the wasmer binaries are not part of GitHub releases;
   build them from source with the instructions above.
