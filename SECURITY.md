# Security Policy

## Supported Versions

Only the latest release is supported with security updates.

| Version | Supported |
|---------|-----------|
| 1.0.x   | Yes       |

## Reporting a Vulnerability

We take security seriously. If you discover a vulnerability in nano-review, please report it responsibly.

**Do NOT** open a public GitHub issue for security vulnerabilities.

Instead, please report via [GitHub Security Advisories](https://github.com/kmmuntasir/nano-review/security/advisories/new). This ensures the report is visible only to maintainers until a fix is released.

### What to Include

- Description of the vulnerability and its impact
- Steps to reproduce (minimal reproducible example)
- Affected component or endpoint
- Any suggested mitigation (if known)

### Response Timeline

- **Acknowledgment**: Within 72 hours of receiving a report
- **Initial assessment**: Within 7 days
- **Patch release**: As soon as a fix is available, depending on severity

### Responsible Disclosure Guidelines

- Do not exploit the vulnerability or access data beyond what is needed to demonstrate the issue
- Do not share the vulnerability with others until a fix is released
- Allow reasonable time for a fix to be developed and deployed before public disclosure
- We will credit researchers in our security advisories (unless anonymity is requested)

---

## Security Model

### Authentication Layers

#### Webhook Authentication

The `POST /review` endpoint authenticates requests via a shared secret header.

- Clients must include `X-Webhook-Secret` header matching the `WEBHOOK_SECRET` environment variable
- Requests with missing or mismatched secrets receive a `401 Unauthorized` response
- Fail-fast validation occurs before any processing

#### Google OAuth 2.0 (Dashboard)

Dashboard and WebSocket endpoints use Google OAuth 2.0 for user authentication.

- **CSRF protection**: OAuth state parameter uses a 32-byte cryptographically random token stored in an `HttpOnly`, `SameSite=Lax` cookie with a 5-minute max age. State validation uses `hmac.Equal()` for constant-time comparison.
- **Email domain restriction**: `ALLOWED_EMAIL_DOMAINS` environment variable restricts login to users with matching email domains (case-insensitive). Empty list allows all domains.
- **Fail-fast validation**: Server exits on startup if `AUTH_ENABLED=true` (default) and OAuth credentials are missing.

#### Session Tokens

Authenticated sessions use HMAC-SHA256 signed tokens with a dual-cookie strategy.

- **Token format**: `base64(sessionID).base64(timestamp).base64(random).base64(userInfo).base64(signature)`
- **HMAC-SHA256 signature**: 32-byte signature using `SESSION_SECRET` (min 32 bytes; falls back to `WEBHOOK_SECRET`)
- **Constant-time verification**: `hmac.Equal()` prevents timing attacks
- **Expiration**: Tokens expire after `SESSION_MAX_AGE_HOURS` (default: 24h)
- **Replay protection**: 16 random bytes included in signed payload
- **Dual cookies**:
  - `nano_session` — `HttpOnly`, `Secure` (default: true), `SameSite=Lax` — used for HTTP requests
  - `nano_session_token` — non-`HttpOnly`, `Secure` (default: true), `SameSite=Lax` — used for WebSocket authentication via query parameter

#### Route Protection

| Route | Auth Method |
|-------|-------------|
| `POST /review` | `X-Webhook-Secret` header |
| `GET /auth/*` | Public |
| `GET /reviews`, `GET /reviews/{run_id}`, `GET /ws`, `GET /metrics` | Session cookie or `?token=` query parameter |

### Token Handling

#### GitHub PAT (`GITHUB_PAT`)

- Injected into git clone URLs at runtime (never written to disk)
- Used in MCP server `Authorization` header for GitHub API access
- URLs are sanitized before logging (`x-access-token:***`)
- Minimal `repo` scope (read PRs, post comments, git clone)

#### Anthropic Token (`ANTHROPIC_AUTH_TOKEN`)

- Passed to Claude Code CLI as an environment variable
- Never logged or written to persistent storage
- Inherited only by child review processes

#### Webhook Secret (`WEBHOOK_SECRET`)

- Required environment variable; server exits on startup if missing
- Used for webhook header validation
- Fallback for `SESSION_SECRET` if not explicitly set (not recommended for production)

#### General Principles

- No secrets in source code or committed configuration files
- All credentials injected via environment variables (`.env` in `.gitignore`)
- `config/.claude/settings.json` contains no tokens — MCP authentication is configured at runtime

### Execution Isolation

#### Ephemeral Directories

Each review runs in an isolated temporary directory created via `os.MkdirTemp("", "nano-review-*")`.

```
/tmp/nano-review-<random>/              # CWD for Claude Code CLI
/tmp/nano-review-<random>/<repo-name>/  # Cloned repository
```

- `defer os.RemoveAll()` guarantees cleanup regardless of success or failure
- Unique per review — no shared state between executions

#### Subdirectory Clone Architecture

The repository is cloned into a subdirectory, not at the temp directory root. Claude Code's working directory is set to the parent. This prevents the target repository's `.claude/` configuration from being loaded as project config, which would allow a malicious repo to inject custom skills or MCP servers.

#### MCP Config Isolation

Claude Code is invoked with `--strict-mcp-config`, which prevents project-level `.mcp.json` files in cloned repositories from overriding the production MCP configuration. Only the GitHub MCP server (configured at `/app/mcp-config.json`) is available.

#### Process Group Cleanup

Claude Code CLI is spawned with `Setpgid: true`, creating a new process group. On completion or timeout, `syscall.Kill(-pid, SIGKILL)` terminates the entire process tree, preventing orphan processes.

#### Review Timeout

Each review is bounded by `MAX_REVIEW_DURATION` (default: 10 minutes) via `context.WithTimeout`. On timeout, the Claude Code process is killed and the temp directory is cleaned up.

### Container Security

#### Multi-Stage Build

- **Builder stage** (`golang:1.23-bookworm`): Compiles the static binary (`CGO_ENABLED=0`)
- **Runtime stage** (`ubuntu:24.04`): Contains only the binary, `git`, `curl`, and Claude Code CLI — no Go toolchain, no build tools

#### Non-Root Execution

- Container runs as dedicated `appuser` (required by Claude Code's `--dangerously-skip-permissions` flag)
- No root access in the runtime container

#### Read-Only Config Mount

Production Claude Code configuration (`config/.claude/`) is mounted as `:ro` (read-only). This prevents runtime modification of skill definitions and MCP server settings.

#### Named Volumes

- `review-logs:/app/logs` — JSON logs with lumberjack rotation (10 MB, 3 backups, 7-day retention)
- `review-data:/app/data` — SQLite database for review history

### Network Security

#### WebSocket Origin Validation

WebSocket connections at `GET /ws` validate the `Origin` header against `WS_ALLOWED_ORIGINS` (comma-separated list). Supports wildcard subdomain matching (`https://*.example.com`). Empty list accepts all origins (with a startup warning in non-development configurations).

#### Database

SQLite database is accessed via local filesystem only — no TCP port is exposed. Docker named volume provides persistence and isolation.

#### HTTPS

The container listens on HTTP internally (port 8080). An external reverse proxy (nginx, Caddy, Traefik, or cloud load balancer) must terminate HTTPS in front of the service.

### Logging

- Structured JSON logging via `log/slog` to `/app/logs/`
- No sensitive values (tokens, secrets, PATs) are logged
- Git clone URLs are sanitized before logging
- Log rotation via lumberjack: 10 MB max file size, 3 backups, 7-day retention, compressed archives
