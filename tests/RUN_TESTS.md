# How to Run Integration Tests

## Prerequisites

1. **Docker Swarm must be initialized**
   ```bash
   docker swarm init
   ```

2. **Go 1.21+ installed**

## Running Tests

### Run all integration tests
```bash
go test -v -tags=integration ./tests/ -timeout 30m
```

### Run specific test
```bash
go test -v -tags=integration ./tests/ -run TestIntegration_FullDeploymentCycle
go test -v -tags=integration ./tests/ -run TestIntegration_HealthCheck
go test -v -tags=integration ./tests/ -run TestIntegration_Prune
```

### Run tests by category
```bash
# All health-related tests
go test -v -tags=integration ./tests/ -run TestIntegration_.*Health

# All negative tests
go test -v -tags=integration ./tests/ -run TestIntegration_Negative

# All resource tests
go test -v -tags=integration ./tests/ -run TestIntegration_.*Secrets
go test -v -tags=integration ./tests/ -run TestIntegration_.*Volumes
go test -v -tags=integration ./tests/ -run TestIntegration_.*Networks
```

## Important Notes

- **Integration tag required**: Tests use `//go:build integration` build tag, so `-tags=integration` is mandatory
- **Tests run from tests/ directory**: All paths are relative to the tests directory
- **Binary location**: stackman binary is built to `../stackman` (parent directory)
- **Test data**: Test compose files are created in `testdata/` during test execution
- **Timeout**: Use `-timeout 30m` for full test suite (default 10m may not be enough)
- **Cleanup**: Tests automatically clean up resources with `defer cleanup()`

## Test Structure

Tests are split into logical files:
- `apply_test.go` - Deployment and update tests
- `health_test.go` - Health check and rollback tests
- `resources_test.go` - Secrets, configs, volumes, networks
- `operations_test.go` - Prune, signal handling
- `negative_test.go` - Error scenarios (timeouts, invalid compose, etc.)
- `helpers_test.go` - Common utilities

## Manual Cleanup

If tests are interrupted and leave resources:

```bash
# Remove test stacks
./stackman rm -n stackman-test
./stackman rm -n stackman-health-test
./stackman rm -n stackman-secrets-test

# Or use Docker directly
docker stack rm stackman-test
docker stack rm stackman-health-test
# ... etc
```

## Troubleshooting

### "build constraints exclude all Go files"
- Solution: Use `-tags=integration` flag

### "no such file or directory" for testdata
- Tests must run from project root: `go test -v -tags=integration ./tests/`
- Don't run from within tests/ directory

### "./stackman: no such file or directory"
- First test builds the binary automatically
- Or build manually: `go build -o stackman`

### Tests hang
- Check Docker: `docker info | grep Swarm`
- Increase timeout: `-timeout 45m`
- Clean up manually: `docker stack ls` and remove old stacks
