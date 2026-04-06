# Development Environment Setup

## Prerequisites

- Docker and Docker Compose (v2+)
- Git

> Claude Code CLI is installed inside the Docker image at build time. No local installation needed.

## Quick Start

### 1. Clone and Start

```bash
git clone git@github.com:kmmuntasir/nano-review.git
cd nano-review
cp .env.example .env
# Edit .env with your values (see below)
docker compose up --build
```

This uses `docker-compose.yml` — the base dev configuration with:
- Multi-stage build: Go builder → Ubuntu runtime with Claude Code CLI, git, curl, openssh-client
- Port mapping (`${PORT:-8080}:8080`)
- `.env` file loaded automatically
- `review-logs` named volume at `/app/logs`
- `restart: unless-stopped` policy

Server starts on `http://localhost:8080`.

### 2. Configure Environment

Edit `.env` with your values:

| Variable | Description | Example |
|---|---|---|
| `WEBHOOK_SECRET` | Secret shared with GitHub Action to authenticate webhooks | `dev-secret-abc123` |
| `ANTHROPIC_AUTH_TOKEN` | Auth token for Claude Code CLI (e.g., Z.AI token) | `your-auth-token` |
| `GITHUB_PAT` | GitHub Personal Access Token (repo scope) | `ghp_...` |
| `PORT` | Server listen port (optional, default `8080`) | `8080` |
| `CLAUDE_CODE_PATH` | Path to Claude Code binary (optional, auto-detected) | `/usr/local/bin/claude` |
| `MAX_TURNS` | Max Claude Code turns per review (optional, default `30`) | `30` |
| `ANTHROPIC_BASE_URL` | Custom API endpoint (optional) | `https://api.z.ai/api/anthropic` |
| `API_TIMEOUT_MS` | API timeout in milliseconds (optional) | `3000000` |
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | Override haiku model name (optional) | `claude-3-5-haiku-20241022` |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | Override sonnet model name (optional) | `claude-sonnet-4-20250514` |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | Override opus model name (optional) | `claude-opus-4-20250514` |
| `CLAUDE_CODE_DISABLE_1M_CONTEXT` | Disable 1M context window (optional) | `1` |

## Testing

```bash
# Run all tests with race detector
go test -race ./...

# Run tests for a specific package
go test -race ./internal/api/
go test -race ./internal/reviewer/

# Verbose with coverage
go test -v -cover ./...

# Generate HTML coverage report
go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out
```

## Linting

```bash
go vet ./...
go fmt ./...
```

## Manual API Test

Trigger a review request with `curl`:

```bash
curl -X POST http://localhost:8080/review \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Secret: dev-secret-abc123" \
  -d '{
    "repo_url": "git@github.com:owner/repo.git",
    "pr_number": 42,
    "base_branch": "main",
    "head_branch": "feature/test-pr"
  }'
```

Expected response:

```json
{"status":"accepted","run_id":"550e8400-e29b-41d4-a716-446655440000"}
```

## API Reference

### `POST /review`

Initiates an asynchronous PR review.

**Headers:**
- `X-Webhook-Secret` — required, must match server `WEBHOOK_SECRET`
- `Content-Type` — `application/json`

**Request Body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `repo_url` | string | yes | SSH clone URL of the repo |
| `pr_number` | int | yes | PR number to review |
| `base_branch` | string | yes | Base branch of the PR |
| `head_branch` | string | yes | Head branch of the PR |

**Responses:**

| Status | Body | Description |
|---|---|---|
| `200` | `{"status":"accepted","run_id":"<uuid>"}` | Review accepted, processing started |
| `400` | `{"error":"..."}` | Invalid JSON or missing required fields |
| `401` | `{"error":"invalid or missing webhook secret"}` | Missing or wrong webhook secret |
| `405` | `{"error":"method not allowed"}` | Non-POST request |
| `500` | `{"error":"internal server error"}` | Server-side failure |

## Logs

- **Console output**: structured text logs to stdout
- **File output**: rotated JSON logs at `/app/logs/review.log` (Docker) or wherever the logger is configured
- **Rotation**: 10MB max file size, 7-day retention, 3 compressed backups

## Project Structure

```
cmd/server/main.go           # Entry point — env validation, dependency wiring, graceful shutdown
internal/api/
  handler.go                 # POST /review HTTP handler
  models.go                  # Request/response types and validation
  errors.go                  # Sentinel errors
internal/reviewer/
  worker.go                  # Clone repo, run Claude CLI, cleanup
  logger.go                  # Structured multi-writer logger (stdout + file)
config/.claude/
  settings.json              # Claude Code MCP server config (GitHub Copilot)
  skills/pr-review/SKILL.md  # Claude Code skill definition for PR reviews
.github/workflows/
  review.yml                 # GitHub Action that triggers Nano Review on PRs
```
