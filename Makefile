BINARY_NAME ?= kamatera-rke2-controller
IMAGE ?= ghcr.io/kamatera/kamatera-rke2-controller:latest

.PHONY: build
build:
	go build -o bin/$(BINARY_NAME) ./cmd/controller

.PHONY: test
test:
	go test -v ./...

.PHONY: docker-build
docker-build:
	docker build -t $(IMAGE) .
