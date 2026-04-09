# PRD: Nano Review - Headless Asynchronous PR Reviewer

## 1. Overview

**Nano Review** is a lightweight micro backend that performs automated pull request reviews using Claude Code CLI in headless mode. When a GitHub Action triggers on a pull request, it calls the Nano Review API, which asynchronously spawns a Claude Code agent to review the PR and post inline comments via the GitHub MCP server.

## 2. Problem Statement

Manual code reviews are a bottleneck. Existing automated tools (linters, static analyzers) catch syntax and style issues but miss logical bugs, security vulnerabilities, and architectural concerns. A human reviewer is still needed for deeper analysis. Nano Review bridges this gap by delegating the first-pass review to Claude Code, providing contextual, inline feedback before a human reviewer even looks at the PR.

## 3. Goals

1. **Fast webhook response** - Return HTTP 200 immediately so GitHub Action runs stay short.
2. **Asynchronous review** - Perform the full PR review in the background and post results to the PR.
3. **Inline comments** - Post review feedback as inline PR comments at specific file/line locations. Fall back to a summary review comment if inline posting fails.
4. **Ephemeral execution** - Each review runs in an isolated temporary directory that is cleaned up after completion.
5. **Docker-native** - The entire service runs in Docker with clear separation of dev/staging/prod environments.

## 4. Non-Goals (Out of MVP Scope)

- Multi-repository support (MVP targets a single pre-configured repository).
- Authentication/authorization beyond webhook secret validation.
- Review history persistence or dashboards.
- Configurable review rules or per-repository customization.
- Notifications (Slack, email, etc.).
- Rate limiting or queue management for concurrent reviews.
- Support for platforms other than GitHub.

## 5. Architecture

```
GitHub PR Event
      |
      v
GitHub Action  ---(HTTP POST)--->  Nano Review API Server (Go)
                                        |
                                        v
                                   Parse payload (repo URL, PR number, branches)
                                        |
                                        v
                                   Clone repo into /tmp/<run-id>
                                        |
                                        v
                                   Spawn Claude Code CLI (headless, bypass permissions)
                                   with /pr-review skill
                                        |
                                        v
                                   Claude Code reviews PR via GitHub MCP server
                                   (reads diff, posts inline comments, submits review)
                                        |
                                        v
                                   Force-delete /tmp/<run-id>
```

### Components

| Component | Responsibility |
|---|---|
| **API Server** (Go) | Receives webhook, validates secret, parses payload, triggers review goroutine, returns HTTP 200 immediately. |
| **Review Worker** (goroutine) | Clones the repo, launches Claude Code CLI in headless mode, waits for completion, cleans up temp directory. |
| **Claude Code CLI** | Executes the `/pr-review` skill which reads the PR diff and posts review feedback via GitHub MCP server. |
| **GitHub MCP Server** | Provides tools for reading PR data and posting review comments inline. |

## 6. API Specification

### `POST /review`

Receives a webhook payload from GitHub Actions and triggers an asynchronous PR review.

**Headers:**

| Header | Required | Description |
|---|---|---|
| `X-Webhook-Secret` | Yes | Shared secret for authenticating the webhook call. |

**Request Body:**

```json
{
  "repo_url": "https://github.com/owner/repo.git",
  "pr_number": 42,
  "base_branch": "main",
  "head_branch": "feature/add-auth"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `repo_url` | string | Yes | HTTPS URL of the GitHub repository. |
| `pr_number` | integer | Yes | Pull request number. |
| `base_branch` | string | Yes | The base (target) branch of the PR. |
| `head_branch` | string | Yes | The head (source) branch of the PR. |

**Response (synchronous):**

| Status | Body | Condition |
|---|---|---|
| 200 | `{"status": "accepted", "run_id": "abc123"}` | Valid request, review queued. |
| 400 | `{"error": "missing required field: pr_number"}` | Invalid payload. |
| 401 | `{"error": "invalid or missing webhook secret"}` | Missing or wrong secret. |
| 500 | `{"error": "internal server error"}` | Unexpected failure. |

**Response (asynchronous - optional future enhancement):** None in MVP. The review outcome is posted directly to the GitHub PR.

## 7. Review Worker Flow

For each incoming review request:

1. **Generate run ID** - Create a unique identifier (UUID).
2. **Create temp directory** - `/tmp/<run-id>`.
3. **Clone repository** - `git clone <repo_url> --branch <head_branch> /tmp/<run-id>`.
4. **Launch Claude Code** - Execute:
   ```bash
   cd /tmp/<run-id> && claude -p "/pr-review <pr_number> --base <base_branch> --head <head_branch>" \
     --dangerously-skip-permissions \
     --output-format json \
     --max-turns 30
   ```
5. **Capture output** - Log stdout/stderr for debugging. The exit code determines success/failure.
6. **Force cleanup** - `rm -rf /tmp/<run-id>` regardless of success or failure.

The worker runs as a goroutine. No concurrency limit in MVP (one goroutine per request).

## 8. Claude Code Configuration

### `.claude/skills/pr-review/SKILL.md`

The `/pr-review` skill that defines how the agent reviews a PR:

```markdown
---
name: pr-review
description: Review a GitHub pull request
argument-hint: "<pr_number> --base <base_branch> --head <head_branch>"
---

You are an expert code reviewer. Review pull request #$1 (base branch: $3, head branch: $5) using the GitHub MCP server tools.

## Steps

1. Use the GitHub MCP server to fetch the PR diff and details.
2. Analyze the changes for:
   - **Correctness**: Logic errors, off-by-one errors, unhandled edge cases.
   - **Security**: Injection vulnerabilities, exposed secrets, insecure defaults.
   - **Performance**: N+1 queries, unnecessary allocations, missing indices.
   - **Maintainability**: Unclear naming, missing error handling, code duplication.
3. For each issue found, add an inline comment to the pending review with the file path, line number, and a clear explanation of the issue and suggested fix.
4. Submit the review:
   - If issues were found: REQUEST_CHANGES with a summary of the key concerns.
   - If no issues were found: COMMENT with a summary of what was reviewed and a positive note.
5. If inline comments fail for any reason, post a single summary review comment as a fallback.

## Rules
- Be concise. Do not comment on style preferences or formatting that linters handle.
- Only flag genuine issues. Do not pad the review with trivial observations.
- Include suggested code fixes in comments where practical.
```

### `.claude/settings.json`

Configures the GitHub MCP server:

```json
{
  "mcpServers": {
    "github": {
      "type": "http",
      "url": "https://api.githubcopilot.com/mcp",
      "headers": {
        "Authorization": "Bearer ${GITHUB_PAT}"
      }
    }
  }
}
```

## 9. GitHub Action Workflow (Not a part of this project, but a requirement)

The repository using Nano Review will include a GitHub Action:

```yaml
name: Nano Review

on:
  pull_request:
    types: [opened, synchronize]

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - name: Trigger Nano Review
        run: |
          curl -X POST "${{ secrets.NANO_REVIEW_URL }}/review" \
            -H "Content-Type: application/json" \
            -H "X-Webhook-Secret: ${{ secrets.NANO_REVIEW_SECRET }}" \
            -d '{
              "repo_url": "${{ github.event.pull_request.head.repo.clone_url }}",
              "pr_number": ${{ github.event.pull_request.number }},
              "base_branch": "${{ github.base_ref }}",
              "head_branch": "${{ github.head_ref }}"
            }'
```

## 10. Docker Setup

### Directory Structure

```
nano-review/
├── .claude                   # Claude settings used for this project
├── Dockerfile
├── docker-compose.yml
├── docker-compose.staging.yml
├── docker-compose.prod.yml
├── .env.example
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── api/
│   │   └── handler.go
│   └── reviewer/
│       └── worker.go
├── config/
|   └── .claude               # Claude settings meant to be copied over during docker build
│       ├── skills/
│       │   └── pr-review/
|       |       └── SKILL.md
│       └── settings.json
└── docs/
    ├── PRD.md
    └── roadmap.md
```

### Dockerfile

```dockerfile
FROM golang:1.23-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /nano-review ./cmd/server

FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    && rm -rf /var/lib/apt/lists/*

# Install Claude Code CLI
RUN curl -fsSL https://claude.ai/install.sh | sh

# Copy binary
COPY --from=builder /nano-review /usr/local/bin/nano-review

# Copy Claude Code configuration
COPY config/.claude/ /root/.claude/

WORKDIR /app
EXPOSE 8080

CMD ["nano-review"]
```

### Environment Variables

| Variable | Required | Description |
|---|---|---|
| `PORT` | No | Server port (default: `8080`). |
| `WEBHOOK_SECRET` | Yes | Shared secret for webhook authentication. |
| `ANTHROPIC_AUTH_TOKEN` | Yes | Auth token for Claude Code CLI. |
| `GITHUB_PAT` | Yes | GitHub Personal Access Token for the MCP server and git clone authentication. |
| `CLAUDE_CODE_PATH` | No | Override path to `claude` binary (default: auto-detected). |
| `MAX_TURNS` | No | Max agentic turns for Claude Code (default: `30`). |
| `ANTHROPIC_BASE_URL` | No | Custom API endpoint (e.g., Z.AI proxy). |
| `API_TIMEOUT_MS` | No | API timeout in milliseconds. |
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | No | Override haiku model name. |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | No | Override sonnet model name. |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | No | Override opus model name. |
| `CLAUDE_CODE_DISABLE_1M_CONTEXT` | No | Disable 1M context window. |

### `.env.example`

```
PORT=8080
WEBHOOK_SECRET=your-webhook-secret-here
ANTHROPIC_AUTH_TOKEN=your-auth-token
GITHUB_PAT=ghp_...
MAX_TURNS=30
```

### Docker Compose Files

- `docker-compose.yml` - Development: mounts source code, hot-reload, exposed debug ports.
- `docker-compose.staging.yml` - Staging: builds from Dockerfile, mirrors production.
- `docker-compose.prod.yml` - Production: minimal image, no source mount, resource limits.

## 11. Source Code Structure (Go)

### `cmd/server/main.go`

Entry point. Reads environment variables, initializes the HTTP server, registers the `/review` handler, and starts listening.

### `internal/api/handler.go`

HTTP handler for `POST /review`:
- Validates the `X-Webhook-Secret` header against `WEBHOOK_SECRET` env var.
- Parses and validates the JSON payload.
- Calls `reviewer.StartReview(payload)` in a new goroutine.
- Returns `200 {"status": "accepted", "run_id": "<id>"}` immediately.

### `internal/reviewer/worker.go`

Core review logic:
- `StartReview(payload)` - Generates a UUID, launches a goroutine that runs the full review flow (clone, Claude Code, cleanup).
- Clone: `git clone <repo_url> --branch <head_branch> --single-branch /tmp/<run-id>`.
- Execute Claude Code CLI with the `/pr-review` skill.
- Cleanup: `os.RemoveAll("/tmp/<run-id>")` in a deferred call, always runs.

## 12. Security Considerations

- **Webhook authentication**: Every request must include a valid `X-Webhook-Secret` header matching the server's configured secret.
- **No secrets in code**: All secrets (`ANTHROPIC_AUTH_TOKEN`, `GITHUB_PAT`, `WEBHOOK_SECRET`) are injected via environment variables.
- **Ephemeral execution**: Each review runs in a fresh `/tmp/<run-id>` directory that is forcefully deleted after completion.
- **GitHub PAT scoping**: The `GITHUB_PAT` should have minimal permissions (`repo` scope for PR read/write comments and git clone). The PAT is injected into git clone URLs at runtime (never written to disk or logged).

## 13. Error Handling

| Scenario | Behavior |
|---|---|
| Invalid webhook secret | Return 401, log attempt. |
| Missing/invalid payload fields | Return 400 with specific error message. |
| Git clone failure | Log error, skip Claude Code execution, clean up temp dir. |
| Claude Code CLI crash/timeout | Log error with full output, clean up temp dir. |
| Cleanup failure | Log warning, continue. Temp dir will be overwritten on reuse or cleaned by OS. |

## 14. Logging

Logs are written to rotating log files persisted via a Docker volume mount at `/app/logs/`. Rotation is handled by [lumberjack](https://github.com/natefinch/lumberjack) — files rotate when they reach 10 MB or daily (whichever comes first), and logs are retained for 7 days. Each log line is JSON-formatted:

```json
{
  "timestamp": "2026-04-06T12:00:00Z",
  "level": "info",
  "run_id": "abc123",
  "message": "review completed",
  "pr_number": 42,
  "duration_ms": 45000
}
```

Key log events:
- Webhook received (with run_id and PR number).
- Git clone started/completed/failed.
- Claude Code execution started/completed/failed (with exit code).
- Cleanup started/completed/failed.

### Log Rotation Configuration

| Setting | Value |
|---|---|
| Log directory | `/app/logs/` (Docker volume mount) |
| Max file size | 10 MB |
| Rotation trigger | Size-based (10 MB) or daily (whichever comes first) |
| Retention | 7 days (auto-deleted after expiry) |
| Compressed archives | Yes (`.gz`) |
| Max total disk usage | ~100 MB upper bound |

### Docker Volume

All compose files must mount a named volume at `/app/logs/` so logs survive container restarts:

```yaml
volumes:
  - review-logs:/app/logs
```

## 15. Success Criteria

1. GitHub Action triggers Nano Review and gets HTTP 200 in under 1 second.
2. Claude Code reads the PR diff and posts inline review comments within 5 minutes.
3. Review comments contain actionable feedback (not generic observations).
4. Temp directories are cleaned up after every review, verified by no accumulation in `/tmp`.
5. The service runs reliably inside Docker with no manual intervention.
