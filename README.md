# docker-stackwait

`docker-stackwait` is a CLI utility written in Go that automates safe deployments of Docker Swarm stacks.  
It runs `docker stack deploy`, streams live events from the Swarm API, waits for all containers in the stack to become **healthy**, and performs an automatic **rollback** if health checks fail.

---

## Features

- Deploys a stack via `docker stack deploy`.
- Streams **real-time service and container events** from the Docker Engine.
- Waits until all containers report `State.Health.Status == "healthy"`.
- Performs **automatic rollback** to the previous image versions if containers fail to reach healthy status.
- Supports configurable timeout and can run in CI/CD environments.

---

## How to install
```
wget https://github.com/SomeBlackMagic/docker-stackwait/releases/latest/download/docker-stackwait-amd64-linux
chmod +x docker-stackwait-amd64-linux
mv docker-stackwait-amd64-linux /usr/local/bin/docker-stackwait
```

### 1. Build from source

```bash
go build -o docker-stackwait .
```


## Usage

### 1. Run

```bash
docker-stackwait <stack-name> <compose-file>
```

Example:

```bash
docker-stackwait mystack docker-compose.yml
```

### 2. Behavior

1. The tool first checks the current image versions of all services in the stack.
2. It runs `docker stack deploy -c <file> <stack>`.
3. It subscribes to the Docker event stream and prints real-time deployment logs:
   ```
   [2025-11-04T13:21:12Z] service mystack_web: update
   [2025-11-04T13:21:14Z] container mystack_web.1: start
   ```
4. It continuously inspects all containers in the stack for `health: healthy`.
5. If the stack becomes fully healthy within the timeout (default 5 minutes), the process exits successfully.
6. If not, it triggers rollback:
   ```
   Rolling back mystack_web to ghcr.io/myapp/web:v1.2.3
   ```

---

## Command-line Arguments

| Argument         | Description                    | Required |
|------------------|--------------------------------|----------|
| `<stack-name>`   | Name of the Docker Swarm stack | Yes      |
| `<compose-file>` | Path to `docker-compose.yml`   | Yes      |

---

## Exit Codes

| Code | Meaning                                             |
|------|-----------------------------------------------------|
| `0`  | Deployment completed and all containers are healthy |
| `1`  | Deployment failed or rollback was performed         |

---

## Requirements

- Docker Engine with Swarm mode enabled.
- Go 1.21 or later (for local builds).
- Health checks defined in your services (`healthcheck:` in `docker-compose.yml`).

Example:

```yaml
services:
  web:
    image: nginx:latest
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost"]
      interval: 10s
      timeout: 2s
      retries: 3
```

---

## Example Output

```
Deploying stack "mystack"...
[2025-11-04T14:33:05Z] service mystack_api: update
[2025-11-04T14:33:07Z] container mystack_api.1: start
Waiting for all containers to become healthy...
All containers are healthy.
```

If health checks fail:

```
Some containers failed to reach healthy state. Rolling back...
Rolling back mystack_api to myapp_api:1.2.0
```

---

## License

Licensed under the **GPL-3.0** license.  
See [LICENSE](LICENSE) for details.
