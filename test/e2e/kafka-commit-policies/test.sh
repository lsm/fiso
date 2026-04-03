#!/bin/sh
set -e

echo "=== Kafka Commit Policies E2E Tests ==="
echo "Covers: commitPolicy sink, sink_or_dlq, kafka_transaction"
echo ""

cleanup() {
    echo ""
    echo "Cleaning up..."
    docker compose down
}
trap cleanup EXIT

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"

# ─────────────────────────────────────────────────────────────────────────────
# Bootstrap: start Kafka and user-service once for all sub-tests
# ─────────────────────────────────────────────────────────────────────────────
echo "Starting base services..."
docker compose up -d $BUILD_FLAG kafka user-service --wait
echo ""
echo "Waiting for Kafka to be ready..."
sleep 5

echo "Creating topics..."
for topic in cp-sink-events cp-dlq-events cp-dlq-dead cp-tx-source cp-tx-sink; do
    docker compose exec -T kafka /opt/kafka/bin/kafka-topics.sh \
        --bootstrap-server localhost:9092 \
        --create --topic "$topic" \
        --partitions 1 --replication-factor 1 \
        --if-not-exists
done
echo ""

# ─────────────────────────────────────────────────────────────────────────────
# Test 1: commitPolicy: sink
#   Produce two messages → both delivered to HTTP sink → offsets committed
# ─────────────────────────────────────────────────────────────────────────────
echo "=== Test 1: commitPolicy: sink ==="

echo "Producing messages to cp-sink-events..."
printf '%s\n%s\n' \
    '{"id":"sink-msg-1","value":"hello"}' \
    '{"id":"sink-msg-2","value":"world"}' | \
    docker compose exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
        --bootstrap-server localhost:9092 --topic cp-sink-events

echo "Starting fiso-flow-sink..."
docker compose up -d $BUILD_FLAG fiso-flow-sink
echo "Waiting for pipeline to process messages..."
sleep 10

SINK_LOGS=$(docker compose logs user-service 2>&1)
SINK_FAIL=0
echo "$SINK_LOGS" | grep -q "sink-msg-1" || SINK_FAIL=1
echo "$SINK_LOGS" | grep -q "sink-msg-2" || SINK_FAIL=1

if [ "$SINK_FAIL" -eq 1 ]; then
    echo "FAIL: commitPolicy: sink — messages not delivered to sink"
    echo ""
    echo "=== fiso-flow-sink logs ==="
    docker compose logs fiso-flow-sink
    echo ""
    echo "=== user-service logs ==="
    docker compose logs user-service
    exit 1
fi
echo "PASS: commitPolicy: sink — both messages delivered"

# Verify consumer group offset caught up (LAG = 0)
sleep 2
CONSUMER_INFO=$(docker compose exec -T kafka /opt/kafka/bin/kafka-consumer-groups.sh \
    --bootstrap-server localhost:9092 \
    --describe --group fiso-cp-sink 2>/dev/null || true)
if echo "$CONSUMER_INFO" | grep -q "cp-sink-events"; then
    LAG=$(echo "$CONSUMER_INFO" | grep "cp-sink-events" | awk '{print $NF}')
    echo "  consumer group fiso-cp-sink lag: ${LAG}"
    if [ "${LAG}" != "0" ] && [ -n "${LAG}" ]; then
        echo "WARN: expected lag=0 after processing, got lag=${LAG}"
    fi
fi

docker compose stop fiso-flow-sink
echo ""

# ─────────────────────────────────────────────────────────────────────────────
# Test 2: commitPolicy: sink_or_dlq
#   Produce one good message + one "fail-me" message
#   Good message → delivered to sink; fail-me → routed to DLQ topic
#   Both offsets committed (pipeline does not stall on failure)
# ─────────────────────────────────────────────────────────────────────────────
echo "=== Test 2: commitPolicy: sink_or_dlq ==="

echo "Producing messages to cp-dlq-events (one good, one fail)..."
printf '%s\n%s\n' \
    '{"id":"dlq-good-1","value":"deliver me"}' \
    '{"id":"dlq-fail-me-1","value":"fail-me trigger"}' | \
    docker compose exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
        --bootstrap-server localhost:9092 --topic cp-dlq-events

echo "Starting fiso-flow-sink-or-dlq..."
docker compose up -d $BUILD_FLAG fiso-flow-sink-or-dlq
echo "Waiting for pipeline to process messages..."
sleep 12

# Good message must appear in user-service
DLQ_LOGS=$(docker compose logs user-service 2>&1)
if ! echo "$DLQ_LOGS" | grep -q "dlq-good-1"; then
    echo "FAIL: commitPolicy: sink_or_dlq — good message not delivered to sink"
    echo ""
    echo "=== fiso-flow-sink-or-dlq logs ==="
    docker compose logs fiso-flow-sink-or-dlq
    echo ""
    echo "=== user-service logs ==="
    docker compose logs user-service
    exit 1
fi
echo "PASS: good message delivered to sink"

# Failed message must be in DLQ topic
DLQ_MSGS=$(docker compose exec -T kafka /opt/kafka/bin/kafka-console-consumer.sh \
    --bootstrap-server localhost:9092 \
    --topic cp-dlq-dead \
    --from-beginning \
    --max-messages 1 \
    --timeout-ms 8000 2>&1 | grep -v '^$\|^Processed\|^%\|WARN\|INFO' || true)

if echo "$DLQ_MSGS" | grep -q "fail-me"; then
    echo "PASS: failed message routed to DLQ topic"
else
    echo "FAIL: failed message not found in DLQ topic"
    echo "  DLQ consumer output: ${DLQ_MSGS}"
    echo ""
    echo "=== fiso-flow-sink-or-dlq logs ==="
    docker compose logs fiso-flow-sink-or-dlq
    exit 1
fi

docker compose stop fiso-flow-sink-or-dlq
echo ""

# ─────────────────────────────────────────────────────────────────────────────
# Test 3: commitPolicy: kafka_transaction
#   Produce two messages to source topic → fiso-flow transactionally
#   consumes, passes through identity transform, and produces to sink topic
#   Verify messages appear in sink topic with read_committed isolation
# ─────────────────────────────────────────────────────────────────────────────
echo "=== Test 3: commitPolicy: kafka_transaction ==="

echo "Producing messages to cp-tx-source..."
printf '%s\n%s\n' \
    '{"id":"tx-msg-1","value":"transactional"}' \
    '{"id":"tx-msg-2","value":"eos"}' | \
    docker compose exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
        --bootstrap-server localhost:9092 --topic cp-tx-source

echo "Starting fiso-flow-kafka-tx..."
docker compose up -d $BUILD_FLAG fiso-flow-kafka-tx
echo "Waiting for transactional pipeline to process messages..."
sleep 12

# Read from sink topic with read_committed isolation to verify committed writes
TX_MSGS=$(docker compose exec -T kafka /opt/kafka/bin/kafka-console-consumer.sh \
    --bootstrap-server localhost:9092 \
    --topic cp-tx-sink \
    --from-beginning \
    --max-messages 2 \
    --timeout-ms 10000 \
    --isolation-level read_committed 2>&1 | grep -v '^$\|^Processed\|^%\|WARN\|INFO' || true)

TX_FAIL=0
echo "$TX_MSGS" | grep -q "tx-msg-1" || TX_FAIL=1
echo "$TX_MSGS" | grep -q "tx-msg-2" || TX_FAIL=1

if [ "$TX_FAIL" -eq 1 ]; then
    echo "FAIL: commitPolicy: kafka_transaction — messages not in sink topic"
    echo "  consumer output: ${TX_MSGS}"
    echo ""
    echo "=== fiso-flow-kafka-tx logs ==="
    docker compose logs fiso-flow-kafka-tx
    exit 1
fi
echo "PASS: commitPolicy: kafka_transaction — both messages committed to sink topic"

docker compose stop fiso-flow-kafka-tx
echo ""

# ─────────────────────────────────────────────────────────────────────────────
echo "=== All commit policy e2e tests PASSED ==="
