#!/bin/sh
set -e

echo "=== Operator E2E Test ==="
echo "Tests: CRD install → operator deploy → FlowDefinition & LinkTarget reconciliation"
echo ""

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"
CLUSTER_NAME="${KIND_CLUSTER_NAME:-fiso-e2e}"
NAMESPACE="fiso-system"
OPERATOR_IMAGE="fiso-operator:e2e"
KEEP_CLUSTER="${KEEP_CLUSTER:-false}"

# Colors (safe for CI — only used if terminal supports them)
if [ -t 1 ]; then
    GREEN='\033[0;32m'
    RED='\033[0;31m'
    YELLOW='\033[1;33m'
    NC='\033[0m'
else
    GREEN='' RED='' YELLOW='' NC=''
fi

pass() { echo "${GREEN}PASS${NC}: $1"; }
fail() { echo "${RED}FAIL${NC}: $1"; FAILURES=$((FAILURES + 1)); }
info() { echo "${YELLOW}INFO${NC}: $1"; }

FAILURES=0
TESTS=0

cleanup() {
    if [ "$KEEP_CLUSTER" = "true" ]; then
        info "KEEP_CLUSTER=true — skipping cluster deletion"
        return
    fi
    info "Cleaning up kind cluster '$CLUSTER_NAME'..."
    kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true
}

# -------------------------------------------------------------------
# 1. Create kind cluster
# -------------------------------------------------------------------
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    info "Kind cluster '$CLUSTER_NAME' already exists — reusing"
else
    info "Creating kind cluster '$CLUSTER_NAME'..."
    kind create cluster --name "$CLUSTER_NAME" --wait 60s
fi

trap cleanup EXIT

# -------------------------------------------------------------------
# 2. Build and load operator image
# -------------------------------------------------------------------
BUILD_FLAG="${E2E_BUILD_FLAG:-build}"
if [ "$BUILD_FLAG" = " " ]; then
    # Pre-built binary mode (CI): build a minimal image from the binary
    info "Building operator image from pre-compiled binary..."
    chmod +x "$ROOT_DIR/bin/fiso-operator"
    printf 'FROM alpine:3.20\nRUN apk add --no-cache ca-certificates tzdata\nCOPY fiso-operator /usr/local/bin/fiso-operator\nENTRYPOINT ["fiso-operator"]\n' \
        | docker build -t "$OPERATOR_IMAGE" -f - "$ROOT_DIR/bin/"
else
    info "Building operator image from source..."
    docker build -t "$OPERATOR_IMAGE" -f "$ROOT_DIR/Dockerfile.operator" "$ROOT_DIR"
fi

info "Loading operator image into kind cluster..."
kind load docker-image "$OPERATOR_IMAGE" --name "$CLUSTER_NAME"

# -------------------------------------------------------------------
# 3. Install CRDs
# -------------------------------------------------------------------
info "Installing CRDs..."
kubectl apply -f "$ROOT_DIR/deploy/crds/" --context "kind-${CLUSTER_NAME}"

# Wait for CRDs to be established
for crd in flowdefinitions.fiso.io linktargets.fiso.io; do
    kubectl wait --for=condition=Established "crd/$crd" --timeout=30s --context "kind-${CLUSTER_NAME}"
done
pass "CRDs installed and established"
TESTS=$((TESTS + 1))

# -------------------------------------------------------------------
# 4. Create namespace and install RBAC
# -------------------------------------------------------------------
info "Creating namespace and RBAC..."
kubectl create namespace "$NAMESPACE" --context "kind-${CLUSTER_NAME}" --dry-run=client -o yaml \
    | kubectl apply -f - --context "kind-${CLUSTER_NAME}"
kubectl apply -f "$ROOT_DIR/deploy/rbac/" --context "kind-${CLUSTER_NAME}"

# -------------------------------------------------------------------
# 5. Generate self-signed TLS certs for webhook server
# -------------------------------------------------------------------
info "Generating self-signed TLS certificate for webhook..."
CERT_DIR=$(mktemp -d)
openssl req -x509 -newkey rsa:2048 -keyout "$CERT_DIR/tls.key" -out "$CERT_DIR/tls.crt" \
    -days 1 -nodes -subj "/CN=fiso-operator.${NAMESPACE}.svc" \
    -addext "subjectAltName=DNS:fiso-operator.${NAMESPACE}.svc,DNS:fiso-operator.${NAMESPACE}.svc.cluster.local" \
    2>/dev/null

kubectl create secret tls fiso-operator-tls \
    --cert="$CERT_DIR/tls.crt" --key="$CERT_DIR/tls.key" \
    -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" --dry-run=client -o yaml \
    | kubectl apply -f - --context "kind-${CLUSTER_NAME}"
rm -rf "$CERT_DIR"

# -------------------------------------------------------------------
# 6. Deploy operator
# -------------------------------------------------------------------
info "Deploying operator..."

# Patch the deployment to use our e2e image and disable leader election for single-replica test
kubectl apply -f - --context "kind-${CLUSTER_NAME}" <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fiso-operator
  namespace: $NAMESPACE
  labels:
    app.kubernetes.io/name: fiso-operator
    app.kubernetes.io/part-of: fiso
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: fiso-operator
  template:
    metadata:
      labels:
        app.kubernetes.io/name: fiso-operator
    spec:
      serviceAccountName: fiso-operator
      containers:
        - name: operator
          image: $OPERATOR_IMAGE
          imagePullPolicy: Never
          env:
            - name: FISO_OPERATOR_MODE
              value: "controller"
            - name: FISO_ENABLE_LEADER_ELECTION
              value: "false"
            - name: FISO_LINK_IMAGE
              value: "fiso-link:latest"
          ports:
            - name: metrics
              containerPort: 8080
            - name: health
              containerPort: 9090
            - name: webhook
              containerPort: 9443
          livenessProbe:
            httpGet:
              path: /healthz
              port: health
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /readyz
              port: health
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
          volumeMounts:
            - name: tls-certs
              mountPath: /tmp/k8s-webhook-server/serving-certs
              readOnly: true
      volumes:
        - name: tls-certs
          secret:
            secretName: fiso-operator-tls
      terminationGracePeriodSeconds: 10
EOF

info "Waiting for operator to be ready..."
kubectl rollout status deployment/fiso-operator -n "$NAMESPACE" --timeout=90s --context "kind-${CLUSTER_NAME}"
pass "Operator deployed and ready"
TESTS=$((TESTS + 1))

# -------------------------------------------------------------------
# Helper: wait for CR status phase
# -------------------------------------------------------------------
wait_for_phase() {
    local resource="$1"
    local name="$2"
    local expected_phase="$3"
    local timeout="${4:-30}"
    local elapsed=0

    while [ $elapsed -lt $timeout ]; do
        phase=$(kubectl get "$resource" "$name" -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" \
            -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
        if [ "$phase" = "$expected_phase" ]; then
            return 0
        fi
        sleep 2
        elapsed=$((elapsed + 2))
    done
    return 1
}

# -------------------------------------------------------------------
# 7. Test: Valid FlowDefinition → Ready
# -------------------------------------------------------------------
TESTS=$((TESTS + 1))
info "Test: Valid FlowDefinition (kafka→http) should become Ready..."
kubectl apply -f "$SCRIPT_DIR/testdata/valid-flow.yaml" --context "kind-${CLUSTER_NAME}"

if wait_for_phase "flowdefinition" "test-kafka-http-flow" "Ready" 30; then
    pass "Valid FlowDefinition reconciled to Ready"
else
    phase=$(kubectl get flowdefinition test-kafka-http-flow -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" \
        -o jsonpath='{.status.phase}' 2>/dev/null || echo "<none>")
    msg=$(kubectl get flowdefinition test-kafka-http-flow -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" \
        -o jsonpath='{.status.message}' 2>/dev/null || echo "<none>")
    fail "Valid FlowDefinition: expected Ready, got phase=$phase message=$msg"
    echo "  Operator logs:"
    kubectl logs -l app.kubernetes.io/name=fiso-operator -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" --tail=20
fi

# -------------------------------------------------------------------
# 8. Test: Valid FlowDefinition (grpc→temporal with CEL) → Ready
# -------------------------------------------------------------------
TESTS=$((TESTS + 1))
info "Test: Valid FlowDefinition (grpc→temporal+CEL) should become Ready..."
kubectl apply -f "$SCRIPT_DIR/testdata/valid-flow-grpc-temporal.yaml" --context "kind-${CLUSTER_NAME}"

if wait_for_phase "flowdefinition" "test-grpc-temporal-flow" "Ready" 30; then
    pass "Valid FlowDefinition (grpc→temporal) reconciled to Ready"
else
    phase=$(kubectl get flowdefinition test-grpc-temporal-flow -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" \
        -o jsonpath='{.status.phase}' 2>/dev/null || echo "<none>")
    fail "Valid FlowDefinition (grpc→temporal): expected Ready, got phase=$phase"
fi

# -------------------------------------------------------------------
# 9. Test: Invalid FlowDefinition → rejected by CRD validation
# -------------------------------------------------------------------
TESTS=$((TESTS + 1))
info "Test: Invalid FlowDefinition (unsupported sink) should be rejected by CRD validation..."
if kubectl apply -f "$SCRIPT_DIR/testdata/invalid-flow.yaml" --context "kind-${CLUSTER_NAME}" 2>/tmp/e2e-invalid-fd-err.txt; then
    # CRD validation didn't reject it — check if reconciler catches it
    if wait_for_phase "flowdefinition" "test-invalid-flow" "Error" 30; then
        pass "Invalid FlowDefinition caught by reconciler (phase=Error)"
    else
        phase=$(kubectl get flowdefinition test-invalid-flow -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" \
            -o jsonpath='{.status.phase}' 2>/dev/null || echo "<none>")
        fail "Invalid FlowDefinition: expected CRD rejection or Error phase, got phase=$phase"
    fi
else
    # CRD validation rejected it — good
    pass "Invalid FlowDefinition rejected by CRD schema validation"
fi

# -------------------------------------------------------------------
# 10. Test: Valid LinkTarget → Ready
# -------------------------------------------------------------------
TESTS=$((TESTS + 1))
info "Test: Valid LinkTarget (https with auth) should become Ready..."
kubectl apply -f "$SCRIPT_DIR/testdata/valid-linktarget.yaml" --context "kind-${CLUSTER_NAME}"

if wait_for_phase "linktarget" "test-external-api" "Ready" 30; then
    pass "Valid LinkTarget reconciled to Ready"
else
    phase=$(kubectl get linktarget test-external-api -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" \
        -o jsonpath='{.status.phase}' 2>/dev/null || echo "<none>")
    fail "Valid LinkTarget: expected Ready, got phase=$phase"
fi

# -------------------------------------------------------------------
# 11. Test: Invalid LinkTarget → rejected by CRD validation
# -------------------------------------------------------------------
TESTS=$((TESTS + 1))
info "Test: Invalid LinkTarget (unsupported protocol) should be rejected by CRD validation..."
if kubectl apply -f "$SCRIPT_DIR/testdata/invalid-linktarget.yaml" --context "kind-${CLUSTER_NAME}" 2>/tmp/e2e-invalid-lt-err.txt; then
    # CRD validation didn't reject it — check if reconciler catches it
    if wait_for_phase "linktarget" "test-invalid-linktarget" "Error" 30; then
        pass "Invalid LinkTarget caught by reconciler (phase=Error)"
    else
        phase=$(kubectl get linktarget test-invalid-linktarget -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" \
            -o jsonpath='{.status.phase}' 2>/dev/null || echo "<none>")
        fail "Invalid LinkTarget: expected CRD rejection or Error phase, got phase=$phase"
    fi
else
    # CRD validation rejected it — good
    pass "Invalid LinkTarget rejected by CRD schema validation"
fi

# -------------------------------------------------------------------
# 12. Test: Delete FlowDefinition — no errors
# -------------------------------------------------------------------
TESTS=$((TESTS + 1))
info "Test: Deleting FlowDefinition should succeed cleanly..."
kubectl delete flowdefinition test-kafka-http-flow -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" --timeout=15s
if [ $? -eq 0 ]; then
    pass "FlowDefinition deleted cleanly"
else
    fail "FlowDefinition deletion failed"
fi

# -------------------------------------------------------------------
# 13. Test: Delete LinkTarget — no errors
# -------------------------------------------------------------------
TESTS=$((TESTS + 1))
info "Test: Deleting LinkTarget should succeed cleanly..."
kubectl delete linktarget test-external-api -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" --timeout=15s
if [ $? -eq 0 ]; then
    pass "LinkTarget deleted cleanly"
else
    fail "LinkTarget deletion failed"
fi

# -------------------------------------------------------------------
# 14. Test: List CRDs via short names
# -------------------------------------------------------------------
TESTS=$((TESTS + 1))
info "Test: CRD short names (fd, lt) should work..."
kubectl get fd -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" > /dev/null 2>&1 \
    && kubectl get lt -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" > /dev/null 2>&1
if [ $? -eq 0 ]; then
    pass "Short names 'fd' and 'lt' work"
else
    fail "Short names not working"
fi

# -------------------------------------------------------------------
# Summary
# -------------------------------------------------------------------
echo ""
echo "=== Results ==="
echo "Tests: $TESTS  |  Passed: $((TESTS - FAILURES))  |  Failed: $FAILURES"
echo ""

if [ "$FAILURES" -gt 0 ]; then
    echo "${RED}FAILED${NC}"
    echo ""
    echo "=== Operator logs ==="
    kubectl logs -l app.kubernetes.io/name=fiso-operator -n "$NAMESPACE" --context "kind-${CLUSTER_NAME}" --tail=50
    exit 1
fi

echo "${GREEN}ALL PASSED${NC}"
