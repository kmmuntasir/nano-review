# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Nano Review** is a Go microbackend that automates AI-driven PR code reviews. A GitHub Action triggers a webhook, the Go server clones the repo into an ephemeral temp directory, spawns Claude Code CLI in headless mode with the `/pr-review` skill, and posts inline review comments via the GitHub MCP server. Each review runs in isolation and cleans up after itself.

**Project slug**: `NANO` (used in branch names and commit messages)
**Repository**: https://github.com/kmmuntasir/nano-review.git

## Current State

Implementation complete. The codebase includes 34 Go source files, a web dashboard with WebSocket streaming, Google OAuth authentication, SQLite storage with WAL mode, and integration tests. Documentation in `docs/PRD.md` may lag behind the implementation — when in doubt, the code is authoritative.

## Build & Development Commands

### First-Time Setup

Copy the provided template and fill in required values:

```bash
cp .env.example .env
# Edit .env — set WEBHOOK_SECRET, ANTHROPIC_AUTH_TOKEN, GITHUB_PAT
```

For native development, run `make native-setup` (or `./scripts/setup-native.sh`) — it bootstraps `.env` with native-friendly defaults, creates `data/` and `logs/` directories, installs the Claude Code skill config to `~/.claude/`, and builds the binary.

### Running Go Tooling (Native — Primary)

Requires Go 1.23+, Claude Code CLI, Node.js 24.x, and jq installed locally.
`make native-setup` installs missing dependencies automatically.

These commands run directly on the host:

```bash
make native-setup        # First-time setup: dirs, .env defaults, build
make native-build        # Build binary to ./bin/nano-review
make native-run          # Build and run (loads .env)
make native-dev          # Run with auto-rebuild via air
make native-test         # go test -race ./...
make native-test-cover   # Test + HTML coverage report
make native-lint         # go vet + go fmt
make native-clean        # Remove bin/, data/, logs/
```

Or use the bare `make` targets (`test`, `test-cover`, `lint`, `fmt`) which also run natively.

### Running Go Tooling (Docker — Alternative)

> **Important — Multi-stage build:** The Dockerfile uses a multi-stage build. The final runtime image (Stage 2) does **not** contain the Go toolchain — only the compiled binary, `git`, `curl`, and Claude Code CLI. This means `docker compose exec nano-review go ...` will **fail** against the running container. Go commands must target the **builder stage** instead, using `docker compose run` with the build target:

```bash
# Start the dev container (runtime image — no Go binary available)
rtk docker compose up --build

# Run Go commands against the builder stage (has full Go toolchain):
docker compose run --rm nano-review go build -o /nano-review ./cmd/server

docker compose run --rm nano-review go test -race ./...
docker compose run --rm nano-review go test -race ./internal/api/
docker compose run --rm nano-review go test -race -run TestValidatePayload ./internal/api/
docker compose run --rm nano-review go test -v -cover ./...
docker compose run --rm nano-review go test -coverprofile=coverage.out ./... && docker compose run --rm nano-review go tool cover -html=coverage.out

docker compose run --rm nano-review go vet ./...
docker compose run --rm nano-review go fmt ./...

# Integration tests
docker compose run --rm nano-review go test -tags=integration ./...
```

## Architecture

GitHub PR Event → GitHub Action → POST /review → API Server (Go) → Queue (bounded, wraps Worker) → goroutine: git clone to /tmp/<id>/<repo-name>/ → claude -p "/pr-review" (CWD: /tmp/<id>/) → defer rm -rf /tmp/<id>

- Subdirectory clone: repo is cloned into `/tmp/nano-review-<id>/<repo-name>/` with Claude CWD set to `/tmp/nano-review-<id>/` to prevent the target repo's `.claude/` config from being loaded as project config.

### Source Layout

```
cmd/server/main.go               # Entry point — wire deps, start HTTP server

internal/api/
  handler.go                      # HTTP handlers — POST /review, GET /reviews, GET /reviews/{run_id}, GET /metrics, GET /health
  ws_handler.go                   # WebSocket handler for live review streaming (GET /ws)
  hub.go                          # WebSocket hub — manages client subscriptions and topic broadcasting
  models.go                       # Request/response types, ReviewPayload, ReviewStarter interface
  errors.go                       # Sentinel errors and error helpers

internal/storage/
  store.go                        # ReviewStore interface, ReviewRecord, ListFilter, Metrics types
  sqlite.go                       # SQLite implementation with WAL mode
  migrate.go                      # Schema migration logic
  queries.go                      # SQL query constants
  session.go                      # SessionStore interface for session persistence
  session_sqlite.go               # SQLite implementation of SessionStore

internal/reviewer/
  worker.go                       # Review lifecycle: clone, run Claude Code CLI, cleanup, retry
  queue.go                        # Bounded queue wrapping Worker — concurrency limit, queue buffering, health stats
  logger.go                       # Structured file logger with lumberjack rotation
  stream.go                       # Stream accumulator and file-based streaming for WebSocket relay
  broadcaster.go                  # Broadcaster interface for decoupled WebSocket event push

internal/auth/
  auth.go                         # SessionManager — token creation/validation, cookie management, RequireAuth middleware
  oauth.go                        # Google OAuth2 flow handlers (login, callback, logout, session info)
  context.go                      # Context helpers for attaching/extracting User from request context

web/                              # Embedded static assets for the web dashboard
  embed.go                        # go:embed directive for static files
  index.html                      # Single-page app shell
  app.css                         # Styles
  js/                             # Client-side JavaScript (router, views, WebSocket, stream parser, markdown)

tests/integration/                # Integration tests (build tag: integration)
  testhelper.go                   # Shared test setup utilities
  logout_test.go                  # OAuth logout flow tests
  oauth_flow_test.go              # End-to-end OAuth callback tests
  protected_routes_test.go        # RequireAuth middleware tests
  websocket_auth_test.go          # WebSocket authentication tests

config/.claude/                   # Production Claude Code configuration copied into Docker image
  skills/pr-review/SKILL.md       # The /pr-review skill for PR code review
  settings.json                   # Telemetry env vars (MCP configured dynamically at runtime)

scripts/                           # Native development helper scripts
  setup-native.sh                  # First-time native setup: dirs, .env, config install, build
  run-native.sh                    # Load .env and exec ./bin/nano-review
```

### Key Interfaces

- `ReviewStarter` — consumed by API handler, implemented by reviewer queue (wraps Worker)
- `ReviewGetter` — consumed by API handlers for read endpoints, implemented by storage
- `ReviewStore` — persists review records and provides query access
- `ClaudeRunner` — abstracts `os/exec` calls for testability
- `Logger` — structured logging interface wrapping `log/slog`
- `Broadcaster` — decoupled event push to WebSocket subscribers, implemented by `api.Hub` (see `internal/reviewer/broadcaster.go`)
- `SessionManager` — session token lifecycle (create/validate/cookies), OAuth middleware (`RequireAuth`), defined in `internal/auth/auth.go`
- `SessionStore` — server-side session persistence interface, implemented by `internal/storage/session_sqlite.go`

### Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/review` | Webhook secret | Start async review. Returns `200 {"status": "accepted", "run_id": "<uuid>"}`, `202 {"status": "queued", ...}` with `Retry-After`, or `503` if queue full |
| GET | `/health` | None | Service health: active/queued reviews, concurrency limits, uptime. Status `"degraded"` when queue >80% full |
| GET | `/reviews` | Session (if enabled) | List reviews. Query params: `repo`, `status`, `limit`, `offset` |
| GET | `/reviews/{run_id}` | Session (if enabled) | Get single review detail with full output |
| GET | `/metrics` | Session (if enabled) | Aggregate stats: success rate, avg duration, reviews today |
| GET | `/ws` | Session (if enabled) | WebSocket endpoint for live review streaming. Subscribe to `run:<id>` or `all` topics |
| GET | `/auth/login` | None | Redirect to Google OAuth consent screen |
| GET | `/auth/callback` | None | Handle OAuth callback, create session, redirect to dashboard |
| GET | `/auth/logout` | None | Clear session cookies, redirect to login |
| GET | `/auth/me` | None | Return current session user info as JSON |
| GET | `/` | None | Serve embedded web dashboard static files |

Note: Set `AUTH_ENABLED=false` to disable authentication and make all session-protected endpoints accessible without a session. The `RequireAuth` middleware becomes a no-op. Auth is enabled by default.

### Environment Variables

Required: `WEBHOOK_SECRET`, `ANTHROPIC_AUTH_TOKEN`, `GITHUB_PAT`

Claude Code Configuration:
`PORT` (8080), `CLAUDE_CODE_PATH` (auto-detected), `CLAUDE_MODEL`, `ANTHROPIC_BASE_URL`, `API_TIMEOUT_MS`, `ANTHROPIC_DEFAULT_HAIKU_MODEL`, `ANTHROPIC_DEFAULT_SONNET_MODEL`, `ANTHROPIC_DEFAULT_OPUS_MODEL`, `CLAUDE_CODE_DISABLE_1M_CONTEXT`, `DISABLE_TELEMETRY`, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`, `MAX_REVIEW_DURATION` (600s), `MAX_RETRIES` (2), `MAX_CONCURRENT_REVIEWS` (3), `MAX_QUEUE_SIZE` (100)

Database: `DATABASE_PATH` (`/app/data/reviews.db` in Docker, `./data/reviews.db` native)

Native Paths:
`NANO_DATA_DIR` (`./data`) — base dir for database and storage, `NANO_LOG_DIR` (`./logs`) — base dir for logs. Both default to `/app/data` and `/app/logs` respectively inside Docker.

Authentication & Sessions:
`AUTH_ENABLED` (true), `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `SESSION_SECRET` (falls back to WEBHOOK_SECRET), `GOOGLE_OAUTH_REDIRECT_URI`, `SESSION_MAX_AGE` (Go duration), `SESSION_MAX_AGE_HOURS` (24), `SECURE_COOKIES` (true), `AUTH_COOKIE_DOMAIN`, `ALLOWED_EMAIL_DOMAINS` (comma-separated), `SESSION_CLEANUP_INTERVAL` (1h)

WebSocket: `WS_ALLOWED_ORIGINS` (comma-separated, supports `https://*.example.com` wildcards)

## Docker

Multi-stage build: Go builder (`golang:1.23-bookworm`) → Ubuntu runtime with `git`, `curl`, Claude Code CLI, Node.js (for Caveman), and RTK.
- The **builder stage** has the Go toolchain — use this for `go build`, `go test`, `go vet`, etc.
- The **runtime stage** has only the compiled binary — no Go binary is available for running tests or building.
- **RTK (Rust Token Killer)** — CLI proxy for token-optimized command output (60-90% savings). Installed via `curl | sh` from GitHub. Requires `jq` for hook operation. The `rtk init` command configures a `PreToolUse` hook that transparently rewrites Bash tool calls to pipe through `rtk` for output minification.
Compose overlays: `docker-compose.yml` (dev), `docker-compose.staging.yml`, `docker-compose.prod.yml`.
Log volume: `review-logs:/app/logs` with lumberjack rotation (10MB, 7-day retention).
Data volume: `review-data:/app/data` for SQLite database (review history).

## Key Decisions

- Standard library `net/http` only — no router framework (Go 1.22+ enhanced ServeMux)
- No `pkg/` directory — all project code in `internal/`
- Ephemeral execution: every review gets a fresh `/tmp/<run-id>`, force-deleted via `defer os.RemoveAll`
- Native execution support via `NANO_DATA_DIR` and `NANO_LOG_DIR` — paths default to `./data` and `./logs` on host, `/app/data` and `/app/logs` in Docker
- Webhook auth via `X-Webhook-Secret` header comparison
- **TWO separate `.claude/` directories exist** — do NOT confuse them:
  - `.claude/` (project root) — Development-only rules for AI agents working on this codebase (Go style, testing, git workflow). Never copied into Docker.
  - `config/.claude/` — Production Claude Code configuration (skill definitions, MCP server settings) copied into the Docker image at build time. This is what runs inside the container to perform PR reviews.
- **Always run `make lint` before committing.** Lint must pass with zero errors before any commit is created. Fix all issues found — do not bypass or skip.

## File Writing Direction

Claude must write any new report, documentation, or summary file in `./docs/ai_generated/`, unless the user explicitly requests a different location. Do not create such files in the project root or any other directory without being asked.

For permanent team reference documentation (CI/CD guides, security docs, pipeline setup, etc.), use `./docs/references/` instead. Files in `docs/references/` are tracked by git and shared with all team members.

## Documentation

- `docs/PRD.md` — Full product requirements, API spec, Docker setup, security model
- `docs/roadmap.md` — Prioritized future features (check runs API, timeouts, retry, queue)
- `.claude/rules/` — Development-only rules for AI agents coding on this project (Go style, testing, git workflow, persona). NOT used in Docker.
- `config/.claude/skills/pr-review/SKILL.md` — The Claude Code skill that runs inside Docker to perform reviews. This file lives under `config/`, NOT under the project root `.claude/`.
- `config/.claude/settings.json` — Telemetry environment variables for Claude Code. The GitHub MCP server is configured dynamically at runtime in `cmd/server/main.go` (written to `/app/mcp-config.json` and passed via `--mcp-config --strict-mcp-config`). The settings.json file also lives under `config/`, NOT under the project root `.claude/`.
