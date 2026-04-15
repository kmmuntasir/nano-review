# Production Environment Setup

## Overview

Production uses Docker Compose with three overlay layers:

```
docker-compose.yml              # base: build, ports, volumes, env
docker-compose.prod.yml         # overlay: restart always, resource limits, health check, log driver
```

## Prerequisites

- Production server with Docker and Docker Compose v2+
- SSL/TLS termination (reverse proxy: nginx, Caddy, or cloud LB)
- Domain name pointing to the server
- GitHub PAT with minimal `repo` scope (production-scoped, rotated regularly)
- Anthropic auth token (`ANTHROPIC_AUTH_TOKEN`)
- A cryptographically random webhook secret

## Deployment

### 1. Prepare the Server

```bash
git clone https://github.com/kmmuntasir/nano-review.git --depth 1
cd nano-review
```

### 2. Create Environment File

```bash
cp .env.example .env
```

Edit `.env` with production values. Generate a strong webhook secret:

```bash
openssl rand -hex 32
```

```env
WEBHOOK_SECRET=<output-from-openssl-rand>
ANTHROPIC_AUTH_TOKEN=your-prod-auth-token
GITHUB_PAT=ghp_prod_...
PORT=8080
CLAUDE_CODE_PATH=/usr/local/bin/claude
MAX_REVIEW_DURATION=600
MAX_RETRIES=2
```

### 3. Build and Start

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up --build -d
```

Production overlay adds:

| Setting | Value |
|---|---|
| Restart policy | `always` |
| CPU limit | 2 cores |
| Memory limit | 2 GB |
| CPU reservation | 0.5 cores |
| Memory reservation | 512 MB |
| Health check | `curl -f http://localhost:8080/review` every 30s, 3 retries, 5s timeout |
| Docker log driver | `json-file`, 10MB max, 3 files |

### 4. Verify

```bash
# Check container status and health
docker compose ps

# Health check (GET returns 405, which curl -f will reject — verify server is listening)
curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/review
# Expect: 405

# Full API smoke test
curl -X POST http://localhost:8080/review \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Secret: <your-secret>" \
  -d '{
    "repo_url": "https://github.com/kmmuntasir/nano-review.git",
    "pr_number": 1,
    "base_branch": "main",
    "head_branch": "main"
  }'
# Expect: {"status":"accepted","run_id":"<uuid>"}
```

## SSL/TLS Termination

The Nano Review server does not handle TLS. Use a reverse proxy:

### Option A: Caddy (recommended, auto-HTTPS)

```
nano-review.example.com {
    reverse_proxy localhost:8080
}
```

### Option B: Nginx

```nginx
server {
    listen 443 ssl http2;
    server_name nano-review.example.com;

    ssl_certificate     /etc/letsencrypt/live/nano-review.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/nano-review.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### Option C: Cloud Load Balancer

Terminate TLS at the cloud LB (AWS ALB, GCP LB, Cloudflare). Forward to the server on port 8080 over HTTP.

## Configure GitHub Action

In the target repository's GitHub Settings > Secrets and variables > Actions, add:

| Secret | Value | Description |
|---|---|---|
| `NANO_REVIEW_URL` | `https://nano-review.example.com` | Public HTTPS URL behind your reverse proxy |
| `NANO_REVIEW_SECRET` | Same as `WEBHOOK_SECRET` in `.env` | Shared secret for webhook authentication |

Then deploy `.github/workflows/review.yml` in the target repo.

## Production vs Staging Differences

| Feature | Staging | Production |
|---|---|---|
| Restart policy | `on-failure:5` | `always` |
| Memory limit | 4 GB | 2 GB |
| Health check | none | curl every 30s |
| Docker log driver | default | `json-file`, 10MB, 3 files |
| TLS | optional | required (reverse proxy) |

## Security Checklist

- [ ] `WEBHOOK_SECRET` generated with `openssl rand -hex 32` (not a guessable string)
- [ ] `.env` file is not committed to git (already in `.gitignore`)
- [ ] GitHub PAT has only `repo` scope, no admin or org permissions
- [ ] Claude Code runs inside Docker with `--dangerously-skip-permissions` (review [SKILL.md](../../config/.claude/skills/pr-review/SKILL.md) for trust boundary)
- [ ] Temp directories are force-deleted after each review (`defer os.RemoveAll`)
- [ ] Git clone URLs with PAT tokens are never written to disk or logged
- [ ] Reverse proxy enforces TLS 1.2+

## Monitoring

### Application Logs

```bash
# Tail live container logs
docker compose logs -f nano-review

# Inspect rotated review logs inside the container
docker compose exec nano-review cat /app/logs/review.log | tail -100
docker compose exec nano-review ls -la /app/logs/
```

> Note: `docker compose exec` works here because `ls` and `cat` are available in the runtime image. Go commands (build, test, vet) must use `docker compose run --rm` instead — see [dev-setup.md](dev-setup.md) for details.

### Key Log Events to Watch

| Event | Severity | Description |
|---|---|---|
| `server starting` | Info | Server bound to port |
| `review accepted` | Info | Webhook validated, async review started |
| `git clone started` | Info | Repo clone initiated |
| `git clone failed` | Error | Clone failed — check PAT permissions, repo access, network |
| `claude execution failed` | Error | Claude CLI error — check API key, rate limits |
| `review completed` | Info | Full review cycle finished successfully |

### Docker Health Check

The prod overlay includes a built-in health check. Monitor via:

```bash
docker inspect --format='{{.State.Health.Status}}' $(docker compose ps -q nano-review)
```

## Operations

### Update / Redeploy

```bash
git pull
docker compose -f docker-compose.yml -f docker-compose.prod.yml up --build -d
```

### Graceful Restart

The server handles `SIGINT`/`SIGTERM` with a 10-second graceful shutdown:

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml restart
```

### Full Teardown

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml down
# Remove volumes too (deletes log history):
docker compose -f docker-compose.yml -f docker-compose.prod.yml down -v
```

### Credential Rotation

1. Generate new secrets
2. Update `.env` on the server
3. Update GitHub Action secrets in target repo
4. Restart: `docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d`
