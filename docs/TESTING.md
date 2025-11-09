# Testing Guide for stackman

## Overview

stackman has two types of tests:
1. **Unit tests** - Fast tests that don't require Docker
2. **Integration tests** - Real-world tests that require Docker Swarm

## Prerequisites

### For Unit Tests
- Go 1.24+

### For Integration Tests
- Go 1.24+
- Docker Engine
- Docker Swarm initialized

## Running Tests

### Quick Start

```bash
# Run unit tests only (fast)
make test-unit

# Run integration tests (requires Docker Swarm)
make test-integration

# Initialize Swarm and run integration tests
make test-integration-full
```

### Manual Testing

#### Unit Tests
```bash
# All unit tests
go test ./internal/... ./cmd/...

# Specific package
go test ./internal/paths/... -v

# With coverage
go test ./internal/... -cover
```

#### Integration Tests

**Step 1: Initialize Docker Swarm**
```bash
docker swarm init
```

**Step 2: Run integration tests**
```bash
go test -v -tags=integration -timeout=5m ./...
```

**Step 3: Cleanup (optional)**
```bash
# Remove test stacks
docker stack ls | grep stackman-test | awk '{print $1}' | xargs docker stack rm

# Leave swarm
docker swarm leave --force
```

## Integration Test Details

### What is tested

1. **Full Deployment Cycle**
   - Initial deployment of a stack
   - Service health verification
   - Update deployment (scale replicas)
   - Cleanup

2. **Path Resolution**
   - Volume mount path conversion
   - STACKMAN_WORKDIR support
   - Relative and absolute paths

### Test Stack Components

The test uses a simple stack with:
- **nginx** service (with healthcheck)
- **redis** service (with healthcheck)
- **overlay networks** (frontend, backend)
- **volumes** (redis-data)

### Test Duration

- Unit tests: ~2 seconds
- Integration tests: ~3-5 minutes (includes deployment and health checks)

## Troubleshooting

### Integration tests fail with "Docker is not available"
```bash
# Check Docker is running
docker ps

# Check Swarm is initialized
docker info | grep Swarm
```

### Integration tests timeout
```bash
# Increase timeout
go test -v -tags=integration -timeout=10m ./...

# Check Docker resources
docker system df
```

### Cleanup hanging test stacks
```bash
# List stacks
docker stack ls

# Remove specific stack
docker stack rm stackman-test

# Force remove all services
docker service ls | grep stackman-test | awk '{print $1}' | xargs docker service rm
```

### Leave Swarm if stuck
```bash
docker swarm leave --force
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Tests

on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.24'
      - run: make test-unit

  integration-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.24'
      - run: docker swarm init
      - run: make test-integration
      - run: docker swarm leave --force
```

## Writing New Tests

### Unit Test Template

```go
package mypackage

import "testing"

func TestMyFunction(t *testing.T) {
    result := MyFunction("input")
    expected := "output"

    if result != expected {
        t.Errorf("Expected %q, got %q", expected, result)
    }
}
```

### Integration Test Template

```go
//go:build integration
// +build integration

package main

import (
    "testing"
    "os/exec"
)

func TestIntegration_MyFeature(t *testing.T) {
    if !isDockerAvailable(t) {
        t.Skip("Docker not available")
    }

    cmd := exec.Command("./stackman", "apply", "-n", "test", "-f", "test.yml")
    if err := cmd.Run(); err != nil {
        t.Fatalf("Command failed: %v", err)
    }
}
```

## Best Practices

1. **Always cleanup** - Use `defer cleanup()` in integration tests
2. **Check prerequisites** - Skip tests if Docker/Swarm not available
3. **Use timeouts** - Set reasonable timeouts for integration tests
4. **Unique stack names** - Avoid conflicts between concurrent tests
5. **Verify state** - Check actual Docker state, not just exit codes

## Coverage

```bash
# Generate coverage report
go test ./... -coverprofile=coverage.out

# View coverage in browser
go tool cover -html=coverage.out
```
