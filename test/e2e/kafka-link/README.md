# Kafka Link E2E Tests

End-to-end integration tests for Fiso-Link's Kafka target support feature. These tests use real Kafka (via Docker) to verify the complete message flow works correctly.

## Overview

This test suite validates:
- All 5 Kafka key generation strategies (uuid, header, payload, static, random)
- Message publishing and consumption
- Header propagation from HTTP to Kafka
- Circuit breaker behavior
- Rate limiting
- Retry logic
- Multiple message handling

## Prerequisites

- Docker (20.10 or later)
- Docker Compose (2.0 or later)
- Go 1.23+ (for local test execution)
- Make (optional, for automation)

## Quick Start

### Using Make (Recommended)

```bash
# Set up the test environment
make setup

# Start all services
make start

# Run all tests
make test

# Stop services
make stop

# Clean up everything
make clean
```

### Using Docker Compose Directly

```bash
# Start services
docker-compose up -d

# Run tests
docker-compose run --rm test-runner go test -v ./test/e2e/kafka-link/...

# Stop services
docker-compose down
```

### Running Tests Locally

If you have Kafka and Fiso-Link running locally:

```bash
# Set environment variables
export KAFKA_BROKERS=localhost:9092
export FISO_LINK_ADDR=http://localhost:3500

# Run tests
go test -v -timeout=10m ./test/e2e/kafka-link/...
```

## Test Scenarios

### 1. TestKafkaUUIDKeyStrategy
Tests publishing with UUID key strategy. Verifies that messages are published with valid UUID keys.

### 2. TestKafkaHeaderKeyStrategy
Tests header-based key extraction. Verifies that the key is correctly extracted from the `X-Message-Id` header.

### 3. TestKafkaPayloadKeyStrategy
Tests payload-based key extraction. Verifies that the key is correctly extracted from the `user_id` field in the JSON payload.

### 4. TestKafkaStaticKeyStrategy
Tests static key strategy. Verifies that all messages use the configured static key.

### 5. TestKafkaRandomKeyStrategy
Tests random key strategy (timestamp-based). Verifies that non-empty keys are generated.

### 6. TestKafkaNoKey
Tests publishing with no key. Verifies that messages are published with nil/empty keys.

### 7. TestKafkaHeadersPropagation
Tests header propagation from HTTP to Kafka. Verifies that both static headers (from config) and dynamic headers (from HTTP request) are correctly propagated.

### 8. TestKafkaCircuitBreaker
Tests circuit breaker behavior. Verifies that the circuit breaker is configured correctly.

### 9. TestKafkaRateLimiting
Tests rate limiting functionality. Verifies that requests exceeding the configured rate are properly rate-limited.

### 10. TestKafkaRetryOnTransientFailures
Tests retry logic. Verifies that messages are retried on transient failures.

### 11. TestKafkaMultipleMessages
Tests publishing multiple messages. Verifies that all messages are correctly published and consumed.

## Configuration

### Fiso-Link Configuration

The test configuration (`config/fiso-link-config.yaml`) defines 10 Kafka targets:

| Target Name | Key Strategy | Purpose |
|-------------|-------------|---------|
| kafka-uuid | UUID | Tests UUID key generation |
| kafka-header-key | Header | Tests header-based key extraction |
| kafka-payload-key | Payload | Tests payload-based key extraction |
| kafka-static-key | Static | Tests static key |
| kafka-random-key | Random | Tests random key generation |
| kafka-no-key | None | Tests no key |
| kafka-rate-limited | UUID | Tests rate limiting (10 req/s) |
| kafka-cb-test | UUID | Tests circuit breaker |
| kafka-retry-test | UUID | Tests retry logic |
| kafka-headers-test | UUID | Tests header propagation |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| KAFKA_BROKERS | kafka:9092 | Comma-separated list of Kafka brokers |
| FISO_LINK_ADDR | http://fiso-link:3500 | Fiso-Link service address |

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make help` | Show all available targets |
| `make setup` | Set up the test environment |
| `make start` | Start all services |
| `make stop` | Stop all services |
| `make restart` | Restart all services |
| `make test` | Run all E2E tests |
| `make test-verbose` | Run tests with extra verbosity |
| `make test-race` | Run tests with race detection |
| `make test-short` | Run short tests only |
| `make test-local` | Run tests locally |
| `make test-one TEST=name` | Run a specific test |
| `make test-coverage` | Run tests with coverage |
| `make clean` | Clean up test artifacts |
| `make clean-all` | Clean up everything including images |
| `make shell` | Open shell in test runner |
| `make logs` | Show logs from all services |
| `make health` | Check health of all services |
| `make ci` | Run full CI pipeline |
| `make quick-test` | Quick test cycle (start, test, stop) |

## Running Individual Tests

### Using Make

```bash
# Run a specific test
make test-one TEST=TestKafkaUUIDKeyStrategy

# Run all UUID-related tests
make test-one TEST=TestKafka.*UUID
```

### Using Go Directly

```bash
# Inside test runner container
go test -v -run TestKafkaUUIDKeyStrategy ./test/e2e/kafka-link/

# From host (with services running)
docker-compose exec test-runner go test -v -run TestKafkaUUIDKeyStrategy
```

## Troubleshooting

### Services Not Starting

```bash
# Check service status
make status

# Check logs
make logs

# Restart services
make restart
```

### Tests Timing Out

```bash
# Increase test timeout
export TEST_TIMEOUT=20m
make test

# Or run directly with Go
go test -v -timeout=20m ./test/e2e/kafka-link/...
```

### Kafka Connection Issues

```bash
# Check Kafka health
make health

# View Kafka logs
make logs-kafka

# Verify Kafka topics
make kafka-topics
```

### Fiso-Link Issues

```bash
# Check Fiso-Link health
curl http://localhost:9090/healthz

# View Fiso-Link logs
make logs-fiso-link

# Check Fiso-Link metrics
curl http://localhost:9090/metrics
```

## Manual Testing

### Using Test Client Container

```bash
# Enter test client container
docker-compose exec test-client sh

# Publish a message with UUID key
curl -X POST http://fiso-link:3500/link/kafka-uuid \
  -H "Content-Type: application/json" \
  -d '{"user_id":"user123","message":"test message"}'

# Publish a message with header key
curl -X POST http://fiso-link:3500/link/kafka-header-key \
  -H "Content-Type: application/json" \
  -H "X-Message-Id: msg-456" \
  -d '{"message":"test with header key"}'

# Publish a message with payload key
curl -X POST http://fiso-link:3500/link/kafka-payload-key \
  -H "Content-Type: application/json" \
  -d '{"user_id":"user789","message":"test with payload key"}'
```

### Consuming Messages

```bash
# List all topics
make kafka-topics

# Consume from a topic
make kafka-consume TOPIC=test-events-uuid

# Or use kafka-console-consumer directly
docker-compose exec kafka kafka-console-consumer \
  --bootstrap-server localhost:9092 \
  --topic test-events-uuid \
  --from-beginning \
  --max-messages 10
```

## Service Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────┐
│ Test Runner │────▶│  Fiso-Link   │────▶│  Kafka  │
│             │     │              │     │         │
│  (Go tests) │     │  :3500/:9090 │     │  :9092  │
└─────────────┘     └──────────────┘     └─────────┘
                            │
                            ▼
                     ┌─────────────┐
                     │ Zookeeper   │
                     │   :2181     │
                     └─────────────┘
```

## Test Output

Successful test run output:

```
=== RUN   TestKafkaUUIDKeyStrategy
    kafka_link_test.go:XXX: Setting up E2E test with Kafka brokers: [kafka:9092], Fiso-Link: http://fiso-link:3500
    kafka_link_test.go:XXX: E2E test setup complete
    kafka_link_test.go:XXX: Successfully published and consumed message with UUID key: 550e8400-e29b-41d4-a716-446655440000
--- PASS: TestKafkaUUIDKeyStrategy (5.23s)
=== RUN   TestKafkaHeaderKeyStrategy
    kafka_link_test.go:XXX: Successfully published and consumed message with header key: msg-123-from-header
--- PASS: TestKafkaHeaderKeyStrategy (4.12s)
...
PASS
ok      github.com/lsm/fiso/test/e2e/kafka-link    65.43s
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: E2E Tests

on: [push, pull_request]

jobs:
  kafka-link-e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Run E2E tests
        run: |
          cd test/e2e/kafka-link
          make ci
```

### GitLab CI Example

```yaml
kafka-link-e2e:
  image: docker:latest
  services:
    - docker:dind
  script:
    - cd test/e2e/kafka-link
    - make ci
```

## Contributing

When adding new tests:

1. Follow the existing test structure
2. Use `SetupE2ETest` and `TeardownE2ETest` for setup/teardown
3. Register topics for cleanup using `RegisterTopicForCleanup`
4. Add appropriate test configuration to `config/fiso-link-config.yaml`
5. Update this README with the new test scenario

## License

See the project root LICENSE file.
