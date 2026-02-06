# Makefile for Kafka Link E2E Tests
# This Makefile provides automation for running end-to-end tests for Kafka target support

# Variables
DOCKER_COMPOSE := docker-compose
GO := go
GO_TEST := $(GO) test
GO_TEST_FLAGS := -v -count=1
TEST_TIMEOUT := 10m

# Environment
KAFKA_BROKERS ?= kafka:9092
FISO_LINK_ADDR ?= http://fiso-link:3500
export KAFKA_BROKERS
export FISO_LINK_ADDR

# Colors for output
COLOR_RESET := \033[0m
COLOR_BOLD := \033[1m
COLOR_GREEN := \033[32m
COLOR_YELLOW := \033[33m
COLOR_BLUE := \033[34m

.PHONY: help
help: ## Show this help message
	@echo "$(COLOR_BOLD)Kafka Link E2E Tests$(COLOR_RESET)"
	@echo ""
	@echo "$(COLOR_BOLD)Available targets:$(COLOR_RESET)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(COLOR_BLUE)%-20s$(COLOR_RESET) %s\n", $$1, $$2}'

.PHONY: setup
setup: ## Set up the test environment
	@echo "$(COLOR_GREEN)Setting up test environment...$(COLOR_RESET)"
	@$(DOCKER_COMPOSE) pull
	@$(DOCKER_COMPOSE) build
	@echo "$(COLOR_GREEN)Setup complete!$(COLOR_RESET)"

.PHONY: start
start: ## Start all services (Kafka, Fiso-Link, test runner)
	@echo "$(COLOR_GREEN)Starting services...$(COLOR_RESET)"
	@$(DOCKER_COMPOSE) up -d kafka zookeeper
	@echo "$(COLOR_YELLOW)Waiting for Kafka to be healthy...$(COLOR_RESET)"
	@timeout 120 sh -c 'until docker-compose exec -T kafka kafka-broker-api-versions --bootstrap-server localhost:9092 >/dev/null 2>&1; do sleep 2; done'
	@$(DOCKER_COMPOSE) up -d fiso-link
	@echo "$(COLOR_YELLOW)Waiting for Fiso-Link to be healthy...$(COLOR_RESET)"
	@timeout 60 sh -c 'until docker-compose exec -T fiso-link wget --quiet --tries=1 --spider http://localhost:9090/healthz >/dev/null 2>&1; do sleep 2; done'
	@$(DOCKER_COMPOSE) up -d test-runner test-client
	@echo "$(COLOR_GREEN)All services started!$(COLOR_RESET)"
	@$(DOCKER_COMPOSE) ps

.PHONY: stop
stop: ## Stop all services
	@echo "$(COLOR_YELLOW)Stopping services...$(COLOR_RESET)"
	@$(DOCKER_COMPOSE) down
	@echo "$(COLOR_GREEN)Services stopped.$(COLOR_RESET)"

.PHONY: restart
restart: stop start ## Restart all services

.PHONY: logs
logs: ## Show logs from all services
	@$(DOCKER_COMPOSE) logs -f

.PHONY: logs-kafka
logs-kafka: ## Show Kafka logs
	@$(DOCKER_COMPOSE) logs -f kafka

.PHONY: logs-fiso-link
logs-fiso-link: ## Show Fiso-Link logs
	@$(DOCKER_COMPOSE) logs -f fiso-link

.PHONY: logs-test-runner
logs-test-runner: ## Show test runner logs
	@$(DOCKER_COMPOSE) logs -f test-runner

.PHONY: status
status: ## Show status of all services
	@$(DOCKER_COMPOSE) ps

.PHONY: health
health: ## Check health of all services
	@echo "$(COLOR_BOLD)Service Health Status:$(COLOR_RESET)"
	@echo "Kafka:"
	@$(DOCKER_COMPOSE) exec -T kafka kafka-broker-api-versions --bootstrap-server localhost:9092 >/dev/null 2>&1 && echo "  $(COLOR_GREEN)✓ Healthy$(COLOR_RESET)" || echo "  $(COLOR_YELLOW)⚠ Unhealthy$(COLOR_RESET)"
	@echo "Fiso-Link:"
	@$(DOCKER_COMPOSE) exec -T fiso-link wget --quiet --tries=1 --spider http://localhost:9090/healthz >/dev/null 2>&1 && echo "  $(COLOR_GREEN)✓ Healthy$(COLOR_RESET)" || echo "  $(COLOR_YELLOW)⚠ Unhealthy$(COLOR_RESET)"

.PHONY: test
test: ## Run all E2E tests
	@echo "$(COLOR_GREEN)Running E2E tests...$(COLOR_RESET)"
	@$(DOCKER_COMPOSE) run --rm test-runner $(GO_TEST) -timeout=$(TEST_TIMEOUT) $(GO_TEST_FLAGS) ./test/e2e/kafka-link/...

.PHONY: test-verbose
test-verbose: ## Run tests with extra verbosity
	@echo "$(COLOR_GREEN)Running E2E tests (verbose)...$(COLOR_RESET)"
	@$(DOCKER_COMPOSE) run --rm test-runner $(GO_TEST) -timeout=$(TEST_TIMEOUT) $(GO_TEST_FLAGS) -v ./test/e2e/kafka-link/...

.PHONY: test-race
test-race: ## Run tests with race detection
	@echo "$(COLOR_GREEN)Running E2E tests with race detection...$(COLOR_RESET)"
	@$(DOCKER_COMPOSE) run --rm test-runner $(GO_TEST) -timeout=$(TEST_TIMEOUT) $(GO_TEST_FLAGS) -race ./test/e2e/kafka-link/...

.PHONY: test-short
test-short: ## Run short tests only (skip E2E)
	@echo "$(COLOR_GREEN)Running short tests...$(COLOR_RESET)"
	@$(GO_TEST) -short $(GO_TEST_FLAGS) ./test/e2e/kafka-link/...

.PHONY: test-local
test-local: ## Run tests locally (requires running services)
	@echo "$(COLOR_GREEN)Running tests locally...$(COLOR_RESET)"
	@$(GO_TEST) -timeout=$(TEST_TIMEOUT) $(GO_TEST_FLAGS) ./test/e2e/kafka-link/...

.PHONY: test-one
test-one: ## Run a specific test (use TEST=name)
	@echo "$(COLOR_GREEN)Running test: $(TEST)$(COLOR_RESET)"
	@$(DOCKER_COMPOSE) run --rm test-runner $(GO_TEST) -timeout=$(TEST_TIMEOUT) $(GO_TEST_FLAGS) -run "$(TEST)" ./test/e2e/kafka-link/...

.PHONY: test-coverage
test-coverage: ## Run tests with coverage
	@echo "$(COLOR_GREEN)Running tests with coverage...$(COLOR_RESET)"
	@$(DOCKER_COMPOSE) run --rm test-runner sh -c "$(GO_TEST) -timeout=$(TEST_TIMEOUT) -coverprofile=coverage.out -covermode=atomic ./test/e2e/kafka-link/... && $(GO) tool cover -html=coverage.out -o coverage.html"
	@echo "$(COLOR_GREEN)Coverage report generated: coverage.html$(COLOR_RESET)"

.PHONY: clean
clean: ## Clean up test artifacts and containers
	@echo "$(COLOR_YELLOW)Cleaning up...$(COLOR_RESET)"
	@$(DOCKER_COMPOSE) down -v --remove-orphans
	@rm -f coverage.out coverage.html
	@echo "$(COLOR_GREEN)Cleanup complete.$(COLOR_RESET)"

.PHONY: clean-all
clean-all: clean ## Clean up everything including images
	@echo "$(COLOR_YELLOW)Removing images...$(COLOR_RESET)"
	@docker rmi fiso-link:e2e fiso-flow:e2e 2>/dev/null || true
	@echo "$(COLOR_GREEN)Full cleanup complete.$(COLOR_RESET)"

.PHONY: shell
shell: ## Open shell in test runner container
	@$(DOCKER_COMPOSE) run --rm test-runner /bin/sh

.PHONY: shell-kafka
shell-kafka: ## Open shell in Kafka container
	@$(DOCKER_COMPOSE) exec kafka /bin/bash

.PHONY: shell-fiso-link
shell-fiso-link: ## Open shell in Fiso-Link container
	@$(DOCKER_COMPOSE) exec fiso-link /bin/sh

.PHONY: kafka-topics
kafka-topics: ## List all Kafka topics
	@$(DOCKER_COMPOSE) exec kafka kafka-topics --bootstrap-server localhost:9092 --list

.PHONY: kafka-consume
kafka-consume: ## Consume from a Kafka topic (use TOPIC=name)
	@$(DOCKER_COMPOSE) exec kafka kafka-console-consumer --bootstrap-server localhost:9092 --topic $(TOPIC) --from-beginning --max-messages 10

.PHONY: rebuild
rebuild: ## Rebuild all images
	@echo "$(COLOR_YELLOW)Rebuilding images...$(COLOR_RESET)"
	@$(DOCKER_COMPOSE) build --no-cache
	@echo "$(COLOR_GREEN)Rebuild complete.$(COLOR_RESET)"

.PHONY: ci
ci: setup start test ## Run full CI pipeline: setup, start, test

.PHONY: quick-test
quick-test: ## Quick test cycle (start, test, stop)
	@echo "$(COLOR_GREEN)Quick test cycle...$(COLOR_RESET)"
	@$(MAKE) -s start
	@sleep 5
	@$(MAKE) -s test
	@$(MAKE) -s stop

.PHONY: list-targets
list-targets: ## List all configured Fiso-Link targets
	@echo "$(COLOR_BOLD)Configured Fiso-Link Targets:$(COLOR_RESET)"
	@echo "  - kafka-uuid (UUID key strategy)"
	@echo "  - kafka-header-key (Header-based key)"
	@echo "  - kafka-payload-key (Payload-based key)"
	@echo "  - kafka-static-key (Static key)"
	@echo "  - kafka-random-key (Random key)"
	@echo "  - kafka-no-key (No key)"
	@echo "  - kafka-rate-limited (Rate limited: 10 req/s)"
	@echo "  - kafka-cb-test (Circuit breaker test)"
	@echo "  - kafka-retry-test (Retry test)"
	@echo "  - kafka-headers-test (Headers propagation)"

# Default target
.DEFAULT_GOAL := help
