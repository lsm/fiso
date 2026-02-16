#!/bin/sh
set -e

echo "=== Wasmer Standalone E2E Test ==="
echo "Test: fiso-wasmer running a WASIX HTTP server"
echo ""

# Build WASM module if not already present
if [ ! -f http-server/server.wasm ]; then
    echo "Building WASM HTTP server module..."
    cd http-server
    GOOS=wasip1 GOARCH=wasm go build -o server.wasm .
    cd ..
fi

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"
echo "Starting services... ($BUILD_FLAG)"
docker compose up -d $BUILD_FLAG --wait

echo ""
echo "Waiting for wasmer app to be ready..."
sleep 3

echo ""
echo "Testing HTTP endpoint through fiso-wasmer..."
STATUS="000"
for i in 1 2 3 4 5 6 7 8 9 10; do
    STATUS=$(curl -s -o /tmp/e2e-wasmer-standalone-response.txt -w "%{http_code}" \
        -X GET http://localhost:9000/hello \
        -H "Accept: application/json" || true)
    if [ "$STATUS" = "200" ]; then
        break
    fi
    sleep 1
done

echo "Response status: $STATUS"
echo "Response body:"
cat /tmp/e2e-wasmer-standalone-response.txt || true
echo ""

if [ "$STATUS" != "200" ]; then
    echo ""
    echo "FAIL: Expected 200, got $STATUS"
    echo ""
    echo "=== fiso-wasmer logs ==="
    docker compose logs fiso-wasmer
    docker compose down
    exit 1
fi

# Validate response contains expected data
if ! grep -q "greeting" /tmp/e2e-wasmer-standalone-response.txt; then
    echo ""
    echo "FAIL: Response does not contain expected 'greeting' field"
    echo ""
    echo "=== fiso-wasmer logs ==="
    docker compose logs fiso-wasmer
    docker compose down
    exit 1
fi

echo ""
echo "Testing POST endpoint..."
STATUS=$(curl -s -o /tmp/e2e-wasmer-standalone-response.txt -w "%{http_code}" \
    -X POST http://localhost:9000/echo \
    -H "Content-Type: application/json" \
    -d '{"message": "hello-world", "count": 42}')

echo "Response status: $STATUS"
echo "Response body:"
cat /tmp/e2e-wasmer-standalone-response.txt
echo ""

if [ "$STATUS" != "200" ]; then
    echo ""
    echo "FAIL: Expected 200, got $STATUS"
    echo ""
    echo "=== fiso-wasmer logs ==="
    docker compose logs fiso-wasmer
    docker compose down
    exit 1
fi

# Validate echo response
if ! grep -q "hello-world" /tmp/e2e-wasmer-standalone-response.txt; then
    echo ""
    echo "FAIL: Echo response does not contain original message"
    echo ""
    echo "=== fiso-wasmer logs ==="
    docker compose logs fiso-wasmer
    docker compose down
    exit 1
fi

echo ""
echo "Testing health endpoint..."
STATUS=$(curl -s -o /tmp/e2e-wasmer-standalone-response.txt -w "%{http_code}" \
    -X GET http://localhost:9000/health)

echo "Health check status: $STATUS"

if [ "$STATUS" != "200" ]; then
    echo ""
    echo "FAIL: Health check returned $STATUS"
    echo ""
    echo "=== fiso-wasmer logs ==="
    docker compose logs fiso-wasmer
    docker compose down
    exit 1
fi

echo ""
echo "SUCCESS: All wasmer standalone tests passed"
echo "  - GET /hello: greeting endpoint working"
echo "  - POST /echo: echo endpoint working"
echo "  - GET /health: health check working"
echo ""
echo "Cleaning up..."
docker compose down
echo "Done."
