#!/bin/sh
# Script to consume messages from Kafka topics for verification

set -e

KAFKA_BROKERS="${KAFKA_BROKERS:-kafka:9092}"
TOPIC="${1:-test-events-uuid}"
MAX_MESSAGES="${2:-10}"

echo "Consuming from topic: $TOPIC"
echo "Broker: $KAFKA_BROKERS"
echo "Max messages: $MAX_MESSAGES"
echo "Press Ctrl+C to stop"
echo ""

kafka-console-consumer \
    --bootstrap-server "$KAFKA_BROKERS" \
    --topic "$TOPIC" \
    --from-beginning \
    --max-messages "$MAX_MESSAGES" \
    --property print.key=true \
    --property key.separator=" | " \
    --property print.headers=true \
    --property headers.separator=" | " \
    --formatter "kafka.tools.DefaultMessageFormatter"
