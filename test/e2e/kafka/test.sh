#!/bin/sh
set -e

echo "=== Kafka E2E Test ==="
echo "Flow: kafka → fiso-flow → user-service → fiso-link → external-api"
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
    --create --topic test-events \
    --partitions 1 --replication-factor 1 \
    --if-not-exists

echo ""
echo "Producing test message to Kafka..."
echo '{"order_id": "kafka-12345", "amount": 49.99}' | \
    docker compose exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
    --bootstrap-server localhost:9092 \
    --topic test-events

echo "Waiting for pipeline to process message..."
sleep 8

echo ""
echo "Checking user-service logs for processed event..."
LOGS=$(docker compose logs user-service 2>&1)

if echo "$LOGS" | grep -q "kafka-12345"; then
    echo ""
    echo "SUCCESS: Full Kafka flow completed"
    echo "  kafka → fiso-flow → user-service → fiso-link → external-api"
else
    echo ""
    echo "FAIL: Expected user-service to receive event with order_id kafka-12345"
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
    echo "=== kafka logs ==="
    docker compose logs kafka
    docker compose down
    exit 1
fi

echo ""
echo "Cleaning up..."
docker compose down
echo "Done."
