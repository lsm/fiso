.PHONY: build build-link build-operator build-all test test-integration lint clean coverage-check fmt-check mod-check vulncheck checks docker docker-flow docker-link docker-operator docker-all compose-up compose-down

MODULE := github.com/lsm/fiso
IMAGE_REPO ?= ghcr.io/lsm
IMAGE_TAG  ?= latest

build:
	go build -o bin/fiso-flow ./cmd/fiso-flow

build-link:
	go build -o bin/fiso-link ./cmd/fiso-link

build-operator:
	go build -o bin/fiso-operator ./cmd/fiso-operator

build-all: build build-link build-operator

test:
	go test -race -coverprofile=coverage.out ./...

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
	rm -rf bin/ dist/ coverage.out

.DEFAULT_GOAL := build
