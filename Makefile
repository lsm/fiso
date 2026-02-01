.PHONY: build test lint clean docker

BINARY := bin/fiso-flow
MODULE := github.com/lsm/fiso

build:
	go build -o $(BINARY) ./cmd/fiso-flow

test:
	go test -race -coverprofile=coverage.out ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ coverage.out

docker:
	docker build -t fiso-flow:latest .

.DEFAULT_GOAL := build
