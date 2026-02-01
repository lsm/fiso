.PHONY: build build-link build-operator build-all test lint clean docker coverage-check

MODULE := github.com/lsm/fiso

build:
	go build -o bin/fiso-flow ./cmd/fiso-flow

build-link:
	go build -o bin/fiso-link ./cmd/fiso-link

build-operator:
	go build -o bin/fiso-operator ./cmd/fiso-operator

build-all: build build-link build-operator

test:
	go test -race -coverprofile=coverage.out ./...

coverage-check: test
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | tr -d '%'); \
	echo "Total coverage: $${COVERAGE}%"; \
	if [ $$(echo "$${COVERAGE} < 95" | bc) -eq 1 ]; then \
		echo "FAIL: Coverage $${COVERAGE}% is below 95% threshold"; \
		exit 1; \
	fi

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ coverage.out

docker:
	docker build -t fiso-flow:latest .

.DEFAULT_GOAL := build
