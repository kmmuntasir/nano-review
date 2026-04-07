# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Nano Review** is a Go microbackend that automates AI-driven PR code reviews. A GitHub Action triggers a webhook, the Go server clones the repo into an ephemeral temp directory, spawns Claude Code CLI in headless mode with the `/pr-review` skill, and posts inline review comments via the GitHub MCP server. Each review runs in isolation and cleans up after itself.

**Project slug**: `NANO` (used in branch names and commit messages)
**Repository**: https://github.com/kmmuntasir/nano-review.git

## Current State

Greenfield — documentation and development rules are in place, but no Go source code exists yet. Implementation should follow the architecture defined in `docs/PRD.md`.

## Build & Development Commands

```bash
# Build
rtk go build -o /nano-review ./cmd/server

# Test (all packages, with race detector)
rtk go test -race ./...

# Test a single package
rtk go test -race ./internal/api/

# Run one test by name
rtk go test -race -run TestValidatePayload ./internal/api/

# Verbose with coverage
rtk go test -v -cover ./...

# Coverage report
rtk go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out

# Lint
rtk go vet ./...
rtk go fmt ./...

# Docker (dev)
rtk docker compose up --build

# Integration tests (require Docker)
rtk go test -tags=integration ./...
```

## Architecture

GitHub PR Event → GitHub Action → POST /review → API Server (Go) → async goroutine: git clone to /tmp/<id>/<repo-name>/ → claude -p "/pr-review" (CWD: /tmp/<id>/) → defer rm -rf /tmp/<id>

- Subdirectory clone: repo is cloned into `/tmp/nano-review-<id>/<repo-name>/` with Claude CWD set to `/tmp/nano-review-<id>/` to prevent the target repo's `.claude/` config from being loaded as project config.

### Planned Source Layout

```
cmd/server/main.go          # Entry point — wire deps, start HTTP server
internal/api/handler.go     # POST /review handler — validate, respond immediately
internal/reviewer/worker.go # Clone, run Claude Code CLI, cleanup
config/.claude/             # Claude config copied into Docker image
```

### Key Interfaces

- `ReviewStarter` — consumed by API handler, implemented by reviewer worker
- `ClaudeRunner` — abstracts `os/exec` calls for testability
- `Logger` — structured logging interface wrapping `log/slog`

### Single Endpoint

`POST /review` — accepts `repo_url`, `pr_number`, `base_branch`, `head_branch`. Returns `200 {"status": "accepted", "run_id": "<uuid>"}` immediately. Review happens asynchronously in a goroutine.

### Environment Variables

Required: `WEBHOOK_SECRET`, `ANTHROPIC_AUTH_TOKEN`, `GITHUB_PAT`
Optional: `PORT` (8080), `CLAUDE_CODE_PATH`, `MAX_TURNS` (30), `ANTHROPIC_BASE_URL`, `API_TIMEOUT_MS`, `ANTHROPIC_DEFAULT_HAIKU_MODEL`, `ANTHROPIC_DEFAULT_SONNET_MODEL`, `ANTHROPIC_DEFAULT_OPUS_MODEL`, `CLAUDE_CODE_DISABLE_1M_CONTEXT`

## Docker

Multi-stage build: Go builder → Ubuntu runtime with `git`, `curl`, `openssh-client`, Claude Code CLI.
Compose overlays: `docker-compose.yml` (dev), `docker-compose.staging.yml`, `docker-compose.prod.yml`.
Log volume: `review-logs:/app/logs` with lumberjack rotation (10MB, 7-day retention).

## Key Decisions

- Standard library `net/http` only — no router framework (single endpoint)
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
