#!/bin/sh
set -e

echo "=== Wasmer-AIO (All-in-One) E2E Test ==="
echo "Test: fiso-wasmer-aio combining Flow + Wasmer + Link"
echo ""
echo "This test validates:"
echo "  1. Flow HTTP source receives requests"
echo "  2. WASM interceptor transforms data"
echo "  3. Long-running Wasmer app handles requests"
echo "  4. Link proxies to backend services"
echo ""

# Build WASM modules if not already present
if [ ! -f module/transform.wasm ]; then
    echo "Building transform WASM module..."
    cd module
    GOOS=wasip1 GOARCH=wasm go build -o transform.wasm ./transform.go
    cd ..
fi

if [ ! -f wasmer-app/processor.wasm ]; then
    echo "Building long-running Wasmer app..."
    cd wasmer-app
    GOOS=wasip1 GOARCH=wasm go build -o processor.wasm .
    cd ..
fi

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"
echo "Starting services... ($BUILD_FLAG)"
docker compose up -d $BUILD_FLAG --wait

echo ""
echo "Waiting for all components to be ready..."
sleep 5

echo ""
echo "=== Test 1: Flow with WASM Transformation ==="
STATUS=$(curl -s -o /tmp/e2e-wasmer-aio-response.txt -w "%{http_code}" \
    -X POST http://localhost:8080/events/order \
    -H "Content-Type: application/json" \
    -d '{"order_id": "ORD-AIO-001", "items": [{"sku": "PROD-123", "qty": 2, "price": 49.99}], "customer": "CUST-001"}')

echo "Response status: $STATUS"
echo "Response body:"
cat /tmp/e2e-wasmer-aio-response.txt
echo ""

if [ "$STATUS" != "200" ]; then
    echo ""
    echo "FAIL: Flow test expected 200, got $STATUS"
    echo ""
    echo "=== fiso-wasmer-aio logs ==="
    docker compose logs fiso-wasmer-aio
    echo ""
    echo "=== backend-service logs ==="
    docker compose logs backend-service
    docker compose down
    exit 1
fi

# Check backend received transformed data
BACKEND_LOGS=$(docker compose logs backend-service)
if echo "$BACKEND_LOGS" | grep -q "aio_transformed"; then
    echo "SUCCESS: Flow + WASM transformation working"
else
    echo ""
    echo "FAIL: Expected 'aio_transformed' marker in backend logs"
    echo ""
    echo "=== backend-service logs ==="
    echo "$BACKEND_LOGS"
    docker compose down
    exit 1
fi

echo ""
echo "=== Test 2: Long-running Wasmer App ==="
STATUS=$(curl -s -o /tmp/e2e-wasmer-aio-response.txt -w "%{http_code}" \
    -X GET http://localhost:9000/process \
    -H "Content-Type: application/json")

echo "Response status: $STATUS"
echo "Response body:"
cat /tmp/e2e-wasmer-aio-response.txt
echo ""

if [ "$STATUS" != "200" ]; then
    echo ""
    echo "FAIL: Wasmer app test expected 200, got $STATUS"
    echo ""
    echo "=== fiso-wasmer-aio logs ==="
    docker compose logs fiso-wasmer-aio
    docker compose down
    exit 1
fi

# Verify processor response
if grep -q "processor" /tmp/e2e-wasmer-aio-response.txt; then
    echo "SUCCESS: Long-running Wasmer app responding"
else
    echo ""
    echo "FAIL: Wasmer app response missing expected marker"
    docker compose down
    exit 1
fi

echo ""
echo "=== Test 3: Link Proxy ==="
STATUS=$(curl -s -o /tmp/e2e-wasmer-aio-response.txt -w "%{http_code}" \
    -X GET http://localhost:3500/link/api/health)

echo "Response status: $STATUS"
echo "Response body:"
cat /tmp/e2e-wasmer-aio-response.txt
echo ""

if [ "$STATUS" != "200" ]; then
    echo ""
    echo "FAIL: Link proxy test expected 200, got $STATUS"
    echo ""
    echo "=== fiso-wasmer-aio logs ==="
    docker compose logs fiso-wasmer-aio
    echo ""
    echo "=== backend-service logs ==="
    docker compose logs backend-service
    docker compose down
    exit 1
fi

echo "SUCCESS: Link proxy working"

echo ""
echo "=== Test 4: Multiple Flow Routing ==="
# Test second flow
STATUS=$(curl -s -o /tmp/e2e-wasmer-aio-response.txt -w "%{http_code}" \
    -X POST http://localhost:8080/events/notification \
    -H "Content-Type: application/json" \
    -d '{"type": "email", "recipient": "user@example.com", "message": "Test notification"}')

echo "Notification flow status: $STATUS"

if [ "$STATUS" != "200" ]; then
    echo "FAIL: Notification flow expected 200, got $STATUS"
    docker compose logs fiso-wasmer-aio
    docker compose down
    exit 1
fi

echo "SUCCESS: Multiple flows routing correctly"

echo ""
echo "=== Test 5: Health and Metrics ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/healthz)
echo "Health endpoint status: $STATUS"

if [ "$STATUS" = "200" ]; then
    echo "SUCCESS: Health endpoint available"
else
    echo "WARNING: Health endpoint returned $STATUS"
fi

STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/metrics)
echo "Metrics endpoint status: $STATUS"

if [ "$STATUS" = "200" ]; then
    echo "SUCCESS: Metrics endpoint available"
else
    echo "WARNING: Metrics endpoint returned $STATUS"
fi

echo ""
echo "=== All Tests Passed ==="
echo ""
echo "Summary:"
echo "  [PASS] Flow with WASM transformation"
echo "  [PASS] Long-running Wasmer app"
echo "  [PASS] Link proxy"
echo "  [PASS] Multiple flow routing"
echo "  [INFO] Health/Metrics endpoints"
echo ""
echo "Architecture verified:"
echo "  curl -> fiso-wasmer-aio -> (Flow -> WASM -> Sink)"
echo "                              (Link -> Backend)"
echo "                              (Wasmer App)"
echo ""
echo "Cleaning up..."
docker compose down
echo "Done."
