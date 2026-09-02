#!/bin/sh
set -e

echo "=== Wasmer-Link E2E Test ==="
echo "Test: fiso-wasmer-link combining proxy with embedded Wasmer apps"
echo "Flow: curl -> fiso-wasmer-link (proxy + WASM intercept) -> backend-service"
echo ""

# Build WASM module if not already present
if [ ! -f module/intercept.wasm ]; then
    echo "Building WASM intercept module..."
    cd module
    GOOS=wasip1 GOARCH=wasm go build -o intercept.wasm .
    cd ..
fi

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"
echo "Starting services... ($BUILD_FLAG)"
docker compose up -d $BUILD_FLAG --wait

echo ""
echo "Waiting for services to be ready..."
sleep 3

echo ""
echo "Testing proxy with WASM interception..."
STATUS=$(curl -s -D /tmp/e2e-wasmer-link-headers.txt -o /tmp/e2e-wasmer-link-response.txt -w "%{http_code}" \
    -X POST http://localhost:3500/link/api/process \
    -H "Content-Type: application/json" \
    -H "X-Request-ID: test-req-001" \
    -d '{"action": "calculate", "params": {"value": 100, "operation": "double"}}')

echo "Response status: $STATUS"
echo "Response headers:"
cat /tmp/e2e-wasmer-link-headers.txt
echo "Response body:"
cat /tmp/e2e-wasmer-link-response.txt
echo ""

if [ "$STATUS" != "200" ]; then
    echo ""
    echo "FAIL: Expected 200, got $STATUS"
    echo ""
    echo "=== fiso-wasmer-link logs ==="
    docker compose logs fiso-wasmer-link
    echo ""
    echo "=== backend-service logs ==="
    docker compose logs backend-service
    docker compose down
    exit 1
fi

# The backend stamps X-Proxy on responses; it must survive the proxy hop.
if grep -qi '^x-proxy:' /tmp/e2e-wasmer-link-headers.txt; then
    echo "SUCCESS: Response carried the backend's X-Proxy header through the link"
else
    echo ""
    echo "FAIL: Response did not carry the backend's X-Proxy header"
    echo ""
    echo "=== fiso-wasmer-link logs ==="
    docker compose logs fiso-wasmer-link
    echo ""
    echo "=== backend-service logs ==="
    docker compose logs backend-service
    docker compose down
    exit 1
fi

# The transformed payload must still be processable: value 100 doubled.
if grep -q '"result":200' /tmp/e2e-wasmer-link-response.txt; then
    echo "SUCCESS: Backend processed the intercepted payload (100 doubled to 200)"
else
    echo ""
    echo "FAIL: Backend did not process the intercepted payload as expected"
    echo ""
    echo "=== fiso-wasmer-link logs ==="
    docker compose logs fiso-wasmer-link
    echo ""
    echo "=== backend-service logs ==="
    docker compose logs backend-service
    docker compose down
    exit 1
fi

# Verify the backend received the intercepted request, not just any request.
BACKEND_LOGS=$(docker compose logs backend-service)

echo ""
echo "Checking for WASM interception markers at the backend..."

# Check that backend received the request
if echo "$BACKEND_LOGS" | grep -q "received request"; then
    echo "SUCCESS: Backend received the request"
else
    echo ""
    echo "FAIL: Backend did not receive the request"
    echo ""
    echo "=== fiso-wasmer-link logs ==="
    docker compose logs fiso-wasmer-link
    echo ""
    echo "=== backend-service logs ==="
    echo "$BACKEND_LOGS"
    docker compose down
    exit 1
fi

# The interceptor adds an X-Intercepted-By header; the backend must see it.
if echo "$BACKEND_LOGS" | grep -q "X-Intercepted-By=wasmer-link"; then
    echo "SUCCESS: Interceptor's X-Intercepted-By header reached the backend"
else
    echo ""
    echo "FAIL: Interceptor's X-Intercepted-By header did not reach the backend (no interception?)"
    echo ""
    echo "=== fiso-wasmer-link logs ==="
    docker compose logs fiso-wasmer-link
    echo ""
    echo "=== backend-service logs ==="
    echo "$BACKEND_LOGS"
    docker compose down
    exit 1
fi

# The interceptor stamps the payload; the backend must log the markers.
if echo "$BACKEND_LOGS" | grep -q '"intercepted":true'; then
    echo "SUCCESS: Interceptor's payload marker reached the backend"
else
    echo ""
    echo "FAIL: Interceptor's payload marker did not reach the backend (no interception?)"
    echo ""
    echo "=== fiso-wasmer-link logs ==="
    docker compose logs fiso-wasmer-link
    echo ""
    echo "=== backend-service logs ==="
    echo "$BACKEND_LOGS"
    docker compose down
    exit 1
fi

# The interceptor's env (INTERCEPT_MODE) must propagate through the runtime.
if echo "$BACKEND_LOGS" | grep -q '"intercept_mode":"request-response"'; then
    echo "SUCCESS: Interceptor env propagated (intercept_mode=request-response)"
else
    echo ""
    echo "FAIL: Interceptor env did not propagate (intercept_mode missing or wrong)"
    echo ""
    echo "=== fiso-wasmer-link logs ==="
    docker compose logs fiso-wasmer-link
    echo ""
    echo "=== backend-service logs ==="
    echo "$BACKEND_LOGS"
    docker compose down
    exit 1
fi

echo ""
echo "Testing GET endpoint..."
STATUS=$(curl -s -o /tmp/e2e-wasmer-link-response.txt -w "%{http_code}" \
    -X GET http://localhost:3500/link/api/status \
    -H "X-Request-ID: test-req-002")

echo "Response status: $STATUS"
echo "Response body:"
cat /tmp/e2e-wasmer-link-response.txt
echo ""

if [ "$STATUS" != "200" ]; then
    echo ""
    echo "FAIL: GET request returned $STATUS"
    echo ""
    echo "=== fiso-wasmer-link logs ==="
    docker compose logs fiso-wasmer-link
    echo ""
    echo "=== backend-service logs ==="
    docker compose logs backend-service
    docker compose down
    exit 1
fi

echo ""
echo "Testing metrics endpoint..."
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
    -X GET http://localhost:9091/metrics)

if [ "$STATUS" = "200" ]; then
    echo "SUCCESS: Metrics endpoint available"
else
    echo "WARNING: Metrics endpoint returned $STATUS"
fi

echo ""
echo "SUCCESS: All wasmer-link tests passed"
echo "  - Proxy routing working"
echo "  - WASM interceptor transformed the outbound request (header + payload + env)"
echo "  - Backend processed the intercepted payload"
echo "  - Multiple HTTP methods supported"
echo ""
echo "=== Full Flow Summary ==="
echo "  curl -> fiso-wasmer-link -> backend-service"
echo ""
echo "Cleaning up..."
docker compose down
echo "Done."
