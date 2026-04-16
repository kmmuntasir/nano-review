# Staging Environment Setup

## Overview

Staging uses Docker Compose with two overlay files applied on top of the base `docker-compose.yml`:

```
docker-compose.yml              # base: build, ports, volumes, env
docker-compose.staging.yml      # overlay: restart policy, resource limits
```

## Prerequisites

- Docker and Docker Compose v2+
- A staging server or VM with outbound internet access
- GitHub PAT with `repo` scope (staging-scoped)
- Anthropic auth token (`ANTHROPIC_AUTH_TOKEN`)
- A webhook secret string

## Deployment

### 1. Prepare the Server

```bash
git clone https://github.com/kmmuntasir/nano-review.git
cd nano-review
```

### 2. Create Environment File

```bash
cp .env.example .env
```

Edit `.env` with staging values:

```env
WEBHOOK_SECRET=staging-webhook-secret-<random-string>
ANTHROPIC_AUTH_TOKEN=your-staging-auth-token
GITHUB_PAT=ghp_staging_...
PORT=8080
CLAUDE_CODE_PATH=/usr/local/bin/claude
MAX_REVIEW_DURATION=600
MAX_RETRIES=2
```

### 3. Build and Start

```bash
docker compose -f docker-compose.yml -f docker-compose.staging.yml up --build -d
```

This applies both the base config and the staging overlay:

| Setting | Value |
|---|---|
| Restart policy | `on-failure:5` (retry up to 5 times, then stop) |
| CPU limit | 2 cores |
| Memory limit | 4 GB |
| CPU reservation | 0.5 cores |
| Memory reservation | 512 MB |
| Port | `${PORT:-8080}` mapped to container 8080 |
| Log volume | `review-logs` at `/app/logs` |
| Data volume | `review-data` at `/app/data` |

### 4. Verify

```bash
# Check container is running
docker compose ps

# Check logs
docker compose logs -f nano-review

# Health check
curl -s http://localhost:8080/review
# Expect: 405 Method Not Allowed (server is up, rejects GET)
```

## Configure GitHub Action

In the target repository's GitHub Settings > Secrets and variables > Actions, add:

| Secret | Value | Description |
|---|---|---|
| `NANO_REVIEW_URL` | `https://your-staging-host:8080` | Public URL of your staging server |
| `NANO_REVIEW_SECRET` | Same as `WEBHOOK_SECRET` in `.env` | Shared secret for webhook auth |

The `.github/workflows/review.yml` file in the target repo sends POST requests to this endpoint.

## Staging vs Dev Differences

| Feature | Dev | Staging |
|---|---|---|
| Restart policy | `unless-stopped` | `on-failure:5` |
| CPU limit | none | 2 cores |
| Memory limit | none | 4 GB |
| Resource reservations | none | 0.5 CPU / 512 MB |
| Health check | none | none |
| Docker log driver | default | default |

## Monitoring Logs

```bash
# Tail live logs
docker compose logs -f nano-review

# Inspect rotated log files
docker compose exec nano-review ls -la /app/logs/
docker compose exec nano-review cat /app/logs/review.log | tail -50
```

> Note: `docker compose exec` works here because `ls` and `cat` are available in the runtime image. Go commands (build, test, vet) must use `docker compose run --rm` instead — see [dev-setup.md](dev-setup.md) for details.

Log rotation: 10MB max file size, 7-day retention, 3 compressed backups.

## Native Staging Deployment

For lighter-weight staging without Docker — useful when you want faster iteration, lower resource usage, or don't need container isolation.

> **Note:** Docker remains the default for most staging deployments. Use native deployment only when container overhead is undesirable.

### Prerequisites

- Go 1.23+ installed on the host
- `git` and `curl` available
- Claude Code CLI installed (`claude` on PATH)

### Steps

```bash
# 1. Clone
git clone https://github.com/kmmuntasir/nano-review.git
cd nano-review

# 2. Set required environment variables
export NANO_DATA_DIR=$HOME/.nano-review/data
export NANO_LOG_DIR=$HOME/.nano-review/logs

# 3. Create directories
mkdir -p "$NANO_DATA_DIR" "$NANO_LOG_DIR"

# 4. One-time native setup (installs Claude Code, configures environment)
make native-setup

# 5. Build
make native-build

# 6. Run
./bin/nano-review
```

Provide the usual required env vars (`WEBHOOK_SECRET`, `ANTHROPIC_AUTH_TOKEN`, `GITHUB_PAT`) via a `.env` file or export them before running. `NANO_DATA_DIR` defaults to `./data` and `NANO_LOG_DIR` defaults to `./logs` if unset.

### Stopping / Restarting

```bash
# Ctrl+C stops the process
# Or use a process manager like systemd (see prod-setup.md for a unit file template)
```

## Troubleshooting

```bash
# Restart the service
docker compose -f docker-compose.yml -f docker-compose.staging.yml restart

# Rebuild after code changes
docker compose -f docker-compose.yml -f docker-compose.staging.yml up --build -d

# Full teardown
docker compose -f docker-compose.yml -f docker-compose.staging.yml down -v
```
