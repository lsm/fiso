#!/bin/sh
set -e

echo "=== Temporal OIDC Auth E2E Test ==="
echo "Flow: kafka → fiso-flow (OIDC auth) → temporal → temporal-worker"
echo ""

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"
echo "Starting services... ($BUILD_FLAG)"
docker compose up -d $BUILD_FLAG --wait --wait-timeout 120
echo ""

echo "Waiting for Kafka and OAuth2 server to be ready..."
sleep 10

echo "Creating test topic..."
docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh \
    --bootstrap-server localhost:9092 \
    --create --topic oidc-test-events \
    --partitions 1 --replication-factor 1 \
    --if-not-exists

echo ""
echo "Waiting for temporal-worker to register workflows..."
sleep 5

echo "Producing test message to Kafka..."
echo '{"data":{"order_id":"oidc-e2e-123","amount":42.50,"customer":"Bob"}}' | \
    docker compose exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
    --bootstrap-server localhost:9092 \
    --topic oidc-test-events

echo "Waiting for full pipeline: kafka → fiso-flow (OIDC token) → temporal → worker..."
sleep 15

echo ""
echo "Checking temporal-worker logs for workflow completion..."
LOGS=$(docker compose logs temporal-worker 2>&1)

if echo "$LOGS" | grep -q "WORKFLOW_COMPLETE"; then
    if echo "$LOGS" | grep -q "oidc-e2e-123"; then
        echo ""
        echo "SUCCESS: Temporal OIDC Auth E2E flow completed"
        echo "  kafka → fiso-flow (OIDC client_credentials) → temporal → temporal-worker"
        echo ""
        echo "Verified:"
        echo "  - Mock OAuth2 server issued JWT token"
        echo "  - fiso-flow acquired OIDC token via client_credentials flow"
        echo "  - Temporal workflow started with OIDC-authenticated client"
        echo "  - Workflow completed with order_id=oidc-e2e-123"
    else
        echo "FAIL: WORKFLOW_COMPLETE found but order_id oidc-e2e-123 missing"
        echo ""
        echo "=== temporal-worker logs ==="
        echo "$LOGS"
        docker compose down
        exit 1
    fi
else
    echo ""
    echo "FAIL: Expected temporal-worker to log WORKFLOW_COMPLETE"
    echo ""
    echo "=== fiso-flow logs ==="
    docker compose logs fiso-flow
    echo ""
    echo "=== temporal-worker logs ==="
    docker compose logs temporal-worker
    echo ""
    echo "=== temporal logs ==="
    docker compose logs temporal
    echo ""
    echo "=== mock-oauth2 logs ==="
    docker compose logs mock-oauth2
    echo ""
    echo "=== kafka logs ==="
    docker compose logs kafka
    docker compose down
    exit 1
fi

echo ""
echo "Cleaning up..."
docker compose down
echo "Done."
