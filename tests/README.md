# Tests

This directory contains all test files for the LLM-Based Intelligent Autoscaling System.

## Directory Structure

- `unit/` - Unit tests for individual components
- `integration/` - Integration tests between components
- `e2e/` - End-to-end tests for complete workflows
- `performance/` - Performance and load testing
- `fixtures/` - Test data and mock objects
- `mocks/` - Mock services and dependencies

## Testing Guidelines

1. All new code must include corresponding unit tests
2. Integration tests should verify component interfaces
3. E2E tests should validate complete workflows
4. Performance tests should benchmark against baseline
5. Test files should follow the naming convention: `*_test.py` or `*_test.go`

## Test Frameworks

- Python components: pytest
- Go components: Go testing framework + testify
- Configuration validation: custom schemas
- End-to-end: Kubernetes kind or minikube clusters