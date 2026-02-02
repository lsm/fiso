#!/bin/sh
set -e

echo "=== HTTP E2E Test ==="
echo "Flow: curl → fiso-flow → user-service → fiso-link → external-api"
echo ""

echo "Starting services..."
docker compose up -d --build --wait

echo ""
echo "Waiting for services to be ready..."
sleep 3

echo ""
echo "Sending test event to fiso-flow HTTP source..."
STATUS=$(curl -s -o /tmp/e2e-response.txt -w "%{http_code}" \
    -X POST http://localhost:8081/ingest \
    -H "Content-Type: application/json" \
    -d '{"order_id": "12345", "amount": 99.99}')

echo "Response status: $STATUS"
echo "Response body:"
cat /tmp/e2e-response.txt
echo ""

if [ "$STATUS" = "200" ]; then
    echo ""
    echo "SUCCESS: Full flow completed"
    echo "  curl → fiso-flow → user-service → fiso-link → external-api"
else
    echo ""
    echo "FAIL: Expected 200, got $STATUS"
    echo ""
    echo "=== fiso-flow logs ==="
    docker compose logs fiso-flow
    echo ""
    echo "=== user-service logs ==="
    docker compose logs user-service
    echo ""
    echo "=== fiso-link logs ==="
    docker compose logs fiso-link
    echo ""
    echo "=== external-api logs ==="
    docker compose logs external-api
    docker compose down
    exit 1
fi

echo ""
echo "Cleaning up..."
docker compose down
echo "Done."
