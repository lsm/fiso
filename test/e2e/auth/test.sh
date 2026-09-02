#!/bin/sh
set -e

echo "=== WASM Auth Interceptor E2E Test ==="
echo "Flow: curl → fiso-flow (wasm JWT auth) → user-service"
echo ""

# Build WASM module if not already present
if [ ! -f module/auth.wasm ]; then
    echo "Building auth module..."
    GOOS=wasip1 GOARCH=wasm go build -o module/auth.wasm ../../../examples/interceptors/auth/
fi

BUILD_FLAG="${E2E_BUILD_FLAG:---build}"
echo "Starting services... ($BUILD_FLAG)"
docker compose up -d $BUILD_FLAG --wait

echo ""
echo "Waiting for services to be ready..."
sleep 3

mint() {
    # mint <exp-offset> [secret]
    GOOS= GOARCH= go run ../../../test/e2e/auth/token -secret "${2:-e2e-hs256-secret}" -sub alice -exp "$1"
}

EXPECT_401() {
    LABEL="$1"; shift
    STATUS=$(curl -s -o /tmp/e2e-auth-response.txt -w "%{http_code}" \
        -X POST http://localhost:8083/ingest \
        -H "Content-Type: application/json" "$@" \
        -d '{"order_id": "12345", "amount": 99.99}')
    echo "  $LABEL -> $STATUS $(cat /tmp/e2e-auth-response.txt)"
    if [ "$STATUS" != "401" ]; then
        echo ""
        echo "FAIL: $LABEL: expected 401, got $STATUS"
        echo ""
        echo "=== fiso-flow logs ==="
        docker compose logs fiso-flow
        docker compose down
        exit 1
    fi
}

echo "Sending requests to fiso-flow HTTP source..."
echo ""

# 1. No credentials
EXPECT_401 "no Authorization header"

# 2. Malformed token
EXPECT_401 "malformed token" -H "Authorization: Bearer not-a-jwt"

# 3. Expired token (signed with the right secret)
EXPIRED=$(mint -1h)
EXPECT_401 "expired token" -H "Authorization: Bearer $EXPIRED"

# 4. Forged token (valid shape, wrong secret)
FORGED=$(mint 1h wrong-secret)
EXPECT_401 "forged signature" -H "Authorization: Bearer $FORGED"

# 5. Valid token
VALID=$(mint 1h)
STATUS=$(curl -s -o /tmp/e2e-auth-response.txt -w "%{http_code}" \
    -X POST http://localhost:8083/ingest \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $VALID" \
    -d '{"order_id": "12345", "amount": 99.99}')
echo "  valid token -> $STATUS $(cat /tmp/e2e-auth-response.txt)"
if [ "$STATUS" != "200" ]; then
    echo ""
    echo "FAIL: valid token: expected 200, got $STATUS"
    echo ""
    echo "=== fiso-flow logs ==="
    docker compose logs fiso-flow
    docker compose down
    exit 1
fi

# Give the async pipeline a moment to deliver to user-service
sleep 2

# Verify the verdict at the sink: the credential is stripped and the
# authenticated identity is present.
USER_LOGS=$(docker compose logs user-service)
echo ""
echo "user-service saw:"
echo "$USER_LOGS" | grep "received event" | tail -1

if ! echo "$USER_LOGS" | grep -q "X-Authenticated=true"; then
    echo ""
    echo "FAIL: Expected 'X-Authenticated=true' in user-service logs"
    docker compose down
    exit 1
fi
if ! echo "$USER_LOGS" | grep -q "X-Auth-Subject=alice"; then
    echo ""
    echo "FAIL: Expected 'X-Auth-Subject=alice' in user-service logs"
    docker compose down
    exit 1
fi
if echo "$USER_LOGS" | grep -q "Authorization="; then
    echo ""
    echo "FAIL: The raw credential must not reach the sink (Authorization forwarded)"
    docker compose down
    exit 1
fi

echo ""
echo "SUCCESS: wasm auth interceptor authenticated the request"
echo "  401: missing / malformed / expired / forged"
echo "  200: valid token, Authorization stripped, X-Auth-Subject set"
echo "  curl → fiso-flow (wasm JWT auth) → user-service"

echo ""
echo "Cleaning up..."
docker compose down
echo "Done."
