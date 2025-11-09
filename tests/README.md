# Integration Tests

Integration tests for stackman (swarm-helm).

## Running Tests

```bash
# Run all integration tests
go test -v -tags=integration ./tests/

# Run specific test
go test -v -tags=integration ./tests/ -run TestIntegration_FullDeploymentCycle
```

## Requirements

- Docker Engine with Swarm mode enabled
- Go 1.21+
- Access to Docker Socket API

## Swarm Initialization

```bash
docker swarm init
```

## Test Coverage

### Core Features

- **TestIntegration_FullDeploymentCycle** - complete deployment and update cycle
- **TestIntegration_UpdateConfig** - service updates with `UpdateStatus.State` control
- **TestIntegration_HealthCheck** - healthcheck monitoring
- **TestIntegration_UnhealthyRollback** - rollback on unhealthy service

### Resources

- **TestIntegration_SecretsAndConfigs** - secrets and configs management
- **TestIntegration_VolumesAndPaths** - volumes and path resolution
- **TestIntegration_OverlayNetworks** - overlay networks

### Operations

- **TestIntegration_Prune** - --prune functionality
- **TestIntegration_SignalInterrupt** - SIGINT/SIGTERM handling and rollback

### Negative Scenarios

- **TestIntegration_NegativeTimeout** - timeout behavior
- **TestIntegration_NegativeInvalidCompose** - invalid compose handling
- **TestIntegration_NegativeImageLatest** - :latest tag protection

## Structure

```
tests/
├── apply_test.go          # Apply and deployment tests
├── health_test.go         # Health check tests
├── resources_test.go      # Resources (secrets, configs, volumes, networks)
├── operations_test.go     # Operations (prune, signals)
├── negative_test.go       # Negative scenarios
├── helpers_test.go        # Test helpers and utilities
├── testdata/              # Test data
│   ├── simple-stack.yml
│   └── healthcheck-demo.yml
└── README.md              # This file
```

## Timeouts

- Default test timeout: 3 minutes
- Deploy timeout: 2 minutes (can override with `-timeout` flag)
- Rollback timeout: 1 minute (can override with `-rollback-timeout` flag)

## Cleanup

All tests automatically clean up created resources via `defer cleanup()`.
