.PHONY: build test lint fmt install

build:
	go build -o .bin/agent-to-nvim .

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	go fmt ./...

install:
	go install .
