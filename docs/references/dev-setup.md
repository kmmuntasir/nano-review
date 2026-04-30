# Development Environment Setup

## Overview

Two development modes are available:

| Mode | Description | Best for |
|------|-------------|----------|
| **Native** (recommended) | Build and run directly on the host | Fast iteration, debugging, CI |
| **Docker** | Multi-stage container build | Reproducible env, production-like testing |

## Prerequisites

### Native Development

- Go 1.23+
- Git
- Claude Code CLI (`claude`)
- Node.js 24.x LTS (Caveman dependency)
- jq (RTK hook dependency)

### Docker Development

- Docker Engine
- Docker Compose v2+

> Claude Code CLI is installed inside the Docker image at build time — no local installation needed for Docker mode.

## Quick Start (Native)

```bash
git clone https://github.com/kmmuntasir/nano-review.git
cd nano-review
cp .env.example .env
# Edit .env — fill in WEBHOOK_SECRET, ANTHROPIC_AUTH_TOKEN, GITHUB_PAT
make native-setup
make native-run
```

> `make native-setup` also installs Caveman plugin and RTK (Rust Token Killer).
> Caveman provides terse communication mode for PR reviews. RTK optimizes CLI output tokens.
> Both configure hooks in `~/.claude/settings.json` automatically.

Verify:

```bash
curl http://localhost:8080/healthz
```

Server starts on `http://localhost:8080`.

## Native Commands

| Target | Description |
|--------|-------------|
| `make native-setup` | First-time setup: check prereqs, create dirs, bootstrap `.env`, install Claude config, build binary |
| `make native-build` | Build `./bin/nano-review` |
| `make native-run` | Build and run (loads `.env`) |
| `make native-dev` | Run with auto-rebuild on file changes (requires [air](https://github.com/air-verse/air)) |
| `make native-test` | Run tests with race detector |
| `make native-test-cover` | Run tests with HTML coverage report |
| `make native-lint` | Run `go vet` and `go fmt` |
| `make native-clean` | Remove `bin/`, `data/`, `logs/` |

## Environment Variables

### Core Variables

| Variable | Description | Example |
|---|---|---|
| `WEBHOOK_SECRET` | Secret shared with GitHub Action to authenticate webhooks | `dev-secret-abc123` |
| `ANTHROPIC_AUTH_TOKEN` | Auth token for Claude Code CLI (e.g., Z.AI token) | `your-auth-token` |
| `GITHUB_PAT` | GitHub Personal Access Token (repo scope) | `ghp_...` |
| `PORT` | Server listen port (optional, default `8080`) | `8080` |
| `CLAUDE_CODE_PATH` | Path to Claude Code binary (optional, auto-detected) | `/usr/local/bin/claude` |
| `ANTHROPIC_BASE_URL` | Custom API endpoint (optional) | `https://api.z.ai/api/anthropic` |
| `API_TIMEOUT_MS` | API timeout in milliseconds (optional) | `3000000` |
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | Override haiku model name (optional) | `claude-3-5-haiku-20241022` |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | Override sonnet model name (optional) | `claude-sonnet-4-20250514` |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | Override opus model name (optional) | `claude-opus-4-20250514` |
| `CLAUDE_CODE_DISABLE_1M_CONTEXT` | Disable 1M context window (optional) | `1` |

### Review Configuration

| Variable | Description | Default |
|---|---|---|
| `CLAUDE_MODEL` | Claude model for reviews (e.g., `haiku`, `sonnet`, `opus`) | `sonnet` |
| `MAX_REVIEW_DURATION` | Maximum review duration in seconds | `600` |
| `MAX_RETRIES` | Maximum retry attempts for transient failures | `2` |
| `DISABLE_TELEMETRY` | Disable Claude telemetry | — |
| `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` | Disable non-essential network traffic | — |
| `DATABASE_PATH` | SQLite database file path | `/app/data/reviews.db` (Docker) / `./data/reviews.db` (native) |

### Native-Specific Variables

| Variable | Description | Default |
|---|---|---|
| `NANO_DATA_DIR` | Directory for SQLite database | `./data` |
| `NANO_LOG_DIR` | Directory for log files | `./logs` |

> `make native-setup` appends these to `.env` automatically if absent.

### Authentication Variables

> These variables are only relevant when authentication is enabled. Authentication is enabled by default; set `AUTH_ENABLED=false` to disable it entirely (useful for local development).

| Variable | Description | Default |
|---|---|---|
| `AUTH_ENABLED` | Enable/disable authentication (`false` to disable) | `true` |
| `GOOGLE_CLIENT_ID` | Google OAuth client ID | — |
| `GOOGLE_CLIENT_SECRET` | Google OAuth client secret | — |
| `SESSION_SECRET` | Session signing secret (falls back to `WEBHOOK_SECRET`) | — |
| `GOOGLE_OAUTH_REDIRECT_URI` | OAuth callback URL | — |
| `SESSION_MAX_AGE_HOURS` | Session cookie max-age in hours | `168` (7 days) |
| `SESSION_MAX_AGE` | Session cleanup max-age (Go duration format) | `168h` |
| `SESSION_CLEANUP_INTERVAL` | Session cleanup interval (Go duration format) | `1h` |
| `SECURE_COOKIES` | Set `Secure` flag on cookies (`false` for local HTTP) | `true` |
| `AUTH_COOKIE_DOMAIN` | Cookie domain restriction | — |
| `ALLOWED_EMAIL_DOMAINS` | Comma-separated allowed email domains | — |

### WebSocket Configuration

| Variable | Description | Default |
|---|---|---|
| `WS_ALLOWED_ORIGINS` | Comma-separated WebSocket allowed origins (supports `https://*.example.com` wildcards) | — (all origins) |

## Docker Setup (Alternative)

### Clone and Start

```bash
git clone https://github.com/kmmuntasir/nano-review.git
cd nano-review
cp .env.example .env
# Edit .env with your values
docker compose up --build
```

This uses `docker-compose.yml` — the base dev configuration with:
- Multi-stage build: Go builder → Ubuntu runtime with Claude Code CLI, git, curl
- Port mapping (`${PORT:-8080}:8080`)
- `.env` file loaded automatically
- `review-logs` named volume at `/app/logs`
- `review-data` named volume at `/app/data`
- `restart: unless-stopped` policy

Server starts on `http://localhost:8080`.

### Docker Testing

> **Important — Multi-stage build:** The Dockerfile uses a multi-stage build. The final runtime image (Stage 2) does **not** contain the Go toolchain — only the compiled binary, `git`, `curl`, and Claude Code CLI. This means `docker compose exec nano-review go ...` will **fail** against the running container. All Go commands must use `docker compose run --rm nano-review`, which targets the builder stage.

```bash
# Run all tests with race detector
docker compose run --rm nano-review go test -race ./...

# Run tests for a specific package
docker compose run --rm nano-review go test -race ./internal/api/
docker compose run --rm nano-review go test -race ./internal/reviewer/

# Verbose with coverage
docker compose run --rm nano-review go test -v -cover ./...

# Generate HTML coverage report
docker compose run --rm nano-review go test -coverprofile=coverage.out ./... && docker compose run --rm nano-review go tool cover -html=coverage.out

# Integration tests only
docker compose run --rm nano-review go test -tags=integration ./...
```

### Docker Linting

```bash
docker compose run --rm nano-review go vet ./...
docker compose run --rm nano-review go fmt ./...
```

## Manual API Test

Trigger a review request with `curl`:

```bash
curl -X POST http://localhost:8080/review \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Secret: dev-secret-abc123" \
  -d '{
    "repo_url": "https://github.com/owner/repo.git",
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

**Auth:** Webhook secret (`X-Webhook-Secret` header)

**Request Body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `repo_url` | string | yes | HTTPS clone URL of the repo |
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

---

### `GET /reviews`

List reviews with optional filters.

**Auth:** Protected (session required unless `AUTH_ENABLED=false`)

**Query Parameters:**

| Parameter | Type | Description |
|---|---|---|
| `repo` | string | Filter by repo URL (substring) |
| `status` | string | Filter by status (`pending`, `running`, `completed`, `failed`, `timed_out`, `cancelled`) |
| `limit` | int | Maximum results to return |
| `offset` | int | Number of results to skip |

**Response:**

```json
{"reviews":[{"run_id":"...","repo":"...","pr_number":42,"status":"completed",...}],"count":1}
```

**Status Codes:** `200` (success), `401` (unauthorized), `500` (server error)

---

### `GET /reviews/{run_id}`

Get a single review detail (full `ReviewRecord` JSON).

**Auth:** Protected (session required unless `AUTH_ENABLED=false`)

**Response:** Full review record including `run_id`, `repo`, `pr_number`, `base_branch`, `head_branch`, `status`, `conclusion`, `duration_ms`, `attempts`, `claude_output`, `created_at`, `completed_at`.

**Status Codes:** `200` (success), `401` (unauthorized), `404` (`{"error":"review not found"}`), `500` (server error)

---

### `GET /metrics`

Aggregate review statistics (success rate, average duration, reviews today).

**Auth:** Protected (session required unless `AUTH_ENABLED=false`)

**Response:**

```json
{
  "total_reviews": 10,
  "success_count": 8,
  "failure_count": 1,
  "timed_out_count": 0,
  "cancelled_count": 1,
  "avg_duration_ms": 45000,
  "reviews_today": 3
}
```

**Status Codes:** `200` (success), `401` (unauthorized), `500` (server error)

---

### `GET /ws`

WebSocket endpoint for streaming review updates. Clients subscribe to topics (`run:<run_id>` or `all`) to receive real-time `review_update`, `stream_done`, and `metrics_update` events.

**Auth:** Protected. Authenticates via `nano_session` cookie or `?token=` query parameter.

**Query Parameters:**

| Parameter | Type | Description |
|---|---|---|
| `token` | string | Session token (alternative to cookie for WebSocket auth) |

**Status Codes:** `101` (upgrade success), `401` (unauthorized)

---

### `GET /auth/login`

Initiates the Google OAuth flow. Redirects to Google's consent screen.

**Auth:** Public

**Query Parameters:**

| Parameter | Type | Description |
|---|---|---|
| `state` | string | Optional redirect path after successful login |

**Status Codes:** `307` (redirect to Google), `405` (non-GET), `501` (OAuth not configured)

---

### `GET /auth/callback`

OAuth callback handler. Exchanges authorization code for tokens, fetches user profile, creates session, and redirects to the frontend.

**Auth:** Public

**Status Codes:** `302` (redirect after success), `400` (invalid/missing state or code), `403` (email domain not allowed), `502` (token exchange or userinfo fetch failed), `501` (OAuth not configured)

---

### `GET /auth/logout`

Clears session cookies and redirects to the home page.

**Auth:** Public

**Status Codes:** `302` (redirect to `/`)

---

### `GET /auth/me`

Returns current session user info as JSON.

**Auth:** Public

**Response (authenticated):**

```json
{"id":"...","email":"user@example.com","name":"User Name","picture":"https://..."}
```

**Response (auth disabled):**

```json
{"auth_enabled":false}
```

**Response (not authenticated):**

```json
{}
```

**Status Codes:** `200` (success), `405` (non-GET)

---

## Logs

- **Console output**: structured text logs to stdout
- **File output**: rotated JSON logs at `<NANO_LOG_DIR>/review.log` (native) or `/app/logs/review.log` (Docker) — 10MB max, 7-day retention, 3 compressed backups
- **Review outputs**: Individual review outputs are saved to `<NANO_LOG_DIR>/reviews/` or `/app/logs/reviews/` as timestamped text files (format: `<timestamp>_<repo>_pr<N>_<run-id-prefix>.txt`). See `internal/reviewer/worker.go` for details.

## Troubleshooting

### Native Issues

| Problem | Solution |
|---------|----------|
| `go: command not found` | Install Go 1.23+ from https://go.dev/dl/ |
| `claude: command not found` | Install Claude Code CLI: https://docs.anthropic.com/en/docs/claude-code/overview |
| Go version < 1.23 | Upgrade Go. Run `go version` to check |
| Permission denied on `./bin/nano-review` | Run `chmod +x ./bin/nano-review` |
| `database is locked` | Ensure only one process is running. Remove stale `./data/reviews.db-wal` and `./data/reviews.db-shm` files |
| `.env` missing native variables | Run `make native-setup` again — it only appends missing keys |
| `node: command not found` | Install Node.js 24.x or re-run `make native-setup` |
| `rtk: command not found` | Re-run `make native-setup` or add `~/.local/bin` to `$PATH` |
| Caveman hooks not loading | Check `~/.claude/settings.json` for hooks section. Re-run Caveman install script |
| RTK hook conflicts with Caveman | Run `rtk init -g --auto-patch` — patches existing settings without overwrite |

### Docker Issues

| Problem | Solution |
|---------|----------|
| `docker compose exec go ...` fails | Runtime image has no Go toolchain. Use `docker compose run --rm nano-review go ...` instead |
| Port already in use | Change `PORT` in `.env` or stop the conflicting service |
| `review-data` volume stale | `docker compose down -v` removes volumes and data |

## Project Structure

```
cmd/server/main.go              # Entry point — env validation, dependency wiring, graceful shutdown
internal/api/
  handler.go                    # HTTP handlers for POST /review, GET /reviews, GET /reviews/{run_id}, GET /metrics
  models.go                     # Request/response types (ReviewPayload, AcceptResponse, ErrorResponse, ListReviewsResponse)
  errors.go                     # Sentinel errors
  hub.go                        # WebSocket hub for pub/sub broadcasting
  ws_handler.go                 # WebSocket upgrade and client management
  handler_test.go               # Unit tests for HTTP handlers
  models_test.go                # Unit tests for models and validation
  hub_test.go                   # Unit tests for WebSocket hub
  ws_handler_test.go            # Unit tests for WebSocket handler
internal/auth/
  auth.go                       # SessionManager — token creation, validation, cookie management, RequireAuth middleware
  context.go                    # User type and context helpers (UserFromContext, ContextWithUser)
  oauth.go                      # Google OAuth handlers (login, callback, logout, session info)
  auth_test.go                  # Unit tests for session management
  oauth_test.go                 # Unit tests for OAuth flow
  context_test.go               # Unit tests for context helpers
internal/reviewer/
  worker.go                     # Review lifecycle — clone, run Claude CLI, retry, cleanup
  stream.go                     # Stream accumulator and WebSocket stream writer
  broadcaster.go                # Broadcaster interface for WebSocket pub/sub
  logger.go                     # Structured multi-writer logger (stdout + file)
  worker_test.go                # Unit tests for worker
  stream_test.go                # Unit tests for stream accumulator
internal/storage/
  store.go                      # ReviewStore and SessionStore interfaces, record types, metrics
  sqlite.go                     # SQLite implementation with WAL mode
  migrate.go                    # Database schema migrations
  queries.go                    # SQL query constants
  session.go                    # SessionStore interface and SessionRecord types
  session_sqlite.go             # SQLite session store implementation
  sqlite_test.go                # Unit tests for review store
  session_sqlite_test.go        # Unit tests for session store
web/
  embed.go                      # Go embed for serving static frontend files
  index.html                    # Single-page application entry point
  app.css                       # Application styles
  js/
    api.js                      # API client for backend endpoints
    auth.js                     # Authentication state management
    main.js                     # Application bootstrap
    router.js                   # Client-side hash router
    markdown.js                 # Markdown rendering utility
    stream-renderer.js          # WebSocket stream display renderer
    stream-parser.js            # Claude stream-json parser
    utils.js                    # General utility functions
    ws.js                       # WebSocket connection management
    views/
      dashboard.js              # Dashboard view (overview with metrics)
      reviews.js                # Reviews list view
      detail.js                 # Single review detail view
config/.claude/
  settings.json                 # Claude Code MCP server config (GitHub Copilot)
  skills/pr-review/SKILL.md     # Claude Code skill definition for PR reviews
scripts/
  setup-native.sh               # Native first-time setup script
  run-native.sh                 # Native run script (loads .env, execs binary)
tests/integration/
  protected_routes_test.go      # Integration tests for auth-protected routes
  oauth_flow_test.go            # Integration tests for OAuth login/callback/logout
  websocket_auth_test.go        # Integration tests for WebSocket authentication
  logout_test.go                # Integration tests for logout flow
  testhelper.go                 # Shared test helpers for integration tests
.github/workflows/
  review.yml                    # GitHub Action that triggers Nano Review on PRs
Makefile                        # Build, test, and lint convenience targets
.air.toml                       # Air live-reload config for native dev
.golangci.yml                   # golangci-lint configuration
go.mod                          # Go module definition
go.sum                          # Go module checksums
```
