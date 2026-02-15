# Kubernetes Deployment Guide for WASM Modules

This guide covers how to build, package, and deploy WebAssembly (WASM) modules for use with Fiso in Kubernetes environments.

## Overview

WASM modules in Fiso enable custom data transformations loaded at runtime without requiring custom container images. The Fiso runtime loads WASM modules from the filesystem and executes them as interceptors in the event processing pipeline.

**Key Benefits:**
- **No custom images needed**: The standard Fiso image loads WASM modules at runtime
- **Language flexibility**: Write modules in Go, Rust, TinyGo, C, or any WASI-compatible language
- **Hot-reload capable**: Update modules without rebuilding container images
- **Sandboxed execution**: WASM provides isolation and controlled resource access

**How It Works:**

1. Fiso-flow receives an event from the source (HTTP, Kafka, gRPC)
2. The event passes through the transform pipeline (CEL-based field transforms)
3. Before sending to the sink, the event is passed to the WASM module
4. The WASM module transforms the event and returns the result
5. Fiso-flow delivers the transformed event to the configured sink

## Building WASM Modules

### Go Example

Create a WASM module that processes events using the JSON-in/JSON-out ABI:

```go
// main.go
package main

import (
    "encoding/json"
    "io"
    "os"
)

// Input structure received from Fiso
type wasmInput struct {
    Payload   json.RawMessage   `json:"payload"`
    Headers   map[string]string `json:"headers"`
    Direction string            `json:"direction"`
}

// Output structure returned to Fiso
type wasmOutput struct {
    Payload interface{}       `json:"payload"`
    Headers map[string]string `json:"headers"`
}

func main() {
    // Read JSON input from stdin
    input, err := io.ReadAll(os.Stdin)
    if err != nil {
        os.Exit(1)
    }

    var req wasmInput
    if err := json.Unmarshal(input, &req); err != nil {
        os.Exit(1)
    }

    // Parse and transform the payload
    var data map[string]interface{}
    if err := json.Unmarshal(req.Payload, &data); err != nil {
        os.Exit(1)
    }

    // Your transformation logic here
    data["processed_by"] = "wasm-module"
    data["timestamp"] = "2024-01-15T10:00:00Z"

    // Optionally modify headers
    if req.Headers == nil {
        req.Headers = make(map[string]string)
    }
    req.Headers["X-WASM-Processed"] = "true"

    // Write JSON output to stdout
    output := wasmOutput{
        Payload: data,
        Headers: req.Headers,
    }

    if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
        os.Exit(1)
    }
}
```

Compile for WASI:

```bash
# Standard Go compiler (Go 1.21+)
GOOS=wasip1 GOARCH=wasm go build -o enrich.wasm main.go

# Resulting binary size: ~2-4 MB
```

### TinyGo for Smaller Modules

TinyGo produces significantly smaller WASM binaries:

```bash
# Install TinyGo
curl -sSL https://raw.githubusercontent.com/tinygo-org/tinygo/main/util/tinygo-installer.sh | bash

# Compile with TinyGo
tinygo build -target=wasi -o enrich.wasm main.go

# Resulting binary size: ~100-500 KB
```

**TinyGo limitations:**
- Limited standard library support
- Some reflection features unavailable
- Check [TinyGo compatibility](https://tinygo.org/docs/reference/lang-support/) for details

### Rust Example

```rust
// main.rs
use std::io::{self, Read, Write};

fn main() {
    // Read input from stdin
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();

    // Parse and transform JSON (using serde_json)
    let mut data: serde_json::Value = serde_json::from_str(&input).unwrap();

    // Your transformation logic
    if let Some(obj) = data.as_object_mut() {
        obj.insert("processed_by".to_string(), serde_json::json!("rust-wasm"));
    }

    // Write output to stdout
    let output = serde_json::to_string(&data).unwrap();
    io::stdout().write_all(output.as_bytes()).unwrap();
}
```

Compile for WASI:

```bash
# Add WASM target
rustup target add wasm32-wasi

# Build
cargo build --target wasm32-wasi --release -o enrich.wasm
```

### Local Testing with Wasmtime

Test your WASM module before deploying to Kubernetes:

```bash
# Install wasmtime
curl https://wasmtime.dev/install.sh -sSf | bash

# Test with sample input
echo '{"payload":{"order_id":"123","amount":99.99},"headers":{},"direction":"inbound"}' \
  | wasmtime enrich.wasm

# Expected output:
# {"payload":{"amount":99.99,"order_id":"123","processed_by":"wasm-module"},"headers":{"X-WASM-Processed":"true"}}
```

For more complex testing:

```bash
# Test with a file
cat test-event.json | wasmtime enrich.wasm > output.json

# Validate the output
jq . output.json
```

## Getting WASM into Kubernetes

WASM modules are typically stored in ConfigMaps and mounted as volumes in pods. Choose the method that best fits your workflow.

### Method A: kubectl create configmap (Quick Start)

The simplest approach for development and testing:

```bash
# Create ConfigMap from WASM file
kubectl create configmap wasm-enrich-module \
  --from-file=enrich.wasm=./enrich.wasm \
  -n production

# Verify
kubectl get configmap wasm-enrich-module -n production -o yaml
```

**Pros:**
- Simple, one-line command
- Good for development and testing

**Cons:**
- Not declarative (hard to version control)
- Base64 encoding happens automatically (less transparent)

### Method B: YAML Manifest with binaryData

For declarative management with explicit base64 encoding:

```bash
# Encode the WASM module
base64 -i enrich.wasm -o enrich.wasm.b64

# Or on macOS
base64 -i enrich.wasm -o enrich.wasm.b64
```

```yaml
# wasm-configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: wasm-enrich-module
  namespace: production
binaryData:
  enrich.wasm: AGFzbQAAAABv...  # Base64-encoded WASM binary
```

```bash
# Apply
kubectl apply -f wasm-configmap.yaml
```

**Pros:**
- Fully declarative
- Easy to version control
- Explicit about binary content

**Cons:**
- Manual base64 encoding step
- Large YAML files for bigger modules

### Method C: Kustomize configMapGenerator (Recommended)

Kustomize automatically handles base64 encoding and generates ConfigMaps from files:

```yaml
# kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: production

configMapGenerator:
  - name: wasm-enrich-module
    files:
      - enrich.wasm=modules/enrich.wasm
    options:
      annotations:
        description: "Order enrichment WASM module"

resources:
  - deployment.yaml
```

```bash
# Build and apply
kustomize build . | kubectl apply -f -

# Or with kubectl built-in kustomize
kubectl apply -k .
```

**Multi-module example:**

```yaml
# kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: production

configMapGenerator:
  - name: wasm-modules
    files:
      - enrich.wasm=modules/enrich.wasm
      - validate.wasm=modules/validate.wasm
      - transform.wasm=modules/transform.wasm
    options:
      labels:
        app.kubernetes.io/component: wasm-modules
```

**Pros:**
- No manual base64 encoding
- Declarative and version-controlled
- Automatic hash suffixes for rolling updates
- Good tooling integration (kubectl, ArgoCD, Flux)

### Method D: CI/CD Pipeline Example

Automate WASM module deployment with GitHub Actions:

```yaml
# .github/workflows/deploy-wasm.yaml
name: Deploy WASM Modules

on:
  push:
    branches: [main]
    paths:
      - 'wasm-modules/**'

jobs:
  build-and-deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Build WASM modules
        run: |
          for dir in wasm-modules/*/; do
            name=$(basename "$dir")
            cd "$dir"
            GOOS=wasip1 GOARCH=wasm go build -o "${name}.wasm" .
            cd -
          done

      - name: Test WASM modules
        run: |
          curl https://wasmtime.dev/install.sh -sSf | bash
          source ~/.bashrc
          for wasm in wasm-modules/*/*.wasm; do
            echo '{"payload":{},"headers":{},"direction":"inbound"}' \
              | wasmtime "$wasm" > /dev/null
          done

      - name: Set kubeconfig
        uses: azure/setup-kubectl@v3
        with:
          version: 'v1.28.0'

      - name: Deploy to Kubernetes
        run: |
          kubectl create configmap wasm-modules \
            --from-file=wasm-modules/ \
            -n production \
            --dry-run=client -o yaml | kubectl apply -f -

          # Restart pods to pick up new modules
          kubectl rollout restart deployment/fiso-flow -n production
```

### Size Considerations and Limits

**ConfigMap Size Limit:** Kubernetes ConfigMaps have a maximum size of **1 MiB**. For larger WASM modules, consider alternatives:

| Module Size | Recommended Approach |
|-------------|---------------------|
| < 500 KiB | ConfigMap (any method) |
| 500 KiB - 1 MiB | ConfigMap with compression |
| > 1 MiB | PersistentVolume or init container |

**Reducing WASM module size:**

```bash
# Use TinyGo instead of standard Go
tinygo build -target=wasi -o small.wasm main.go

# Strip debug symbols (standard Go)
GOOS=wasip1 GOARCH=wasm go build -ldflags="-s -w" -o small.wasm main.go

# Compress with gzip
gzip -9 -k enrich.wasm
# Result: enrich.wasm.gz (often 50-70% smaller)
```

**For modules exceeding 1 MiB, use an init container:**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fiso-flow
spec:
  template:
    spec:
      initContainers:
        - name: fetch-wasm
          image: busybox
          command:
            - sh
            - -c
            - |
              wget -O /modules/enrich.wasm https://artifacts.example.com/wasm/enrich.wasm
          volumeMounts:
            - name: wasm-modules
              mountPath: /modules
      containers:
        - name: fiso-flow
          # ... main container config
          volumeMounts:
            - name: wasm-modules
              mountPath: /etc/fiso/modules
              readOnly: true
      volumes:
        - name: wasm-modules
          emptyDir: {}
```

## Deploying Fiso with WASM

### Complete Deployment YAML

```yaml
# fiso-flow-deployment.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: fiso-flow-config
  namespace: production
data:
  wasm-flow.yaml: |
    name: order-processing
    source:
      type: kafka
      config:
        cluster: main
        topic: orders
        consumerGroup: fiso-order-processor
        startOffset: latest
    interceptors:
      - type: wasm
        config:
          module: /etc/fiso/modules/enrich.wasm
          timeout: "5s"
    sink:
      type: http
      config:
        url: http://order-service:8080/orders
        method: POST
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: wasm-modules
  namespace: production
binaryData:
  enrich.wasm: AGFzbQAAAABv...  # Base64-encoded WASM binary
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: kafka-config
  namespace: production
data:
  kafka.yaml: |
    clusters:
      main:
        brokers:
          - kafka-0.kafka-headless:9092
          - kafka-1.kafka-headless:9092
          - kafka-2.kafka-headless:9092
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fiso-flow
  namespace: production
  labels:
    app: fiso-flow
spec:
  replicas: 3
  selector:
    matchLabels:
      app: fiso-flow
  template:
    metadata:
      labels:
        app: fiso-flow
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
    spec:
      containers:
        - name: fiso-flow
          image: ghcr.io/lsm/fiso-flow:latest
          ports:
            - name: http
              containerPort: 8081
            - name: metrics
              containerPort: 9090
          env:
            - name: FISO_CONFIG_DIR
              value: /etc/fiso/flows
            - name: FISO_METRICS_ADDR
              value: ":9090"
          volumeMounts:
            - name: flow-config
              mountPath: /etc/fiso/flows
              readOnly: true
            - name: wasm-modules
              mountPath: /etc/fiso/modules
              readOnly: true
            - name: kafka-config
              mountPath: /etc/fiso/kafka
              readOnly: true
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 512Mi
          livenessProbe:
            httpGet:
              path: /healthz
              port: metrics
            initialDelaySeconds: 10
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /readyz
              port: metrics
            initialDelaySeconds: 5
            periodSeconds: 5
      volumes:
        - name: flow-config
          configMap:
            name: fiso-flow-config
        - name: wasm-modules
          configMap:
            name: wasm-modules
        - name: kafka-config
          configMap:
            name: kafka-config
---
apiVersion: v1
kind: Service
metadata:
  name: fiso-flow
  namespace: production
spec:
  selector:
    app: fiso-flow
  ports:
    - name: http
      port: 8081
      targetPort: 8081
    - name: metrics
      port: 9090
      targetPort: 9090
```

### Flow Configuration Referencing Mounted Path

The flow configuration references the mounted WASM module path:

```yaml
name: wasm-transform-flow
source:
  type: http
  config:
    listenAddr: ":8081"
    path: /ingest
interceptors:
  - type: wasm
    config:
      module: /etc/fiso/modules/enrich.wasm  # Path inside container
      timeout: "5s"  # Optional: execution timeout (default: 5s)
sink:
  type: http
  config:
    url: http://user-service:8082
    method: POST
```

### Multi-Module Example

Chain multiple WASM interceptors for complex transformations:

```yaml
# flow-config.yaml
name: multi-transform-flow
source:
  type: kafka
  config:
    cluster: main
    topic: events
    consumerGroup: fiso-event-processor
interceptors:
  # First: validate the event structure
  - type: wasm
    config:
      module: /etc/fiso/modules/validate.wasm
      timeout: "2s"
  # Second: enrich with external data
  - type: wasm
    config:
      module: /etc/fiso/modules/enrich.wasm
      timeout: "5s"
  # Third: transform to target format
  - type: wasm
    config:
      module: /etc/fiso/modules/transform.wasm
      timeout: "3s"
sink:
  type: http
  config:
    url: http://downstream-service:8080/events
    method: POST
```

**Corresponding ConfigMap:**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: wasm-modules
  namespace: production
binaryData:
  validate.wasm: AGFzbQAAAA...
  enrich.wasm: AGFzbQBBB...
  transform.wasm: AGFzbQCCC...
```

## Production Best Practices

### Use readOnly: true for Security

Always mount ConfigMaps as read-only to prevent accidental modification:

```yaml
volumeMounts:
  - name: wasm-modules
    mountPath: /etc/fiso/modules
    readOnly: true  # Security best practice
```

### Resource Limits for WASM Execution

WASM execution consumes CPU and memory. Set appropriate limits:

```yaml
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m      # Prevent runaway CPU usage
    memory: 512Mi  # WASM modules share container memory
```

**Considerations:**
- Each WASM execution uses memory from the container's allocation
- Complex modules or large payloads need more memory
- Set timeout to prevent long-running modules from blocking the pipeline

### Health Checks

Configure health probes to detect issues:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: metrics
  initialDelaySeconds: 10
  periodSeconds: 10
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /readyz
    port: metrics
  initialDelaySeconds: 5
  periodSeconds: 5
  failureThreshold: 2
```

### Rolling Updates with ConfigMap Changes

ConfigMap changes don't automatically trigger pod restarts. Implement one of these strategies:

**Option 1: Kustomize with hash suffix (recommended)**

```yaml
# kustomization.yaml
configMapGenerator:
  - name: wasm-modules
    files:
      - enrich.wasm=modules/enrich.wasm
    # Kustomize adds hash suffix automatically

resources:
  - deployment.yaml
```

The Deployment references the ConfigMap, and Kustomize updates the reference when the ConfigMap content changes, triggering a rolling update.

**Option 2: Manual rollout restart**

```bash
# After updating the ConfigMap
kubectl rollout restart deployment/fiso-flow -n production

# Monitor the rollout
kubectl rollout status deployment/fiso-flow -n production
```

**Option 3: Checksum annotation**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fiso-flow
spec:
  template:
    metadata:
      annotations:
        # Triggers rollout when ConfigMap changes
        config-hash: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
```

### Monitoring and Observability

**Prometheus metrics:**

```yaml
# ServiceMonitor for Prometheus Operator
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: fiso-flow
  namespace: production
spec:
  selector:
    matchLabels:
      app: fiso-flow
  endpoints:
    - port: metrics
      path: /metrics
      interval: 30s
```

**Key metrics to monitor:**

| Metric | Type | Description |
|--------|------|-------------|
| `fiso_flow_events_total` | Counter | Total events processed |
| `fiso_flow_event_duration_seconds` | Histogram | Event processing latency |
| `fiso_flow_transform_errors_total` | Counter | Transform (including WASM) failures |

**Logging:**

```yaml
# Enable structured JSON logging
env:
  - name: LOG_FORMAT
    value: json
```

## Troubleshooting

### Module Not Found Errors

**Symptom:**
```
ERROR wasm module not found path=/etc/fiso/modules/enrich.wasm
```

**Diagnosis:**

```bash
# Check if ConfigMap exists
kubectl get configmap wasm-modules -n production

# Check ConfigMap contents
kubectl describe configmap wasm-modules -n production

# Verify volume mount in pod
kubectl exec -n production deployment/fiso-flow -- ls -la /etc/fiso/modules/

# Check pod events
kubectl describe pod -n production -l app=fiso-flow
```

**Solutions:**
1. Ensure ConfigMap name matches the volume reference
2. Verify the file name in ConfigMap matches the flow configuration
3. Check that the pod has read permissions on the mounted volume

### Size Limit Issues

**Symptom:**
```
ERROR ConfigMap "wasm-modules" is invalid: []: Too long: must have at most 1048576 bytes
```

**Solutions:**

1. **Compress the module:**
   ```bash
   gzip -9 enrich.wasm
   # Decompress in init container
   ```

2. **Use TinyGo for smaller binaries:**
   ```bash
   tinygo build -target=wasi -o enrich.wasm main.go
   ```

3. **Split large modules across multiple ConfigMaps:**
   ```yaml
   volumes:
     - name: wasm-modules-a
       configMap:
         name: wasm-modules-a
     - name: wasm-modules-b
       configMap:
         name: wasm-modules-b
   ```

4. **Use init container for modules > 1 MiB:**
   ```yaml
   initContainers:
     - name: fetch-wasm
       image: curlimages/curl
       command: ['sh', '-c', 'curl -o /modules/enrich.wasm https://artifacts.example.com/wasm/enrich.wasm']
       volumeMounts:
         - name: wasm-modules
           mountPath: /modules
   ```

### Execution Errors

**Symptom:**
```
ERROR wasm module execution failed error="wasm: exit code 1"
```

**Diagnosis:**

1. **Test locally first:**
   ```bash
   echo '{"payload":{"test":"data"},"headers":{},"direction":"inbound"}' \
     | wasmtime enrich.wasm
   ```

2. **Check module input/output format:**
   - Input must be valid JSON with `payload`, `headers`, `direction` fields
   - Output must be valid JSON with `payload` and `headers` fields

3. **Check for WASI compatibility:**
   ```bash
   wasmtime --version
   wasmtime validate enrich.wasm
   ```

4. **Review container logs:**
   ```bash
   kubectl logs -n production deployment/fiso-flow -c fiso-flow --previous
   ```

**Common causes:**

| Error | Cause | Solution |
|-------|-------|----------|
| `exit code 1` | Module panic or error | Check module error handling |
| `timeout` | Execution took too long | Optimize module or increase timeout |
| `invalid JSON` | Malformed input/output | Validate JSON parsing in module |
| `out of memory` | Memory limit exceeded | Increase container memory limit |

### Debug Checklist

```bash
# 1. Verify ConfigMap exists and has correct content
kubectl get configmap wasm-modules -n production -o yaml

# 2. Check pod is running with correct volumes
kubectl describe pod -n production -l app=fiso-flow

# 3. Verify files are mounted correctly
kubectl exec -n production deployment/fiso-flow -- ls -la /etc/fiso/modules/

# 4. Test WASM module from inside the pod
kubectl exec -n production deployment/fiso-flow -- \
  sh -c 'echo "{\"payload\":{},\"headers\":{},\"direction\":\"inbound\"}" | wasmtime /etc/fiso/modules/enrich.wasm'

# 5. Check recent logs
kubectl logs -n production deployment/fiso-flow --tail=100

# 6. Verify flow configuration
kubectl get configmap fiso-flow-config -n production -o yaml
```
