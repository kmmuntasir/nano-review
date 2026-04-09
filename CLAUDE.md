# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Nano Review** is a Go microbackend that automates AI-driven PR code reviews. A GitHub Action triggers a webhook, the Go server clones the repo into an ephemeral temp directory, spawns Claude Code CLI in headless mode with the `/pr-review` skill, and posts inline review comments via the GitHub MCP server. Each review runs in isolation and cleans up after itself.

**Project slug**: `NANO` (used in branch names and commit messages)
**Repository**: https://github.com/kmmuntasir/nano-review.git

## Current State

Greenfield — documentation and development rules are in place, but no Go source code exists yet. Implementation should follow the architecture defined in `docs/PRD.md`.

## Build & Development Commands

> **All Go tooling (build, test, lint) must run inside the Docker container.** Host machines do not have Go installed. Use `docker compose exec` to run commands against the running dev container, or `docker compose run` for one-off commands.

```bash
# Start the dev container
rtk docker compose up --build

# Run commands inside the container:
docker compose exec nano-review go build -o /nano-review ./cmd/server

docker compose exec nano-review go test -race ./...
docker compose exec nano-review go test -race ./internal/api/
docker compose exec nano-review go test -race -run TestValidatePayload ./internal/api/
docker compose exec nano-review go test -v -cover ./...
docker compose exec nano-review go test -coverprofile=coverage.out ./... && docker compose exec nano-review go tool cover -html=coverage.out

docker compose exec nano-review go vet ./...
docker compose exec nano-review go fmt ./...

# Integration tests
docker compose exec nano-review go test -tags=integration ./...
```

## Architecture

GitHub PR Event → GitHub Action → POST /review → API Server (Go) → async goroutine: git clone to /tmp/<id>/<repo-name>/ → claude -p "/pr-review" (CWD: /tmp/<id>/) → defer rm -rf /tmp/<id>

- Subdirectory clone: repo is cloned into `/tmp/nano-review-<id>/<repo-name>/` with Claude CWD set to `/tmp/nano-review-<id>/` to prevent the target repo's `.claude/` config from being loaded as project config.

### Planned Source Layout

```
cmd/server/main.go          # Entry point — wire deps, start HTTP server
internal/api/handler.go     # HTTP handlers — POST /review, GET /reviews, GET /metrics
internal/storage/store.go   # ReviewStore interface and record types
internal/storage/sqlite.go  # SQLite implementation with WAL mode
internal/reviewer/worker.go # Clone, run Claude Code CLI, cleanup
config/.claude/             # Claude config copied into Docker image
```

### Key Interfaces

- `ReviewStarter` — consumed by API handler, implemented by reviewer worker
- `ReviewGetter` — consumed by API handlers for read endpoints, implemented by storage
- `ReviewStore` — persists review records and provides query access
- `ClaudeRunner` — abstracts `os/exec` calls for testability
- `Logger` — structured logging interface wrapping `log/slog`

### Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/review` | Webhook secret | Start async review. Returns `{"status": "accepted", "run_id": "<uuid>"}` |
| GET | `/reviews` | None | List reviews. Query params: `repo`, `status`, `limit`, `offset` |
| GET | `/reviews/{run_id}` | None | Get single review detail with full output |
| GET | `/metrics` | None | Aggregate stats: success rate, avg duration, reviews today |

### Environment Variables

Required: `WEBHOOK_SECRET`, `ANTHROPIC_AUTH_TOKEN`, `GITHUB_PAT`
Optional: `PORT` (8080), `CLAUDE_CODE_PATH`, `MAX_TURNS` (30), `ANTHROPIC_BASE_URL`, `API_TIMEOUT_MS`, `ANTHROPIC_DEFAULT_HAIKU_MODEL`, `ANTHROPIC_DEFAULT_SONNET_MODEL`, `ANTHROPIC_DEFAULT_OPUS_MODEL`, `CLAUDE_CODE_DISABLE_1M_CONTEXT`, `DATABASE_PATH` (`/app/data/reviews.db`)

## Docker

Multi-stage build: Go builder → Ubuntu runtime with `git`, `curl`, Claude Code CLI.
Compose overlays: `docker-compose.yml` (dev), `docker-compose.staging.yml`, `docker-compose.prod.yml`.
Log volume: `review-logs:/app/logs` with lumberjack rotation (10MB, 7-day retention).
Data volume: `review-data:/app/data` for SQLite database (review history).

## Key Decisions

- Standard library `net/http` only — no router framework (Go 1.22+ enhanced ServeMux)
- No `pkg/` directory — all project code in `internal/`
- Ephemeral execution: every review gets a fresh `/tmp/<run-id>`, force-deleted via `defer os.RemoveAll`
- Webhook auth via `X-Webhook-Secret` header comparison
- **TWO separate `.claude/` directories exist** — do NOT confuse them:
  - `.claude/` (project root) — Development-only rules for AI agents working on this codebase (Go style, testing, git workflow). Never copied into Docker.
  - `config/.claude/` — Production Claude Code configuration (skill definitions, MCP server settings) copied into the Docker image at build time. This is what runs inside the container to perform PR reviews.

## File Writing Direction

Claude must write any new report, documentation, or summary file in `./docs/ai_generated/`, unless the user explicitly requests a different location. Do not create such files in the project root or any other directory without being asked.

For permanent team reference documentation (CI/CD guides, security docs, pipeline setup, etc.), use `./docs/references/` instead. Files in `docs/references/` are tracked by git and shared with all team members.

## Documentation

- `docs/PRD.md` — Full product requirements, API spec, Docker setup, security model
- `docs/roadmap.md` — Prioritized future features (check runs API, timeouts, retry, queue)
- `.claude/rules/` — Development-only rules for AI agents coding on this project (Go style, testing, git workflow, persona). NOT used in Docker.
- `config/.claude/skills/pr-review/SKILL.md` — The Claude Code skill that runs inside Docker to perform reviews. This file lives under `config/`, NOT under the project root `.claude/`.
- `config/.claude/settings.json` — MCP server configuration (GitHub Copilot MCP) copied into Docker. Also lives under `config/`, NOT under the project root `.claude/`.
