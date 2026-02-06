#!/bin/sh
# Test script to validate all Kafka key strategies
# This script sends test requests to Fiso-Link for each configured target

set -e

FISO_LINK_ADDR="${FISO_LINK_ADDR:-http://fiso-link:3500}"
FAILED=0

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_success() {
    echo "${GREEN}✓ $1${NC}"
}

log_error() {
    echo "${RED}✗ $1${NC}"
    FAILED=1
}

log_info() {
    echo "${YELLOW}▶ $1${NC}"
}

# Test 1: UUID Key Strategy
test_uuid_key() {
    log_info "Testing UUID key strategy..."
    RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${FISO_LINK_ADDR}/link/kafka-uuid" \
        -H "Content-Type: application/json" \
        -H "X-Test-Name: test-uuid-key" \
        -d '{"user_id":"user123","message":"test message with UUID key"}')

    HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
    BODY=$(echo "$RESPONSE" | head -n-1)

    if [ "$HTTP_CODE" = "200" ]; then
        log_success "UUID key strategy: $BODY"
    else
        log_error "UUID key strategy failed with HTTP $HTTP_CODE: $BODY"
    fi
}

# Test 2: Header-based Key
test_header_key() {
    log_info "Testing header-based key extraction..."
    RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${FISO_LINK_ADDR}/link/kafka-header-key" \
        -H "Content-Type: application/json" \
        -H "X-Message-Id: msg-123-from-header" \
        -H "X-Test-Name: test-header-key" \
        -d '{"message":"test with header key"}')

    HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
    BODY=$(echo "$RESPONSE" | head -n-1)

    if [ "$HTTP_CODE" = "200" ]; then
        log_success "Header key strategy: $BODY"
    else
        log_error "Header key strategy failed with HTTP $HTTP_CODE: $BODY"
    fi
}

# Test 3: Payload-based Key
test_payload_key() {
    log_info "Testing payload-based key extraction..."
    RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${FISO_LINK_ADDR}/link/kafka-payload-key" \
        -H "Content-Type: application/json" \
        -H "X-Test-Name: test-payload-key" \
        -d '{"user_id":"user-456-payload","message":"test with payload key"}')

    HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
    BODY=$(echo "$RESPONSE" | head -n-1)

    if [ "$HTTP_CODE" = "200" ]; then
        log_success "Payload key strategy: $BODY"
    else
        log_error "Payload key strategy failed with HTTP $HTTP_CODE: $BODY"
    fi
}

# Test 4: Static Key
test_static_key() {
    log_info "Testing static key strategy..."
    RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${FISO_LINK_ADDR}/link/kafka-static-key" \
        -H "Content-Type: application/json" \
        -H "X-Test-Name: test-static-key" \
        -d '{"message":"test with static key"}')

    HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
    BODY=$(echo "$RESPONSE" | head -n-1)

    if [ "$HTTP_CODE" = "200" ]; then
        log_success "Static key strategy: $BODY"
    else
        log_error "Static key strategy failed with HTTP $HTTP_CODE: $BODY"
    fi
}

# Test 5: Random Key
test_random_key() {
    log_info "Testing random key strategy..."
    RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${FISO_LINK_ADDR}/link/kafka-random-key" \
        -H "Content-Type: application/json" \
        -H "X-Test-Name: test-random-key" \
        -d '{"message":"test with random key"}')

    HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
    BODY=$(echo "$RESPONSE" | head -n-1)

    if [ "$HTTP_CODE" = "200" ]; then
        log_success "Random key strategy: $BODY"
    else
        log_error "Random key strategy failed with HTTP $HTTP_CODE: $BODY"
    fi
}

# Test 6: No Key
test_no_key() {
    log_info "Testing no key strategy..."
    RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${FISO_LINK_ADDR}/link/kafka-no-key" \
        -H "Content-Type: application/json" \
        -H "X-Test-Name: test-no-key" \
        -d '{"message":"test with no key"}')

    HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
    BODY=$(echo "$RESPONSE" | head -n-1)

    if [ "$HTTP_CODE" = "200" ]; then
        log_success "No key strategy: $BODY"
    else
        log_error "No key strategy failed with HTTP $HTTP_CODE: $BODY"
    fi
}

# Test 7: Headers Propagation
test_headers_propagation() {
    log_info "Testing headers propagation..."
    RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${FISO_LINK_ADDR}/link/kafka-headers-test" \
        -H "Content-Type: application/json" \
        -H "X-Custom-Header-1: custom-value-1" \
        -H "X-Custom-Header-2: custom-value-2" \
        -H "X-Test-Name: test-headers" \
        -d '{"message":"test headers propagation"}')

    HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
    BODY=$(echo "$RESPONSE" | head -n-1)

    if [ "$HTTP_CODE" = "200" ]; then
        log_success "Headers propagation: $BODY"
    else
        log_error "Headers propagation failed with HTTP $HTTP_CODE: $BODY"
    fi
}

# Test 8: Rate Limiting
test_rate_limiting() {
    log_info "Testing rate limiting (sending 15 rapid requests)..."
    SUCCESS=0
    RATE_LIMITED=0

    for i in $(seq 1 15); do
        RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${FISO_LINK_ADDR}/link/kafka-rate-limited" \
            -H "Content-Type: application/json" \
            -H "X-Test-Name: test-rate-limit" \
            -H "X-Iteration: $i" \
            -d '{"message":"rate limit test"}')

        HTTP_CODE=$(echo "$RESPONSE" | tail -n1)

        if [ "$HTTP_CODE" = "200" ]; then
            SUCCESS=$((SUCCESS + 1))
        elif [ "$HTTP_CODE" = "429" ]; then
            RATE_LIMITED=$((RATE_LIMITED + 1))
        fi
    done

    log_success "Rate limiting: $SUCCESS successful, $RATE_LIMITED rate limited"
}

# Test 9: Multiple Messages
test_multiple_messages() {
    log_info "Testing multiple messages (10 messages)..."
    SUCCESS=0

    for i in $(seq 1 10); do
        RESPONSE=$(curl -s -w "\n%{http_code}" -X POST "${FISO_LINK_ADDR}/link/kafka-uuid" \
            -H "Content-Type: application/json" \
            -H "X-Test-Name: test-multiple" \
            -H "X-Message-Num: $i" \
            -d "{\"message_id\":\"msg-$i\",\"message\":\"message number $i\"}")

        HTTP_CODE=$(echo "$RESPONSE" | tail -n1)

        if [ "$HTTP_CODE" = "200" ]; then
            SUCCESS=$((SUCCESS + 1))
        fi
    done

    if [ "$SUCCESS" -eq 10 ]; then
        log_success "Multiple messages: All 10 messages sent successfully"
    else
        log_error "Multiple messages: Only $SUCCESS/10 messages sent successfully"
    fi
}

# Main execution
main() {
    echo "=========================================="
    echo "Kafka Link E2E Test Suite"
    echo "Fiso-Link Address: $FISO_LINK_ADDR"
    echo "=========================================="
    echo ""

    # Health check
    log_info "Checking Fiso-Link health..."
    HEALTH_CHECK=$(curl -s -o /dev/null -w "%{http_code}" "${FISO_LINK_ADDR/http:\/\/fiso-link:3500/http:\/\/localhost:9090}/healthz" || echo "000")
    if [ "$HEALTH_CHECK" = "200" ]; then
        log_success "Fiso-Link is healthy"
    else
        log_error "Fiso-Link health check failed (HTTP $HEALTH_CHECK)"
        echo "Make sure Fiso-Link is running before running tests"
        exit 1
    fi

    echo ""

    # Run all tests
    test_uuid_key
    test_header_key
    test_payload_key
    test_static_key
    test_random_key
    test_no_key
    test_headers_propagation
    test_rate_limiting
    test_multiple_messages

    echo ""
    echo "=========================================="
    if [ $FAILED -eq 0 ]; then
        echo "${GREEN}All tests passed!${NC}"
        exit 0
    else
        echo "${RED}Some tests failed!${NC}"
        exit 1
    fi
}

main "$@"
