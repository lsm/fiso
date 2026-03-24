#!/bin/sh
set -e

echo "=== Kafka E2E Test (numeric startOffset) ==="
echo "Flow: kafka(offset=1) → fiso-flow → user-service → fiso-link → external-api"
echo ""

cleanup() {
    echo ""
    echo "Cleaning up..."
    docker compose down
}
trap cleanup EXIT

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"

echo "Starting base services... ($BUILD_FLAG)"
docker compose up -d $BUILD_FLAG kafka external-api fiso-link user-service --wait
echo ""

echo "Waiting for Kafka to be ready..."
sleep 5

echo "Creating test topic..."
docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh \
    --bootstrap-server localhost:9092 \
    --create --topic test-events \
    --partitions 1 --replication-factor 1 \
    --if-not-exists

echo ""
echo "Producing two test messages (offsets 0 and 1) before fiso-flow starts..."
printf '%s\n%s\n' \
    '{"order_id":"kafka-ignored-0","amount":10.00}' \
    '{"order_id":"kafka-processed-1","amount":49.99}' | \
    docker compose exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
    --bootstrap-server localhost:9092 \
    --topic test-events

echo "Starting fiso-flow (configured with startOffset: 1)..."
docker compose up -d $BUILD_FLAG fiso-flow --wait

echo "Waiting for pipeline to process message..."
sleep 8

echo ""
echo "Checking user-service logs..."
LOGS=$(docker compose logs user-service 2>&1)

HAS_PROCESSED=0
HAS_IGNORED=0

if echo "$LOGS" | grep -q "kafka-processed-1"; then
    HAS_PROCESSED=1
fi

if echo "$LOGS" | grep -q "kafka-ignored-0"; then
    HAS_IGNORED=1
fi

if [ "$HAS_PROCESSED" -eq 1 ] && [ "$HAS_IGNORED" -eq 0 ]; then
    echo ""
    echo "SUCCESS: Numeric startOffset respected"
    echo "  processed offset 1 event, skipped offset 0 event"
else
    echo ""
    echo "FAIL: Unexpected offset behavior"
    echo "  expected: kafka-processed-1 present, kafka-ignored-0 absent"
    echo ""
    echo "=== user-service logs ==="
    docker compose logs user-service
    echo ""
    echo "=== fiso-flow logs ==="
    docker compose logs fiso-flow
    echo ""
    echo "=== kafka logs ==="
    docker compose logs kafka
    exit 1
fi

echo ""
echo "Done."
