# docker-stackwait

`docker-stackwait` is a production-ready CLI utility written in Go that provides **zero-downtime deployments** for Docker Swarm stacks with intelligent health monitoring and automatic rollback capabilities.

---

## The Problem It Solves

When deploying services to Docker Swarm, `docker stack deploy` returns immediately after submitting the deployment request, without waiting for services to actually start or become healthy. This creates several critical problems in production environments:

1. **No deployment validation** - You don't know if your deployment succeeded or failed
2. **Broken services go unnoticed** - Failed health checks or crashed containers are only discovered later
3. **Manual rollback required** - When deployments fail, you must manually identify and revert to previous versions
4. **CI/CD pipeline issues** - Pipelines report success even when services are broken
5. **Downtime risk** - No automated way to prevent bad deployments from reaching production

`docker-stackwait` solves these problems by:
- **Waiting** for all services to deploy and pass health checks
- **Monitoring** real-time service status, container health, and task failures
- **Validating** that all containers become healthy before considering deployment successful
- **Rolling back automatically** if health checks fail or services don't start
- **Providing visibility** with real-time logs and event streaming during deployment

---

## How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│ 1. SNAPSHOT: Capture current service state for rollback        │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│ 2. DEPLOY: Execute full stack deployment                       │
│    • Parse compose file                                         │
│    • Remove obsolete services                                   │
│    • Pull Docker images                                         │
│    • Create networks and volumes                                │
│    • Deploy/update services                                     │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│ 3. MONITOR: Real-time streaming and health monitoring          │
│    • Stream service logs                                        │
│    • Stream Docker events                                       │
│    • Monitor container health status                            │
│    • Track failed tasks                                         │
└─────────────────────────────────────────────────────────────────┘
                              ↓
                    ┌─────────────────┐
                    │ All healthy?    │
                    └─────────────────┘
                       ↙           ↘
                    YES             NO
                     ↓               ↓
            ┌──────────────┐  ┌──────────────────┐
            │ SUCCESS      │  │ ROLLBACK         │
            │ Exit 0       │  │ Restore snapshot │
            └──────────────┘  │ Exit 1           │
                              └──────────────────┘
```

### Deployment Process

1. **Snapshot Creation** - Captures current service configurations and task states
2. **Service Cleanup** - Removes services no longer in compose file
3. **Image Pull** - Pulls all required Docker images with progress tracking
4. **Network Setup** - Creates or updates Docker networks with proper configuration
5. **Volume Creation** - Sets up named volumes
6. **Service Deployment** - Creates new or updates existing services
7. **Task Monitoring** - Tracks service tasks and detects recreation
8. **Health Checks** - Continuously monitors container health status
9. **Rollback on Failure** - Automatically restores previous service versions if deployment fails

---

## Key Features

### Production-Ready Deployment
- **Intelligent service updates** - Detects whether services actually changed and need recreation
- **Task tracking** - Monitors old tasks shutdown and new tasks startup
- **Progress reporting** - Real-time visibility into deployment status
- **Graceful shutdown** - Handles interruption signals (SIGINT/SIGTERM) with automatic rollback

### Health Monitoring
- **Container health checks** - Waits for all containers to report "healthy" status
- **Service readiness** - Ensures all service tasks are running before health checks
- **Failed task detection** - Tracks and reports failed tasks across services
- **Configurable thresholds** - Set maximum failed tasks before deployment fails
- **Timeout protection** - Prevents hanging on unresponsive services

### Automatic Rollback
- **State snapshots** - Captures complete service state before deployment
- **Smart rollback** - Restores previous service specs on failure
- **Interrupt handling** - Rollback on Ctrl+C or other signals
- **Version preservation** - Maintains previous image versions and configurations

### Real-Time Visibility
- **Log streaming** - Live logs from all services during deployment
- **Event streaming** - Docker events for containers, services, and nodes
- **Health log streaming** - Dedicated healthcheck execution logs with pass/fail status
- **Status reporting** - Periodic updates on task states and container health

### Enterprise Features
- **CI/CD integration** - Proper exit codes and logging for automated pipelines
- **Environment configuration** - Support for environment variables
- **Flexible timeouts** - Configurable health check and deployment timeouts
- **Multi-service support** - Handles complex stacks with many interdependent services

---

## Installation

### Download Pre-built Binary

```bash
wget https://github.com/SomeBlackMagic/docker-stackwait/releases/latest/download/docker-stackwait-amd64-linux
chmod +x docker-stackwait-amd64-linux
mv docker-stackwait-amd64-linux /usr/local/bin/docker-stackwait
```

### Build from Source

```bash
git clone https://github.com/SomeBlackMagic/docker-stackwait.git
cd docker-stackwait
go build -o docker-stackwait .
```

---

## Usage

### Basic Usage

```bash
docker-stackwait <stack-name> <compose-file> [health-timeout-minutes] [max-failed-tasks]
```

### Examples

**Basic deployment:**
```bash
docker-stackwait mystack docker-compose.yml
```

**With custom health timeout (10 minutes):**
```bash
docker-stackwait mystack docker-compose.yml 10
```

**With custom timeout and max failed tasks:**
```bash
docker-stackwait mystack docker-compose.yml 10 5
```

**Using environment variables:**
```bash
HEALTH_TIMEOUT_MINUTES=15 MAX_FAILED_TASKS=5 docker-stackwait mystack docker-compose.yml
```

---

## Configuration

### Command-line Arguments

| Position | Argument              | Description                                           | Default | Required |
|----------|-----------------------|-------------------------------------------------------|---------|----------|
| 1        | `<stack-name>`        | Name of the Docker Swarm stack                        | -       | Yes      |
| 2        | `<compose-file>`      | Path to docker-compose.yml                            | -       | Yes      |
| 3        | `[health-timeout]`    | Health check timeout in minutes                       | 1       | No       |
| 4        | `[max-failed-tasks]`  | Maximum failed tasks before rollback                  | 3       | No       |

### Environment Variables

Environment variables take precedence over command-line arguments:

| Variable                 | Description                                    | Default |
|--------------------------|------------------------------------------------|---------|
| `HEALTH_TIMEOUT_MINUTES` | Health check timeout in minutes                | 1       |
| `MAX_FAILED_TASKS`       | Maximum number of failed tasks before rollback | 3       |

---

## Exit Codes

| Code | Meaning                                                     |
|------|-------------------------------------------------------------|
| `0`  | Deployment completed successfully, all services healthy     |
| `1`  | Deployment failed, rollback performed                       |
| `130`| Deployment interrupted by user (SIGINT), rollback performed |

---

## Requirements

### System Requirements
- **Docker Engine** with Swarm mode enabled (`docker swarm init`)
- **Go 1.21+** (only for building from source)
- **Linux/macOS/Windows** - Cross-platform support

### Docker Compose File Requirements
- Services **must have health checks** defined for monitoring to work
- Supports Docker Compose file format version 3.x
- Stack must be deployed to Docker Swarm (not standalone Docker)

### Minimal Health Check Example

```yaml
version: '3.8'

services:
  web:
    image: nginx:latest
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost"]
      interval: 10s
      timeout: 2s
      retries: 3
      start_period: 30s
    deploy:
      replicas: 2
      update_config:
        parallelism: 1
        delay: 10s
        failure_action: rollback
```

---

## Output Examples

### Successful Deployment

```
2025/11/06 14:33:05 Start Docker Stack Wait version=1.0.0 revision=abc123
2025/11/06 14:33:05 Parsing compose file: docker-compose.yml
2025/11/06 14:33:05 Creating snapshot of current stack state...
2025/11/06 14:33:06 Snapshotted service: mystack_web (version 42)
2025/11/06 14:33:06 Snapshotted service: mystack_api (version 38)
2025/11/06 14:33:06 Snapshot created with 2 services
2025/11/06 14:33:06 Starting deployment of stack: mystack
2025/11/06 14:33:06 No obsolete services to remove
2025/11/06 14:33:06 Pulling image: nginx:latest
2025/11/06 14:33:08 Image nginx:latest pulled successfully
2025/11/06 14:33:08 Network mystack_default already exists
2025/11/06 14:33:08 Updating service: mystack_web
2025/11/06 14:33:09 Service mystack_web updated, waiting for tasks to be recreated...
[event:service:mystack_web] update
[event:container:mystack_web.1.xyz] start
Stack deployed successfully. Starting health checks...
2025/11/06 14:33:12 Starting log streaming for 2 services...
Waiting for service tasks to start...
2025/11/06 14:33:15 Waiting for services to become healthy...
2025/11/06 14:33:20 Container statuses: mystack_web.1: running/starting, mystack_api.1: running/healthy
[event:container:mystack_web.1.xyz] healthcheck passed (exit 0): curl -f http://localhost
2025/11/06 14:33:25 Container statuses: mystack_web.1: running/healthy, mystack_api.1: running/healthy
2025/11/06 14:33:25 All containers are healthy (checked 2 containers)
All containers healthy.
```

### Failed Deployment with Rollback

```
2025/11/06 14:45:10 Start Docker Stack Wait version=1.0.0 revision=abc123
2025/11/06 14:45:10 Parsing compose file: docker-compose.yml
2025/11/06 14:45:10 Creating snapshot of current stack state...
2025/11/06 14:45:11 Starting deployment of stack: mystack
2025/11/06 14:45:11 Pulling image: myapp:broken-version
2025/11/06 14:45:15 Updating service: mystack_api
[event:service:mystack_api] update
[event:container:mystack_api.1.abc] start
[event:container:mystack_api.1.abc] healthcheck failed (exit 1): curl -f http://localhost/health
2025/11/06 14:45:30 ERROR: Service mystack_api task abc123def456 failed with state shutdown (desired: shutdown)
2025/11/06 14:45:30 ERROR: New task abc123def456 failed with state complete (desired: shutdown): task: non-zero exit (1)
  Container exit code: 1
  Task was shutdown and replaced (likely healthcheck failure)
ERROR: Services failed healthcheck or didn't start in time.
Starting rollback to previous state...
2025/11/06 14:45:31 Rolling back stack: mystack
2025/11/06 14:45:31 Rolling back service: mystack_api to version 38
2025/11/06 14:45:32 Service mystack_api rolled back successfully
2025/11/06 14:45:32 Rollback completed for stack: mystack
Rollback completed successfully
```

### Interrupted Deployment

```
2025/11/06 14:50:15 Start Docker Stack Wait version=1.0.0 revision=abc123
2025/11/06 14:50:15 Deploying stack: mystack
[event:service:mystack_web] update
^C
2025/11/06 14:50:20 Received signal: interrupt
2025/11/06 14:50:20 Deployment interrupted, initiating rollback...
Starting rollback to previous state...
2025/11/06 14:50:21 Rolling back stack: mystack
2025/11/06 14:50:22 Service mystack_web rolled back successfully
Rollback completed successfully
```

---

## Project Structure

The application is organized into focused packages:

```
docker-stackwait/
├── main.go              # Entry point, CLI argument parsing, orchestration
├── compose/             # Docker Compose file parsing and conversion
│   ├── types.go         # Complete Compose file structure definitions
│   ├── parser.go        # YAML parsing logic
│   └── converter.go     # Compose → Swarm ServiceSpec conversion
├── deployer/            # Stack deployment and management
│   ├── stack.go         # Main deployment orchestration
│   ├── services.go      # Service creation and updates
│   ├── images.go        # Image pulling with progress tracking
│   ├── networks.go      # Network creation and management
│   ├── volumes.go       # Volume creation and management
│   ├── cleanup.go       # Obsolete service removal
│   └── rollback.go      # Snapshot creation and rollback logic
└── monitor/             # Real-time monitoring and health checks
    ├── health.go        # Container health monitoring
    ├── logs.go          # Service log streaming
    ├── events.go        # Docker event streaming
    └── healthlog.go     # Health check log streaming
```

---

## Docker Compose Support

`docker-stackwait` includes a comprehensive Docker Compose parser that converts `docker-compose.yml` files to Docker Swarm service specifications.

### Supported Docker Compose Features

#### Service Configuration
- **Images & Build**: `image`, `build` (context, dockerfile, args, target, cache_from)
- **Commands**: `command`, `entrypoint`
- **Environment**: `environment` (array and map formats), `env_file`
- **Container Settings**: `hostname`, `domainname`, `user`, `working_dir`, `stdin_open`, `tty`, `read_only`, `init`
- **Lifecycle**: `stop_signal`, `stop_grace_period`, `restart`

#### Networking
- **Ports**: Short syntax (`"8080:80"`) and long syntax (with mode and protocol)
- **Networks**: Network attachment with aliases
- **DNS**: `dns`, `dns_search`, `dns_opt`
- **Hosts**: `extra_hosts`, `mac_address`

#### Storage
- **Volumes**: Bind mounts with automatic relative → absolute path conversion
- **Named Volumes**: Volume references from top-level `volumes:` section
- **Tmpfs**: Temporary filesystem mounts

#### Health Checks
- **Test Commands**: CMD-SHELL and exec array formats
- **Timing**: `interval`, `timeout`, `retries`, `start_period`
- **Control**: `disable` flag

#### Deployment (Swarm-specific)
- **Mode**: `replicated` (with replica count) or `global`
- **Updates**: Parallelism, delay, order, failure action, monitor period, max failure ratio
- **Rollback**: Same configuration as updates
- **Resources**: CPU and memory limits/reservations
- **Restart Policy**: Condition, delay, max attempts, window
- **Placement**: Node constraints, spread preferences, max replicas per node

#### Security & Capabilities
- **Capabilities**: `cap_add`, `cap_drop`
- **Devices**: Device mappings
- **Isolation**: Container isolation technology

#### Top-Level Sections
- **Services**: Complete service definitions
- **Networks**: Custom networks with driver options, IPAM config
- **Volumes**: Named volumes with driver options
- **Secrets**: File or external secrets (parsed, creation not implemented)
- **Configs**: File or external configs (parsed, creation not implemented)

### Known Limitations

Some Docker Compose fields are **parsed but not applied** due to Docker Swarm API restrictions:

| Field | Reason |
|-------|--------|
| `privileged` | Not supported in Swarm mode |
| `security_opt` | Not available in Swarm ContainerSpec |
| `sysctls` | Not available in Swarm ContainerSpec |
| `ulimits` | Not available in Swarm ContainerSpec |
| `links`, `external_links` | Deprecated in favor of networks |
| `depends_on` | No start order control in Swarm |

These fields remain in the type definitions for completeness and potential future use.

---

## Use Cases

### CI/CD Pipelines

```yaml
# GitLab CI example
deploy:
  stage: deploy
  script:
    - docker-stackwait production docker-compose.yml 10 5
  only:
    - main
```

### Blue-Green Deployments

```bash
# Deploy to green environment
docker-stackwait green-stack docker-compose.yml

# If successful, switch traffic and deploy to blue
docker-stackwait blue-stack docker-compose.yml
```

### Canary Deployments

```yaml
# docker-compose.yml
services:
  web:
    image: myapp:${VERSION}
    deploy:
      replicas: 1  # Start with 1 replica
      update_config:
        parallelism: 1
        delay: 30s
```

```bash
# Deploy canary
VERSION=v2.0 docker-stackwait mystack docker-compose.yml

# If healthy, scale up
docker service scale mystack_web=10
```

### Multi-Environment Deployment

```bash
#!/bin/bash
# deploy.sh

ENVIRONMENTS=("dev" "staging" "production")
COMPOSE_FILE="docker-compose.yml"

for ENV in "${ENVIRONMENTS[@]}"; do
  echo "Deploying to $ENV..."
  if docker-stackwait "${ENV}-stack" "$COMPOSE_FILE" 15 3; then
    echo "✓ $ENV deployment successful"
  else
    echo "✗ $ENV deployment failed, stopping"
    exit 1
  fi
done
```

---

## Troubleshooting

### Issue: Deployment times out waiting for health checks

**Solution**: Increase health timeout or adjust health check configuration
```bash
# Increase timeout to 15 minutes
docker-stackwait mystack docker-compose.yml 15

# Or adjust healthcheck in compose file
healthcheck:
  start_period: 60s  # Give container more time to start
  interval: 15s
  timeout: 10s
```

### Issue: Service keeps failing with "task: non-zero exit"

**Solution**: Check service logs and health check command
```bash
# View service logs
docker service logs mystack_servicename

# Test healthcheck manually
docker exec <container-id> curl -f http://localhost/health
```

### Issue: Rollback restores old version but it's also unhealthy

**Cause**: Previous version also had health issues
**Solution**: Fix health checks or deploy a known-good version first
```bash
# Deploy last known good version
docker-stackwait mystack docker-compose.good.yml
```

### Issue: "No services found" error

**Cause**: Stack name doesn't match or not using Swarm mode
**Solution**: Verify swarm mode and stack name
```bash
# Initialize swarm if needed
docker swarm init

# List existing stacks
docker stack ls
```

---

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

### Development Setup

```bash
# Clone repository
git clone https://github.com/SomeBlackMagic/docker-stackwait.git
cd docker-stackwait

# Install dependencies
go mod download

# Run tests
go test ./...

# Build
go build -o docker-stackwait .
```

---

## License

Licensed under the **GPL-3.0** license.
See [LICENSE](LICENSE) for details.

---

## Credits

Developed by [SomeBlackMagic](https://github.com/SomeBlackMagic)

Built with:
- [Docker Engine API](https://docs.docker.com/engine/api/)
- [Go](https://golang.org/)
- [gopkg.in/yaml.v3](https://github.com/go-yaml/yaml) for YAML parsing
