.PHONY: build test test-unit test-integration clean help swarm-init swarm-leave

# Build binary
build:
	@echo "Building stackman..."
	@go build -o stackman

# Run all tests (unit only by default)
test: test-unit

# Run unit tests
test-unit:
	@echo "Running unit tests..."
	@go test -v ./internal/... ./cmd/...

# Run integration tests (requires Docker Swarm)
test-integration:
	@echo "Running integration tests..."
	@echo "NOTE: This requires Docker Swarm to be initialized"
	@go test -v -tags=integration -timeout=5m ./...

# Run integration tests with swarm initialization
test-integration-full: swarm-init test-integration

# Initialize Docker Swarm (if not already initialized)
swarm-init:
	@echo "Checking Docker Swarm status..."
	@docker info | grep -q "Swarm: active" || docker swarm init

# Leave Docker Swarm (cleanup)
swarm-leave:
	@echo "Leaving Docker Swarm..."
	@docker swarm leave --force || true

# Clean build artifacts and test stacks
clean:
	@echo "Cleaning..."
	@rm -f stackman
	@docker stack ls | grep stackman-test | awk '{print $$1}' | xargs -r docker stack rm || true

# Show help
help:
	@echo "Available targets:"
	@echo "  build                  - Build stackman binary"
	@echo "  test                   - Run unit tests"
	@echo "  test-unit              - Run unit tests"
	@echo "  test-integration       - Run integration tests (requires Swarm)"
	@echo "  test-integration-full  - Initialize Swarm and run integration tests"
	@echo "  swarm-init             - Initialize Docker Swarm"
	@echo "  swarm-leave            - Leave Docker Swarm"
	@echo "  clean                  - Clean build artifacts and test stacks"
	@echo "  help                   - Show this help message"
