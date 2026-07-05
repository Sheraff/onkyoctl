ARDUINO_FQBN ?= arduino:avr:nano

.PHONY: test build check test-go build-go build-controller fmt vet

test: test-go

build: build-go build-controller

check: fmt vet test build-controller

test-go:
	cd service && go test ./...

build-go:
	cd service && go build ./cmd/onkyoctl

fmt:
	cd service && go fmt ./...

vet:
	cd service && go vet ./...

build-controller:
	arduino-cli compile --fqbn $(ARDUINO_FQBN) controller
