# Makefile for LLM-Based Intelligent Autoscaling System
# Common development, build, and deployment tasks

# Variables
APP_NAME := llm-assisted-predictive-autoscaling
BINARY := llm-autoscaler
VERSION := 0.1.0
GO_FLAGS := -ldflags="-s -w -X main.version=$(VERSION)"
DOCKER_IMAGE := $(APP_NAME):$(VERSION)
DOCKER_LATEST := $(APP_NAME):latest

# Default target
.PHONY: all
all: help

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  generate       - Regenerate protos, deepcopy, CRD, and RBAC manifests"
	@echo "  build          - Build the binary"
	@echo "  docker         - Build Docker images (Go + Python)"
	@echo "  kind-up        - Create kind cluster, deploy control plane + demo"
	@echo "  kind-down      - Delete the kind cluster"
	@echo "  test           - Run all tests (Go + Python)"
	@echo "  test-go        - Run Go unit tests"
	@echo "  test-python    - Run Python unit tests"
	@echo "  lint           - Run linters"
	@echo "  fmt            - Format code"
	@echo "  clean          - Remove build artifacts"
	@echo "  version        - Show version information"

# Code generation: protos (Go+Python), deepcopy, CRD, RBAC
.PHONY: generate
generate:
	@echo "Generating code..."
	./scripts/generate_proto.sh
	go mod tidy
	controller-gen object paths=./operator/api/...
	controller-gen crd:allowDangerousTypes=true paths=./operator/... output:crd:artifacts:config=deploy/crd
	controller-gen rbac:roleName=llm-autoscaler paths={./operator/...,./data_acquisition/...} output:rbac:artifacts:config=deploy/rbac

# Build targets
.PHONY: build
build:
	@echo "Building $(BINARY) version $(VERSION)..."
	go build $(GO_FLAGS) -o $(BINARY) .

# Docker targets
.PHONY: docker
docker:
	@echo "Building Docker images..."
	docker build -t llm-autoscaler:latest .
	docker build -f Dockerfile.python -t multi-agent-reasoning:latest .

# kind development cluster
.PHONY: kind-up
kind-up:
	./deploy/dev/kind-up.sh

.PHONY: kind-down
kind-down:
	kind delete cluster --name llm-autoscaler || true

# Test targets
.PHONY: test
test: test-go test-python

.PHONY: test-go
test-go:
	go test ./...

.PHONY: test-python
test-python:
	.venv/bin/python -m pytest tests/ -q

# Run the test suite inside the NixOS test VM (vm.nix).
.PHONY: test-vm
test-vm:
	./deploy/dev/vm-test.sh

.PHONY: test-vm-kind
test-vm-kind:
	./deploy/dev/vm-test.sh --kind

.PHONY: docker
docker:
	@echo "Building Docker image $(DOCKER_IMAGE)..."
	docker build -t $(DOCKER_IMAGE) .
	docker tag $(DOCKER_IMAGE) $(DOCKER_LATEST)

# Code quality targets
.PHONY: lint
lint:
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Installing..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
		golangci-lint run ./...; \
	fi

.PHONY: fmt
fmt:
	@echo "Formatting code..."
	gofmt -w .

# Clean targets
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	rm -f $(BINARY)
	go clean -cache

.PHONY: distclean
distclean: clean
	@echo "Cleaning everything..."
	go clean -modcache
	rm -rf vendor/
	docker system prune -f

# Version target
.PHONY: version
version:
	@echo "Application: $(APP_NAME)"
	@echo "Version: $(VERSION)"
	@echo "Git commit: $$(git rev-parse --short HEAD 2>/dev/null || echo \"unknown\")"
	@echo "Built: $$(date -u +%Y-%m-%dT%H:%M:%SZ)"