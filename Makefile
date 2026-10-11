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
	@echo "  build          - Build the binary"
	@echo "  docker         - Build Docker image"
	@echo "  run            - Run the application locally"
	@echo "  docker-run     - Run in Docker container"
	@echo "  compose-up     - Start development environment with docker-compose"
	@echo "  compose-down   - Stop development environment"
	@echo "  test           - Run all tests"
	@echo "  test-unit      - Run unit tests"
	@echo "  test-integration - Run integration tests"
	@echo "  lint           - Run linters"
	@echo "  fmt            - Format code"
	@echo "  clean          - Remove build artifacts"
	@echo "  distclean      - Clean everything including dependencies"
	@echo "  version        - Show version information"

# Build targets
.PHONY: build
build:
	@echo "Building $(BINARY) version $(VERSION)..."
	go build $(GO_FLAGS) -o $(BINARY) main.go

.PHONY: docker
docker:
	@echo "Building Docker image $(DOCKER_IMAGE)..."
	docker build -t $(DOCKER_IMAGE) .
	docker tag $(DOCKER_IMAGE) $(DOCKER_LATEST)

# Run targets
.PHONY: run
run: build
	@echo "Running $(BINARY)..."
	./$(BINARY)

.PHONY: docker-run
docker-run: docker
	@echo "Running $(BINARY) in Docker..."
	docker run --rm \
		-p 8080:8080 -p 9090:9090 \
		-e LLM_API_KEY -e REDIS_PASSWORD \
		-v $(PWD)/config.yaml:/etc/llm-autoscaler/config.yaml:ro \
		$(DOCKER_IMAGE)

# Docker Compose targets
.PHONY: compose-up
compose-up:
	@echo "Starting development environment..."
	docker-compose up -d

.PHONY: compose-down
compose-down:
	@echo "Stopping development environment..."
	docker-compose down

# Testing targets
.PHONY: test
test: test-unit test-integration

.PHONY: test-unit
test-unit:
	@echo "Running unit tests..."
	go test ./... -short -v

.PHONY: test-integration
test-integration:
	@echo "Running integration tests..."
	go test ./... -tags=integration -v

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
	go fmt ./ PDA
	@echo "Running goimports..."
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -w .; \
	else \
		echo "goimports not installed. Installing..."; \
		go install golang.org/x/tools/cmd/goimports@latest; \
		goimports -w .; \
	fi

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