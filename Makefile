ARDUINO_FQBN ?= arduino:avr:nano
ARDUINO_OUTPUT_DIR ?= dist/controller
GO_OUTPUT_DIR ?= dist/service

.PHONY: test build check test-go build-go build-controller fmt vet

test: test-go

build: build-go build-controller

check: fmt vet test build-controller

test-go:
	cd service && go test ./...

build-go:
	mkdir -p $(GO_OUTPUT_DIR)
	cd service && go build -o ../$(GO_OUTPUT_DIR)/onkyoctl ./cmd/onkyoctl

fmt:
	cd service && go fmt ./...

vet:
	cd service && go vet ./...

build-controller:
	arduino-cli compile --fqbn $(ARDUINO_FQBN) --output-dir $(ARDUINO_OUTPUT_DIR) controller
