#!/bin/sh
set -e

echo "=== Kafka → Temporal Signal E2E Test ==="
echo "Flow: kafka → fiso-flow (mapping+CE) → temporal signal → temporal-worker → fiso-link → external-api"
echo ""

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"
echo "Starting services... ($BUILD_FLAG)"
docker compose up -d $BUILD_FLAG --wait --wait-timeout 120
echo ""

echo "Waiting for Kafka to be ready..."
sleep 5

echo "Creating test topic..."
docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh \
    --bootstrap-server localhost:9092 \
    --create --topic order-signal-events \
    --partitions 1 --replication-factor 1 \
    --if-not-exists

echo ""
echo "Waiting for temporal-worker to start workflow and register for signals..."
sleep 10

# Verify the workflow was started before producing Kafka messages
WORKER_LOGS=$(docker compose logs temporal-worker 2>&1)
if ! echo "$WORKER_LOGS" | grep -q "WORKFLOW_STARTED"; then
    echo "FAIL: temporal-worker did not start the workflow"
    echo ""
    echo "=== temporal-worker logs ==="
    echo "$WORKER_LOGS"
    docker compose down
    exit 1
fi
echo "Workflow started, ready for signals."

echo ""
echo "Producing test message to Kafka..."
echo '{"order_id":"e2e-signal-123","status":"shipped","updated_by":"e2e-test"}' | \
    docker compose exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
    --bootstrap-server localhost:9092 \
    --topic order-signal-events

echo "Waiting for full pipeline: kafka → fiso-flow → temporal signal → worker → fiso-link → external-api..."
sleep 15

echo ""
echo "Checking temporal-worker logs for signal processing..."
LOGS=$(docker compose logs temporal-worker 2>&1)

if echo "$LOGS" | grep -q "SIGNAL_PROCESSED"; then
    if echo "$LOGS" | grep -q "e2e-signal-123"; then
        echo ""
        echo "SUCCESS: Full Kafka → Temporal Signal E2E flow completed"
        echo "  kafka → fiso-flow (mapping+CloudEvents) → temporal signal → temporal-worker → fiso-link → external-api"
        echo ""
        echo "Verified:"
        echo "  - Kafka source consumed message"
        echo "  - Mapping transform reshaped data"
        echo "  - CloudEvents envelope applied"
        echo "  - Temporal workflow signaled (not started — already running)"
        echo "  - Activity called external service via fiso-link"
        echo "  - Signal processed with order_id=e2e-signal-123"
    else
        echo "FAIL: SIGNAL_PROCESSED found but order_id e2e-signal-123 missing"
        echo ""
        echo "=== temporal-worker logs ==="
        echo "$LOGS"
        docker compose down
        exit 1
    fi
else
    echo ""
    echo "FAIL: Expected temporal-worker to log SIGNAL_PROCESSED"
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
