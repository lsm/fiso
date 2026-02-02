#!/bin/sh
set -e

echo "=== Kafka → Temporal E2E Test ==="
echo "Flow: kafka → fiso-flow (mapping+CE) → temporal → temporal-worker → fiso-link → external-api"
echo ""

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"
echo "Starting services... ($BUILD_FLAG)"
docker compose up -d $BUILD_FLAG --wait
echo ""

echo "Waiting for Kafka to be ready..."
sleep 5

echo "Creating test topic..."
docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh \
    --bootstrap-server localhost:9092 \
    --create --topic order-events \
    --partitions 1 --replication-factor 1 \
    --if-not-exists

echo ""
echo "Waiting for temporal-worker to register workflows..."
sleep 5

echo "Producing test message to Kafka..."
echo '{"order_id":"e2e-123","amount":99.99,"customer":"Alice"}' | \
    docker compose exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
    --bootstrap-server localhost:9092 \
    --topic order-events

echo "Waiting for full pipeline: kafka → fiso-flow → temporal → worker → fiso-link → external-api..."
sleep 15

echo ""
echo "Checking temporal-worker logs for workflow completion..."
LOGS=$(docker compose logs temporal-worker 2>&1)

if echo "$LOGS" | grep -q "WORKFLOW_COMPLETE"; then
    if echo "$LOGS" | grep -q "e2e-123"; then
        echo ""
        echo "SUCCESS: Full Kafka → Temporal E2E flow completed"
        echo "  kafka → fiso-flow (mapping+CloudEvents) → temporal → temporal-worker → fiso-link → external-api"
        echo ""
        echo "Verified:"
        echo "  - Kafka source consumed message"
        echo "  - Mapping transform reshaped data"
        echo "  - CloudEvents envelope applied"
        echo "  - Temporal workflow started"
        echo "  - Activity called external service via fiso-link"
        echo "  - Workflow completed with order_id=e2e-123"
    else
        echo "FAIL: WORKFLOW_COMPLETE found but order_id e2e-123 missing"
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
    echo "=== fiso-link logs ==="
    docker compose logs fiso-link
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
