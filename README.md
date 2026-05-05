<p align="center"><img src="web/logo-large.png" alt="Nano Review" width="120"/></p>

<p align="center">
  <h1 align="center">Nano Review</h1>
  <p align="center">
    <img src="https://img.shields.io/badge/Go-1.23-00ADD8?logo=go&logoColor=white" alt="Go 1.23">
    <img src="https://img.shields.io/badge/License-GPL--3.0-0c7bda" alt="GPL-3.0">
    <img src="https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker&logoColor=white" alt="Docker Ready">
    <img src="https://github.com/kmmuntasir/nano-review/actions/workflows/ci.yml/badge.svg" alt="CI">
    <img src="https://img.shields.io/badge/coverage-67.2%25-yellow" alt="Coverage 67.2%">
  </p>
  <p align="center">Automated AI-driven PR code review via Claude Code, running in an isolated Docker container.</p>
</p>

---

## What It Does

Nano Review is a lightweight Go microbackend that automates pull request code reviews. When a GitHub Action fires on a new or updated PR, it calls the Nano Review API, which clones the repo into an ephemeral temp directory, spawns Claude Code CLI in headless mode, and posts inline review comments directly on the pull request. Each review runs in complete isolation and cleans up after itself -- no state leaks between reviews.

## How It Works

```
 GitHub PR Event
       |
       v
 GitHub Action --(HTTP POST /review)--> API Server (Go)
                                          |
                                          v
                                     Validate payload,
                                     generate run ID
                                          |
                                     ----+----
                                     |        |
                                     v        v
                                SQLite DB   Goroutine (async)
                                (history)       |
                                                 v
                                            git clone into
                                            /tmp/<run-id>/
                                                 |
                                                 v
                                            Claude Code CLI
                                            (headless, /pr-review)
                                                 |
                                                 v
                                            GitHub MCP Server
                                            (inline comments)
                                                 |
                                                 v
                                            rm -rf /tmp/<run-id>

                                     ----+----
                                     |        |
                                     v        v
                                 GET /reviews  GET /metrics
                                 (history)     (stats)
                                        \
                                         v
                                    Web Dashboard
                                    (real-time WebSocket)
```

## Screenshots

| GitHub PR Comments | Review Dashboard |
|---|---|
| ![PR comments](docs/images/github-pull-request-comment.jpg) | ![Review dashboard](docs/images/review-details-page.jpg) |

## Quick Start

### Docker

```bash
git clone https://github.com/kmmuntasir/nano-review.git
cd nano-review
cp .env.example .env   # then edit .env with your secrets
docker compose up --build
```

### Native (No Docker)

Requires Go 1.23+, Node.js 24.x, and Claude Code CLI installed locally.

```bash
git clone https://github.com/kmmuntasir/nano-review.git
cd nano-review
make native-setup      # installs deps, generates .env, builds binary
nano .env              # fill in your secrets
make native-run        # build and run
```

The server starts on `http://localhost:8080`. See [Configuration](#configuration) for required environment variables.

### Triggering Reviews

**Via GitHub Actions** -- add the `review.yml` workflow to the target repository. See [`.github/workflows/review.yml`](.github/workflows/review.yml) for the workflow file.

**Via curl** -- for local testing, POST directly:

```bash
curl -X POST http://localhost:8080/review \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Secret: the-secret-from-your-env" \
  -d '{
    "repo_url": "https://github.com/owner/repo.git",
    "pr_number": 10,
    "base_branch": "main",
    "head_branch": "feature-branch"
  }'
```

## Features

- Async review processing -- webhook returns immediately, review runs in a background goroutine
- Inline PR comments posted via GitHub MCP server, with summary fallback
- Ephemeral execution -- each review gets an isolated temp directory, force-deleted after completion
- Automatic retry with exponential backoff for transient failures (rate limits, network errors)
- Configurable review timeout and max agentic turns
- Real-time web dashboard with WebSocket streaming and review history
- Docker Compose overlays for dev, staging, and prod environments
- Configurable Claude model selection (Haiku, Sonnet, Opus)
- Google OAuth authentication for the dashboard
- Structured JSON logging with rotation via lumberjack

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/review` | Webhook secret | Start an async PR review. Returns `{"status": "accepted", "run_id": "<uuid>"}` |
| GET | `/reviews` | Session (if enabled) | List reviews with optional filters (`repo`, `status`, `limit`, `offset`) |
| GET | `/reviews/{run_id}` | Session (if enabled) | Get a single review record with full output |
| GET | `/metrics` | Session (if enabled) | Aggregate stats: success rate, avg duration, reviews today |
| GET | `/ws` | Session (if enabled) | WebSocket endpoint for live review streaming |
| GET | `/auth/login` | None | Redirect to Google OAuth consent screen |
| GET | `/auth/callback` | None | Handle OAuth callback, create session, redirect to dashboard |
| GET | `/auth/logout` | None | Clear session cookies, redirect to login |
| GET | `/auth/me` | None | Return current session user info as JSON |
| GET | `/` | None | Serve embedded web dashboard static files |

Full API documentation: [docs/api-documentation.md](docs/api-documentation.md)

## Configuration

All configuration via environment variables. Copy [`.env.example`](.env.example) and fill in the required values.

### Required

| Variable | Description |
|----------|-------------|
| `WEBHOOK_SECRET` | Shared secret for webhook authentication |
| `ANTHROPIC_AUTH_TOKEN` | Auth token for Claude Code CLI |
| `GITHUB_PAT` | GitHub PAT with `repo` scope (clone + MCP). The GitHub account must have read access to any repository whose PRs you want reviewed. |

### Server

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server listen port |
| `DATABASE_PATH` | `/app/data/reviews.db` | SQLite database file path |
| `WS_ALLOWED_ORIGINS` | *(all origins)* | Comma-separated allowed WebSocket origins (supports `https://*.example.com` wildcards) |

### Review Worker

| Variable | Default | Description |
|----------|---------|-------------|
| `CLAUDE_CODE_PATH` | auto-detected | Path to the Claude Code binary |
| `CLAUDE_MODEL` | `sonnet` | Claude model for reviews (`haiku`, `sonnet`, `opus`) |
| `MAX_REVIEW_DURATION` | `600` | Maximum review duration in seconds |
| `MAX_RETRIES` | `2` | Retry attempts for transient failures |

### Claude API

| Variable | Default | Description |
|----------|---------|-------------|
| `ANTHROPIC_BASE_URL` | -- | Custom API endpoint (e.g., proxy) |
| `API_TIMEOUT_MS` | -- | Claude API timeout in milliseconds |
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | -- | Override default haiku model name |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | -- | Override default sonnet model name |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | -- | Override default opus model name |
| `CLAUDE_CODE_DISABLE_1M_CONTEXT` | -- | Set to `true` to disable 1M context window |
| `DISABLE_TELEMETRY` | -- | Set to `true` to disable Claude telemetry |
| `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` | -- | Set to `true` to reduce background network calls |

### Authentication

| Variable | Default | Description |
|----------|---------|-------------|
| `AUTH_ENABLED` | `true` | Set to `false` to disable OAuth (dashboard open to all) |
| `GOOGLE_CLIENT_ID` | -- | Google OAuth 2.0 client ID *(required when auth enabled)* |
| `GOOGLE_CLIENT_SECRET` | -- | Google OAuth 2.0 client secret *(required when auth enabled)* |
| `GOOGLE_OAUTH_REDIRECT_URI` | -- | OAuth callback URL *(required when auth enabled)* |
| `SESSION_SECRET` | falls back to `WEBHOOK_SECRET` | Session signing key (>= 32 chars recommended) |
| `SESSION_MAX_AGE` | `168h` | Session cookie max-age (Go duration format) |
| `SESSION_MAX_AGE_HOURS` | `24` | Server-side session lifetime in hours |
| `SESSION_CLEANUP_INTERVAL` | `1h` | Expired session cleanup interval (Go duration format) |
| `SECURE_COOKIES` | `true` | Set `Secure` flag on session cookies |
| `AUTH_COOKIE_DOMAIN` | -- | Cookie domain restriction |
| `ALLOWED_EMAIL_DOMAINS` | -- | Comma-separated allowed email domains (e.g., `company.com`) |

## Development

### Docker

```bash
make dev          # Build and run (foreground)
make test         # Run tests with race detector
make test-cover   # Tests with HTML coverage report
make lint         # Vet and format code
make stage        # Build and run staging overlay
make prod         # Build and run prod overlay
make help         # Show all available targets
```

> Go tooling runs inside Docker. Use `make` targets or `docker compose run --rm nano-review go <cmd>`.

### Native

```bash
make native-setup       # First-time: deps, .env, build
make native-run         # Build and run
make native-dev         # Run with auto-rebuild (requires air)
make native-test        # Run tests with race detector
make native-lint        # Vet and format code
```

## Production Deployment (Native)

For deploying directly on a VPS without Docker:

```bash
# 1. First-time setup
make native-setup-prod        # installs deps, generates .env.prod, builds binary
nano .env.prod                # fill in production secrets

# 2. Install as systemd service (auto-restart, starts on boot)
make native-install-prod

# 3. Verify
systemctl status nano-review
journalctl -u nano-review -f  # follow logs
```

### Updating

```bash
git pull && make native-build && systemctl restart nano-review
```

### Staging

Same flow with stage-specific targets:

```bash
make native-setup-stage       # generates .env.stage
nano .env.stage               # fill in staging secrets
make native-install-stage     # installs nano-review-stage.service
```

Run `make help` for the full list of targets.

## Project Structure

```
cmd/server/main.go            # Entry point -- wire deps, start HTTP server
internal/
  api/                        # HTTP handlers, WebSocket, models
  auth/                       # Google OAuth, session management
  reviewer/                   # Clone, run Claude Code CLI, cleanup, streaming
  storage/                    # SQLite persistence, migrations, session store
config/.claude/               # Claude Code config copied into Docker image
  skills/pr-review/           # The /pr-review skill definition
  settings.json               # MCP server configuration
web/                          # Dashboard frontend (embedded via Go embed)
docs/                         # PRD, API docs, roadmap, images
tests/integration/            # Integration tests (Docker required)
```

## Documentation

| Document | Description |
|----------|-------------|
| [PRD](docs/PRD.md) | Full product requirements and architecture |
| [API Documentation](docs/api-documentation.md) | Complete endpoint reference with examples |
| [Roadmap](docs/roadmap.md) | Prioritized future features |
| [Dev Setup](docs/references/dev-setup.md) | Local development environment guide |
| [Staging Setup](docs/references/staging-setup.md) | Staging deployment guide |
| [Prod Setup](docs/references/prod-setup.md) | Production deployment guide |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development workflow, branch naming, and commit conventions.

## License

This project is licensed under the [GNU General Public License v3.0](LICENSE).
