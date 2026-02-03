#!/bin/sh
set -e

echo "=== WASM Interceptor E2E Test ==="
echo "Flow: curl → fiso-flow (WASM enrich) → user-service"
echo ""

# Build WASM module if not already present
if [ ! -f module/enrich.wasm ]; then
    echo "Building WASM module..."
    GOOS=wasip1 GOARCH=wasm go build -o module/enrich.wasm ./module/
fi

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"
echo "Starting services... ($BUILD_FLAG)"
docker compose up -d $BUILD_FLAG --wait

echo ""
echo "Waiting for services to be ready..."
sleep 3

echo ""
echo "Sending test event to fiso-flow HTTP source..."
STATUS=$(curl -s -o /tmp/e2e-wasm-response.txt -w "%{http_code}" \
    -X POST http://localhost:8081/ingest \
    -H "Content-Type: application/json" \
    -d '{"order_id": "12345", "amount": 99.99}')

echo "Response status: $STATUS"
echo "Response body:"
cat /tmp/e2e-wasm-response.txt
echo ""

if [ "$STATUS" != "200" ]; then
    echo ""
    echo "FAIL: Expected 200, got $STATUS"
    echo ""
    echo "=== fiso-flow logs ==="
    docker compose logs fiso-flow
    echo ""
    echo "=== user-service logs ==="
    docker compose logs user-service
    docker compose down
    exit 1
fi

# Give the async pipeline a moment to deliver to user-service
sleep 2

# Verify the WASM interceptor enriched the payload by checking user-service logs
USER_LOGS=$(docker compose logs user-service)
if echo "$USER_LOGS" | grep -q "wasm_enriched"; then
    echo ""
    echo "SUCCESS: WASM interceptor enriched the payload"
    echo "  curl → fiso-flow (WASM enrich) → user-service"
else
    echo ""
    echo "FAIL: Expected 'wasm_enriched' in user-service logs"
    echo ""
    echo "=== fiso-flow logs ==="
    docker compose logs fiso-flow
    echo ""
    echo "=== user-service logs ==="
    echo "$USER_LOGS"
    docker compose down
    exit 1
fi

echo ""
echo "Cleaning up..."
docker compose down
echo "Done."
