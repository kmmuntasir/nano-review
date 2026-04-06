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
git clone git@github.com:kmmuntasir/nano-review.git
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
MAX_TURNS=30
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

Log rotation: 10MB max file size, 7-day retention, 3 compressed backups.

## Troubleshooting

```bash
# Restart the service
docker compose -f docker-compose.yml -f docker-compose.staging.yml restart

# Rebuild after code changes
docker compose -f docker-compose.yml -f docker-compose.staging.yml up --build -d

# Full teardown
docker compose -f docker-compose.yml -f docker-compose.staging.yml down -v
```
