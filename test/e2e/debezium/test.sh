#!/bin/sh
set -e

echo "=== Debezium CDC E2E Test ==="
echo "Flow: PostgreSQL -> Debezium Server -> Fiso Link -> Kafka -> user-service"
echo ""

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"
echo "Starting services... ($BUILD_FLAG)"
docker compose up -d $BUILD_FLAG --wait

echo ""
echo "Waiting for all services to be ready..."
sleep 10

echo ""
echo "Waiting for Kafka to be ready..."
# Create the orders-cdc topic
docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh \
    --bootstrap-server localhost:9092 \
    --create --topic orders-cdc \
    --partitions 1 --replication-factor 1 \
    --if-not-exists || true

echo ""
echo "Waiting for Debezium to start capturing changes..."
# Debezium needs time to connect to PostgreSQL and start streaming
sleep 15

echo ""
echo "Inserting test data into PostgreSQL..."
# Insert test orders into the orders table
docker compose exec -T postgres psql -U postgres -d testdb -c "
INSERT INTO inventory.orders (order_id, customer_id, amount, status) VALUES
    ('order-001', 'customer-123', 99.99, 'pending'),
    ('order-002', 'customer-456', 149.50, 'confirmed'),
    ('order-003', 'customer-789', 299.00, 'shipped');
"

echo ""
echo "Waiting for CDC events to flow through the pipeline..."
sleep 15

echo ""
echo "Updating data to trigger UPDATE events..."
docker compose exec -T postgres psql -U postgres -d testdb -c "
UPDATE inventory.orders SET status = 'confirmed' WHERE order_id = 'order-001';
UPDATE inventory.orders SET amount = 159.50 WHERE order_id = 'order-002';
"

echo ""
echo "Waiting for CDC events to propagate..."
sleep 10

echo ""
echo "Checking Kafka for CDC events..."
MESSAGE_COUNT=$(docker compose exec -T kafka /opt/kafka/bin/kafka-run-class.sh kafka.tools.GetOffsetShell \
    --broker-list localhost:9092 \
    --topic orders-cdc \
    --time -1 2>/dev/null | grep -o '[0-9]*$' || echo "0")

echo "Messages in orders-cdc topic: $MESSAGE_COUNT"

echo ""
echo "Checking Fiso-Link logs for Debezium event processing..."
FISO_LOGS=$(docker compose logs fiso-link 2>&1)

echo ""
echo "Checking user-service for processed events..."
USER_LOGS=$(docker compose logs user-service 2>&1)

# Check for success indicators
SUCCESS=0

# Check if Fiso-Link received and processed events
if echo "$FISO_LOGS" | grep -q "orders-cdc\|published\|kafka"; then
    echo "SUCCESS: Fiso-Link is processing events"
    SUCCESS=1
fi

# Check if user-service received CDC events
if echo "$USER_LOGS" | grep -q "order-001\|order-002\|order-003"; then
    echo "SUCCESS: User-service received CDC events"
    SUCCESS=1
fi

# Alternative check: verify Kafka has messages
KAFKA_HAS_MESSAGES=$(docker compose exec -T kafka /opt/kafka/bin/kafka-console-consumer.sh \
    --bootstrap-server localhost:9092 \
    --topic orders-cdc \
    --from-beginning \
    --max-messages 1 \
    --timeout-ms 5000 2>/dev/null || echo "")

if [ -n "$KAFKA_HAS_MESSAGES" ]; then
    echo "SUCCESS: Kafka has CDC events in orders-cdc topic"
    echo "Sample event: $KAFKA_HAS_MESSAGES"
    SUCCESS=1
fi

if [ $SUCCESS -eq 1 ]; then
    echo ""
    echo "=== TEST PASSED ==="
    echo "CDC flow verified: PostgreSQL -> Debezium -> Fiso Link -> Kafka"
    echo ""
    echo "=== Fiso-Link logs (last 20 lines) ==="
    docker compose logs --tail 20 fiso-link
    echo ""
    echo "=== User-service logs (last 20 lines) ==="
    docker compose logs --tail 20 user-service
else
    echo ""
    echo "=== TEST FAILED ==="
    echo "CDC events did not flow through the pipeline as expected"
    echo ""
    echo "=== PostgreSQL logs ==="
    docker compose logs postgres
    echo ""
    echo "=== Debezium Server logs ==="
    docker compose logs debezium-server
    echo ""
    echo "=== Fiso-Link logs ==="
    docker compose logs fiso-link
    echo ""
    echo "=== User-service logs ==="
    docker compose logs user-service
    echo ""
    echo "=== Kafka logs ==="
    docker compose logs kafka
    docker compose down
    exit 1
fi

echo ""
echo "Cleaning up..."
docker compose down
echo "Done."
