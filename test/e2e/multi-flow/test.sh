#!/bin/sh
set -e

echo "=== Multi-Flow E2E Test ==="
echo "Tests that fiso-flow runs multiple flows on a shared port with path-based routing"
echo ""
echo "Flow A: curl:8080/ingest-a → fiso-flow → user-service/flow-a"
echo "Flow B: curl:8080/ingest-b → fiso-flow → user-service/flow-b"
echo ""

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"
echo "Starting services... ($BUILD_FLAG)"
docker compose up -d $BUILD_FLAG --wait

echo ""
echo "Waiting for services to be ready..."
sleep 3

echo ""
echo "=== Test 1: Send event to Flow A (port 8080, path /ingest-a) ==="
STATUS_A=$(curl -s -o /tmp/e2e-response-a.txt -w "%{http_code}" \
    -X POST http://localhost:8080/ingest-a \
    -H "Content-Type: application/json" \
    -d '{"flow": "a", "order_id": "A-001"}')

echo "Response status: $STATUS_A"
echo "Response body:"
cat /tmp/e2e-response-a.txt
echo ""

echo ""
echo "=== Test 2: Send event to Flow B (port 8080, path /ingest-b) ==="
STATUS_B=$(curl -s -o /tmp/e2e-response-b.txt -w "%{http_code}" \
    -X POST http://localhost:8080/ingest-b \
    -H "Content-Type: application/json" \
    -d '{"flow": "b", "order_id": "B-001"}')

echo "Response status: $STATUS_B"
echo "Response body:"
cat /tmp/e2e-response-b.txt
echo ""

echo ""
echo "=== Test 3: Verify both flows processed events ==="
STATS=$(curl -s http://localhost:8082/stats)
echo "Stats: $STATS"

# Check both flows received events
FLOW_A_COUNT=$(echo "$STATS" | grep -o '"flow_a_count":[0-9]*' | grep -o '[0-9]*')
FLOW_B_COUNT=$(echo "$STATS" | grep -o '"flow_b_count":[0-9]*' | grep -o '[0-9]*')

echo ""
if [ "$STATUS_A" = "200" ] && [ "$STATUS_B" = "200" ] && [ "$FLOW_A_COUNT" -ge 1 ] && [ "$FLOW_B_COUNT" -ge 1 ]; then
    echo "SUCCESS: Both flows running concurrently!"
    echo "  Flow A processed: $FLOW_A_COUNT event(s)"
    echo "  Flow B processed: $FLOW_B_COUNT event(s)"
else
    echo "FAIL: Expected both flows to process events"
    echo "  STATUS_A=$STATUS_A, STATUS_B=$STATUS_B"
    echo "  FLOW_A_COUNT=$FLOW_A_COUNT, FLOW_B_COUNT=$FLOW_B_COUNT"
    echo ""
    echo "=== fiso-flow logs ==="
    docker compose logs fiso-flow
    echo ""
    echo "=== user-service logs ==="
    docker compose logs user-service
    docker compose down
    exit 1
fi

echo ""
echo "=== Test 4: Readiness is 200 while all required flows serve ==="
READY=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:9091/readyz)
echo "readyz: $READY"
if [ "$READY" != "200" ]; then
    echo "FAIL: expected /readyz 200 with all flows serving"
    docker compose logs fiso-flow
    docker compose down
    exit 1
fi
echo "SUCCESS: /readyz 200 before any terminal failure"

echo ""
echo "=== Test 5: Terminal flow failure drops readiness, survivors keep serving ==="
# flow-c and flow-d both bind a gRPC source on :9095. Exactly one bind
# succeeds; the other pipeline dies terminally at startup (address already
# in use), which must drop readiness without disturbing anything else.
cat > fiso/flows/flow-c.yaml <<'YAML'
name: flow-c
source:
  type: grpc
  config:
    listenAddr: ":9095"
sink:
  type: http
  config:
    url: http://user-service:8082/flow-c
    method: POST
YAML
cat > fiso/flows/flow-d.yaml <<'YAML'
name: flow-d
source:
  type: grpc
  config:
    listenAddr: ":9095"
sink:
  type: http
  config:
    url: http://user-service:8082/flow-d
    method: POST
YAML
docker compose restart fiso-flow >/dev/null 2>&1

# readiness must become 503 (terminal flow) while healthz stays 200
READY=""; I=0
while [ $I -lt 15 ]; do
    sleep 1
    READY=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:9091/readyz || echo "000")
    [ "$READY" = "503" ] && break
    I=$((I+1))
done
HEALTH=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:9091/healthz || echo "000")
echo "readyz: $READY  healthz: $HEALTH"
if [ "$READY" != "503" ] || [ "$HEALTH" != "200" ]; then
    echo "FAIL: expected /readyz 503 with /healthz 200 after terminal flow failure"
    docker compose logs fiso-flow
    docker compose down
    rm -f fiso/flows/flow-c.yaml fiso/flows/flow-d.yaml
    exit 1
fi

# surviving flows must still process events (aggregate readiness only)
SURVIVOR=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/ingest-a \
    -H "Content-Type: application/json" -d '{"flow": "a", "order_id": "A-002"}')
STATS=$(curl -s http://localhost:8082/stats)
FLOW_A_AFTER=$(echo "$STATS" | grep -o '"flow_a_count":[0-9]*' | grep -o '[0-9]*')
echo "survivor flow-a status: $SURVIVOR (flow_a_count=$FLOW_A_AFTER)"
if [ "$SURVIVOR" != "200" ] || [ "$FLOW_A_AFTER" -lt 2 ]; then
    echo "FAIL: surviving flow stopped processing after another flow died"
    docker compose logs fiso-flow
    docker compose down
    rm -f fiso/flows/flow-c.yaml fiso/flows/flow-d.yaml
    exit 1
fi
rm -f fiso/flows/flow-c.yaml fiso/flows/flow-d.yaml
echo "SUCCESS: /readyz 503, /healthz 200, surviving flow still serving"

echo ""
echo "Cleaning up..."
docker compose down
echo "Done."
