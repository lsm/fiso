# Wasmer Integration Guide

This guide covers the Wasmer runtime integration in Fiso, which enables full-featured WASM applications with network access, database connectivity, and threading support.

## Overview

Fiso supports two WASM runtimes:

| Runtime | Type | Use Case | CGO Required |
|---------|------|----------|--------------|
| **wazero** | Pure Go | Simple transforms (JSON-in/JSON-out) | No |
| **wasmer** | CGO | Full WASIX apps with network, threading, DB | Yes |

Wasmer provides WASIX support, enabling:
- Network access (sockets, HTTP, DNS)
- Database connectivity via sockets
- Threading (pthreads)
- Full filesystem access
- Running Python (Django, FastAPI), PHP, JavaScript (Next.js)

## When to Use Wasmer

**Use wazero (default) when:**
- You need simple JSON transformations
- You want pure Go builds (no CGO)
- You need fast startup and low memory

**Use wasmer when:**
- Your WASM app needs network access
- You need threading support
- You're running full applications (Python, PHP, JS)
- You need database connectivity

## Deployment Modes

Fiso provides four deployment modes for Wasmer:

### 1. fiso-wasmer (Standalone)
Runs a single Wasmer app with HTTP server.

```bash
# Build
CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-wasmer ./cmd/fiso-wasmer

# Run
./tmp/fiso-wasmer -config apps.yaml
```

### 2. fiso-flow-wasmer (Flow + Wasmer)
Fiso-flow with Wasmer runtime support for WASM interceptors.

```bash
# Build
CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-flow-wasmer ./cmd/fiso-flow-wasmer

# Run
./tmp/fiso-flow-wasmer -config-dir /etc/fiso/flows
```

### 3. fiso-wasmer-link (Link + Wasmer)
Fiso-link with embedded Wasmer apps for interceptors.

```bash
# Build
CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-wasmer-link ./cmd/fiso-wasmer-link

# Run
./tmp/fiso-wasmer-link -config /etc/fiso/link/config.yaml
```

### 4. fiso-wasmer-aio (All-in-One)
Combined Flow + Link + Wasmer in a single binary.

```bash
# Build
CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-wasmer-aio ./cmd/fiso-wasmer-aio

# Run
./tmp/fiso-wasmer-aio -config /etc/fiso/aio/config.yaml
```

## Configuration

### Runtime Configuration

Specify the runtime in your flow configuration:

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
      runtime: wasmer    # Use wasmer instead of default wazero
      timeout: 30s
      memoryLimit: 268435456  # 256MB
      env:
        LOG_LEVEL: debug
      preopens:
        /data: /var/lib/fiso/data
sink:
  type: http
  config:
    url: http://backend:8080
```

### App Configuration

For long-running Wasmer apps:

```yaml
# apps.yaml
apps:
  - name: python-api
    module: /etc/fiso/wasm/api.wasm
    execution: longRunning
    port: 8090
    memoryMB: 512
    timeout: 60s
    healthCheck: /health
    healthCheckInterval: 10s
    env:
      DATABASE_URL: postgres://user:pass@db:5432/mydb
      REDIS_URL: redis://redis:6379
    preopens:
      /app/data: /var/lib/fiso/data
```

## Execution Modes

### perRequest (Default)
Creates a new WASM instance for each request. Best for:
- Simple transforms
- Short-lived operations
- Memory isolation between requests

```yaml
execution: perRequest
```

### longRunning
Runs a single WASM instance with an HTTP server. Best for:
- Applications with state
- Database connection pooling
- Reduced startup overhead

```yaml
execution: longRunning
port: 8090
healthCheck: /health
```

### pooled
Maintains a pool of pre-initialized instances. Best for:
- High throughput scenarios
- Balancing isolation and performance

```yaml
execution: pooled
poolSize: 10
```

## Health Checking

For long-running apps, enable health checking:

```yaml
apps:
  - name: my-app
    module: /etc/fiso/wasm/app.wasm
    execution: longRunning
    port: 8090
    healthCheck: /health        # HTTP endpoint to check
    healthCheckInterval: 10s    # Check every 10 seconds
```

The manager will:
1. Send GET requests to `http://127.0.0.1:8090/health`
2. Mark the app as healthy if status is 2xx
3. Mark as unhealthy on connection failure or non-2xx response

## Building WASM Modules

### Go (wasip1)

```bash
GOOS=wasip1 GOARCH=wasm go build -o app.wasm .
```

### TinyGo

```bash
tinygo build -target=wasi -o app.wasm .
```

### Python (py2wasm)

```bash
pip install py2wasm
py2wasm app.py -o app.wasm
```

### Rust

```bash
rustup target add wasm32-wasi
cargo build --target wasm32-wasi --release
```

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
# Build Wasmer Docker images
make docker-wasmer
make docker-flow-wasmer
make docker-wasmer-link
make docker-wasmer-aio
```

## E2E Tests

Run the Wasmer E2E tests:

```bash
# Run specific test
make e2e-wasmer-standalone
make e2e-flow-wasmer
make e2e-wasmer-link
make e2e-wasmer-aio

# Run all Wasmer E2E tests
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

### Health Check Failures

**Error:** App marked as unhealthy

**Solution:** Check:
1. App is listening on the configured port
2. Health check endpoint returns 2xx status
3. No firewall blocking localhost connections

```bash
# Test health endpoint manually
curl http://127.0.0.1:8090/health
```

### Timeout Errors

**Error:** `wasm execution timeout after 30s`

**Solution:** Increase timeout in configuration:
```yaml
timeout: 60s
```

Or optimize your WASM module for faster execution.

## Migration from Wazero

To migrate from wazero to wasmer:

1. Add `runtime: wasmer` to your WASM interceptor config
2. Rebuild with the wasmer build tag
3. Ensure CGO is available in your build environment

```yaml
# Before (wazero, default)
interceptors:
  - type: wasm
    config:
      module: /etc/fiso/modules/transform.wasm

# After (wasmer)
interceptors:
  - type: wasm
    config:
      module: /etc/fiso/modules/transform.wasm
      runtime: wasmer
```

## Limitations

1. **CGO Required:** Wasmer builds require a C compiler
2. **Cross-compilation:** More complex than pure Go
3. **Binary Size:** Larger than wazero builds
4. **Startup Time:** Slightly slower than wazero for transforms
