.PHONY: build build-link build-operator build-cli build-all test test-integration e2e-operator lint clean coverage-check fmt-check mod-check vulncheck checks docker docker-flow docker-link docker-operator docker-all compose-up compose-down build-wasmer build-wasmer-all build-pure build-wasmer-link build-wasmer-aio

MODULE := github.com/lsm/fiso
IMAGE_REPO ?= ghcr.io/lsm
IMAGE_TAG  ?= latest

# Standard builds (wazero only, no CGO)
build:
	CGO_ENABLED=0 go build -o tmp/fiso-flow ./cmd/fiso-flow

build-link:
	CGO_ENABLED=0 go build -o tmp/fiso-link ./cmd/fiso-link

build-operator:
	CGO_ENABLED=0 go build -o tmp/fiso-operator ./cmd/fiso-operator

build-cli:
	CGO_ENABLED=0 go build -o tmp/fiso ./cmd/fiso

build-all: build build-link build-operator build-cli

# Pure Go builds (no CGO, wazero only)
build-pure: build-all

# Wasmer-enabled builds (requires CGO)
# Standalone Wasmer app runner
build-wasmer:
	CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-wasmer ./cmd/fiso-wasmer

# Flow + Wasmer combined
build-flow-wasmer:
	CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-flow-wasmer ./cmd/fiso-flow-wasmer

# Wasmer + Link combined
build-wasmer-link:
	CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-wasmer-link ./cmd/fiso-wasmer-link

# All-in-one: Flow + Wasmer + Link
build-wasmer-aio:
	CGO_ENABLED=1 go build -tags wasmer -o tmp/fiso-wasmer-aio ./cmd/fiso-wasmer-aio

build-wasmer-all: build-wasmer build-flow-wasmer build-wasmer-link build-wasmer-aio

test:
	go test -race -coverprofile=coverage.out -covermode=atomic $$(go list ./... | grep -v /cmd/ | grep -v /test/e2e/)

test-integration:
	go test -tags integration -race -timeout 120s ./test/integration/...

coverage-check: test
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | tr -d '%'); \
	echo "Total coverage: $${COVERAGE}%"; \
	if [ $$(echo "$${COVERAGE} < 95" | bc) -eq 1 ]; then \
		echo "FAIL: Coverage $${COVERAGE}% is below 95% threshold"; \
		exit 1; \
	fi

lint:
	golangci-lint run ./...

fmt-check:
	@UNFORMATTED=$$(gofmt -l .); \
	if [ -n "$${UNFORMATTED}" ]; then \
		echo "FAIL: The following files are not gofmt formatted:"; \
		echo "$${UNFORMATTED}"; \
		exit 1; \
	fi

mod-check:
	go mod tidy
	@if ! git diff --exit-code go.mod go.sum > /dev/null 2>&1; then \
		echo "FAIL: go.mod/go.sum are not tidy. Run 'go mod tidy' and commit."; \
		exit 1; \
	fi
	go mod verify

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

e2e-operator:
	bash test/e2e/operator/test.sh

checks: fmt-check mod-check vulncheck

docker: docker-flow

docker-flow:
	docker build -f Dockerfile.flow -t $(IMAGE_REPO)/fiso-flow:$(IMAGE_TAG) .

docker-link:
	docker build -f Dockerfile.link -t $(IMAGE_REPO)/fiso-link:$(IMAGE_TAG) .

docker-operator:
	docker build -f Dockerfile.operator -t $(IMAGE_REPO)/fiso-operator:$(IMAGE_TAG) .

docker-all: docker-flow docker-link docker-operator

compose-up:
	docker compose up -d

compose-down:
	docker compose down

clean:
	rm -rf tmp/ dist/ coverage.out

.DEFAULT_GOAL := build
