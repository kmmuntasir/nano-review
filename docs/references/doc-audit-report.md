# Documentation Audit Report

**Date:** 2026-04-15
**Auditor:** Automated (Claude Code)
**Scope:** All tracked documentation files

---

## Summary

Audited 9 documentation files against the actual source code, Docker configuration, and project structure. Found **5 critical issues**, **16 non-critical issues**, and confirmed **27 acceptable items**.

The most significant findings are:
1. **CLAUDE.md** is severely outdated — still claims "no Go source code exists yet" and lists an incomplete source layout, missing 8+ source files and the entire `auth/`, `web/`, and `tests/` packages.
2. **PRD.md** is outdated — describes MVP without auth, storage, dashboard, WebSocket, or retry/timeout features that are now implemented.
3. **dev-setup.md** uses `docker compose exec` for Go commands instead of `docker compose run` (as documented in CLAUDE.md), and is missing documentation for `AUTH_ENABLED`, `DATABASE_PATH`, and other new env vars.
4. **config/.claude/settings.json** only contains env vars for disabling telemetry — the GitHub MCP server is now configured at runtime via `configureClaudeMCP()` in `main.go`, not via a static settings file.
5. **Dockerfile** has evolved significantly from the PRD — now uses a non-root user, and `config/.claude/` is also mounted as a read-only volume in `docker-compose.yml`.

---

## Critical Issues

### C1. CLAUDE.md — "Current State" claims no Go code exists

**File:** `CLAUDE.md` (line 14)
**Issue:** States "Greenfield — documentation and development rules are in place, but no Go source code exists yet." The codebase has substantial Go code across 20+ source files including full API handlers, storage layer, auth system, reviewer worker, WebSocket streaming, and web dashboard.

### C2. CLAUDE.md — Planned Source Layout is incomplete and inaccurate

**File:** `CLAUDE.md` (lines 62-69)
**Issue:** The "Planned Source Layout" lists only 5 files. The actual codebase has significantly more:

| Documented | Actually Exists | Missing from Docs |
|---|---|---|
| `cmd/server/main.go` | Yes | - |
| `internal/api/handler.go` | Yes | - |
| `internal/storage/store.go` | Yes | - |
| `internal/storage/sqlite.go` | Yes | - |
| `internal/reviewer/worker.go` | Yes | - |
| - | `internal/api/models.go` | Missing |
| - | `internal/api/errors.go` | Missing |
| - | `internal/api/ws_handler.go` | Missing |
| - | `internal/api/hub.go` | Missing |
| - | `internal/auth/auth.go` | Missing |
| - | `internal/auth/oauth.go` | Missing |
| - | `internal/auth/context.go` | Missing |
| - | `internal/reviewer/logger.go` | Missing |
| - | `internal/reviewer/stream.go` | Missing |
| - | `internal/reviewer/broadcaster.go` | Missing |
| - | `internal/storage/migrate.go` | Missing |
| - | `internal/storage/queries.go` | Missing |
| - | `internal/storage/session.go` | Missing |
| - | `internal/storage/session_sqlite.go` | Missing |
| - | `web/embed.go` | Missing |
| - | `web/index.html`, `web/app.css` | Missing |
| - | `web/js/*.js` (8 files) | Missing |
| - | `tests/integration/*.go` (5 files) | Missing |

### C3. PRD.md — Non-Goals are now implemented

**File:** `docs/PRD.md` (lines 20-28)
**Issue:** Several items listed as "Non-Goals (Out of MVP Scope)" are now implemented:
- "Authentication/authorization beyond webhook secret validation" — Google OAuth, session management, and `RequireAuth` middleware are fully implemented in `internal/auth/`.
- "Review history persistence or dashboards" — SQLite storage (`internal/storage/`) and a web dashboard (`web/`) are implemented.
- "Rate limiting or queue management for concurrent reviews" — Retry with exponential backoff (`MAX_RETRIES`) and timeout (`MAX_REVIEW_DURATION`) are implemented.

### C4. PRD.md — Dockerfile is outdated

**File:** `docs/PRD.md` (lines 238-268)
**Issue:** The documented Dockerfile differs significantly from the actual `Dockerfile`:
- Actual Dockerfile creates a non-root `appuser` (Claude Code refuses `--dangerously-skip-permissions` as root)
- Uses `ENTRYPOINT` instead of `CMD`
- Copies `.claude/` to `/home/appuser/.claude/` (not `/root/.claude/`)
- Creates `/app/logs/reviews` and `/app/data` directories
- Uses `bash` instead of `sh` for Claude install
- Does not set `WORKDIR /app` (only `EXPOSE`)

### C5. PRD.md — `.claude/settings.json` is inaccurate

**File:** `docs/PRD.md` (lines 159-175)
**Issue:** The PRD shows `config/.claude/settings.json` with GitHub MCP server configuration. The actual file at `config/.claude/settings.json` only contains:
```json
{
  "env": {
    "DISABLE_TELEMETRY": "1",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"
  }
}
```
The GitHub MCP server is now configured dynamically at runtime via `configureClaudeMCP()` in `cmd/server/main.go`, which writes a dedicated MCP config to `/app/mcp-config.json` and passes it via `--mcp-config --strict-mcp-config` flags.

---

## Non-Critical Issues

### N1. PRD.md — SKILL.md content in PRD is outdated

**File:** `docs/PRD.md` (lines 126-157)
**Issue:** The PRD embeds a simplified version of the SKILL.md. The actual `config/.claude/skills/pr-review/SKILL.md` is substantially different — it includes parallel subagent strategy, mandatory inline comments rules, detailed git command instructions, and MCP tool call order requirements. The PRD version does not reflect the actual skill.

### N2. PRD.md — Review Worker command is outdated

**File:** `docs/PRD.md` (lines 113-118)
**Issue:** The documented Claude CLI command uses `--output-format json --max-turns 30`. The actual command in `worker.go` uses `--output-format stream-json --verbose --include-partial-messages` and optionally `--model` and `--mcp-config --strict-mcp-config`. The `--max-turns` flag is not used at all.

### N3. PRD.md — Review Worker clone step missing `--single-branch`

**File:** `docs/PRD.md` (line 111)
**Issue:** Documents `git clone <repo_url> --branch <head_branch> /tmp/<run-id>`. The actual `worker.go` uses `--single-branch` flag: `git clone --branch <head_branch> --single-branch <cloneURL> <dir>`.

### N4. PRD.md — Directory structure is incomplete

**File:** `docs/PRD.md` (lines 210-234)
**Issue:** The documented directory structure is missing many existing files/directories: `internal/auth/`, `internal/storage/`, `web/`, `tests/`, `.github/workflows/`, `Makefile`, `go.mod`, `go.sum`, `CLAUDE.md`, `README.md`, `CHANGELOG.md`, `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `.golangci.yml`, etc.

### N5. dev-setup.md — Uses `docker compose exec` instead of `docker compose run`

**File:** `docs/references/dev-setup.md` (lines 54-67)
**Issue:** All test/lint commands use `docker compose exec nano-review go ...`. CLAUDE.md explicitly warns that `docker compose exec` will fail against the running container (runtime image has no Go binary) and instructs to use `docker compose run --rm nano-review go ...` instead. These two documents contradict each other.

### N6. dev-setup.md — Missing many environment variables

**File:** `docs/references/dev-setup.md` (lines 36-48)
**Issue:** The environment variable table is missing variables that are documented in `.env.example` and used in the code:
- `CLAUDE_MODEL`
- `MAX_REVIEW_DURATION`
- `MAX_RETRIES`
- `DISABLE_TELEMETRY`
- `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`
- `DATABASE_PATH`
- `AUTH_ENABLED`
- `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`
- `SESSION_SECRET`
- `GOOGLE_OAUTH_REDIRECT_URI`
- `SESSION_MAX_AGE_HOURS`
- `SECURE_COOKIES`
- `AUTH_COOKIE_DOMAIN`
- `ALLOWED_EMAIL_DOMAINS`
- `WS_ALLOWED_ORIGINS`

### N7. dev-setup.md — Missing endpoints from API Reference

**File:** `docs/references/dev-setup.md` (lines 98-149)
**Issue:** Only documents `POST /review`. Missing:
- `GET /reviews`
- `GET /reviews/{run_id}`
- `GET /metrics`
- `GET /ws` (WebSocket)
- `GET /auth/login`, `GET /auth/callback`, `GET /auth/logout`, `GET /auth/me`

### N8. api-documentation.md — Auth on read endpoints is inaccurate

**File:** `docs/api-documentation.md` (lines 86-88, 152-154, 220-222)
**Issue:** Documents "None required" for `GET /reviews`, `GET /reviews/{run_id}`, and `GET /metrics`. In the actual code (`main.go` lines 363-366), these endpoints are wrapped with `sessionMgr.RequireAuth(...)`. When `AUTH_ENABLED` is not set to `false`, authentication IS required. The docs should clarify this conditional auth requirement.

### N9. CLAUDE.md — Environment Variables list is incomplete

**File:** `CLAUDE.md` (line 91)
**Issue:** Missing these environment variables used in the code:
- `CLAUDE_MODEL`
- `MAX_REVIEW_DURATION`
- `MAX_RETRIES`
- `DISABLE_TELEMETRY`
- `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`
- `AUTH_ENABLED`
- `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`
- `SESSION_SECRET`
- `GOOGLE_OAUTH_REDIRECT_URI`
- `SESSION_MAX_AGE_HOURS`
- `SECURE_COOKIES`
- `AUTH_COOKIE_DOMAIN`
- `ALLOWED_EMAIL_DOMAINS`
- `WS_ALLOWED_ORIGINS`
- `SESSION_CLEANUP_INTERVAL`

### N10. CLAUDE.md — Endpoints table missing several routes

**File:** `CLAUDE.md` (lines 81-87)
**Issue:** Missing endpoints:
- `GET /ws` (WebSocket for streaming)
- `GET /auth/login`, `GET /auth/callback`, `GET /auth/logout`
- `GET /auth/me`

### N11. CLAUDE.md — Key Interfaces list is incomplete

**File:** `CLAUDE.md` (lines 73-77)
**Issue:** Missing interfaces:
- `Broadcaster` (`internal/reviewer/broadcaster.go`) — allows worker to push events to WebSocket subscribers

### N12. roadmap.md — Priority 7 "Review History and Metrics" is implemented

**File:** `docs/roadmap.md` (lines 162-176)
**Issue:** This entire feature (SQLite storage, `GET /reviews`, `GET /reviews/:run_id`, metrics) is now fully implemented. It should be marked as done or removed.

### N13. roadmap.md — Priority 2 "Review Timeout" and Priority 3 "Retry" are implemented

**File:** `docs/roadmap.md` (lines 81-108)
**Issue:** Both timeout (`MAX_REVIEW_DURATION`, default 600s) and retry with exponential backoff (`MAX_RETRIES`, default 2) are fully implemented in `worker.go`.

### N14. staging-setup.md — Missing `review-data` volume

**File:** `docs/references/staging-setup.md` (lines 54-63)
**Issue:** The settings table mentions `review-logs` volume but omits `review-data:/app/data` which exists in `docker-compose.yml`.

### N15. dev-setup.md — Project structure is incomplete

**File:** `docs/references/dev-setup.md` (lines 133-149)
**Issue:** Missing many existing directories and files: `internal/auth/`, `internal/storage/`, `web/`, `tests/`, `.golangci.yml`, `Makefile`, `go.mod`, `go.sum`.

### N16. dev-setup.md — Logs section inaccurate

**File:** `docs/references/dev-setup.md` (lines 127-131)
**Issue:** States "3 compressed backups" for log rotation. The actual `logger.go` uses `MaxBackups: 3` which is correct, but also states the log file is at `/app/logs/review.log`. The code also saves individual review output files to `/app/logs/reviews/` which is not documented.

---

## Acceptable Items Confirmed

1. **File paths:** All referenced file paths in `dev-setup.md`, `staging-setup.md`, and `prod-setup.md` exist (`.env.example`, `Dockerfile`, `docker-compose.yml`, `docker-compose.staging.yml`, `docker-compose.prod.yml`, `config/.claude/settings.json`, `config/.claude/skills/pr-review/SKILL.md`, `.github/workflows/review.yml`).

2. **Docker Compose overlay names:** All three compose files (`docker-compose.yml`, `docker-compose.staging.yml`, `docker-compose.prod.yml`) exist and are referenced correctly.

3. **Docker Compose commands:** All `docker compose -f ... up --build -d` commands in staging and prod docs are syntactically correct and match the `Makefile` compose arguments.

4. **`.env.example` variable names:** All active (non-commented) variables in `.env.example` are read by the code in `main.go`.

5. **No real secrets:** No tracked file contains real secrets. All examples use placeholder values (`your-webhook-secret-here`, `your-auth-token-here`, `your-github-pat-here`).

6. **No private infrastructure:** No hardcoded IPs or references to specific private infrastructure. Cloud providers (AWS, GCP, Cloudflare) appear only as deployment options in `prod-setup.md`, which is acceptable.

7. **`.gitignore` coverage:** `.env` is listed in `.gitignore` (line 33), confirming secrets won't be committed.

8. **Screenshot files:** `docs/images/github-pull-request-comment.jpg` and `docs/images/review-details-page.jpg` exist at documented paths.

9. **PRD.md API spec:** `POST /review` endpoint matches actual handler behavior — same header auth, same request/response models, same error codes.

10. **api-documentation.md:** Request/response models match the Go structs (`ReviewPayload`, `AcceptResponse`, `ErrorResponse`, `ReviewRecord`, `Metrics`, `ListReviewsResponse`).

11. **api-documentation.md — Status values:** Match `storage.go` constants (`pending`, `running`, `completed`, `failed`, `timed_out`, `cancelled`).

12. **api-documentation.md — Conclusion values:** Match `storage.go` constants (`success`, `failure`, `timed_out`, `cancelled`).

13. **api-documentation.md — Retry behavior:** Matches `isTransientError()` in `worker.go` — same patterns, same exit codes, same exponential backoff.

14. **PRD.md — Error handling table:** Matches handler behavior (401 for bad secret, 400 for bad payload, 405 for wrong method).

15. **PRD.md — Log rotation config:** Matches `logger.go` (10MB max, 7 days retention, compressed, 3 backups).

16. **prod-setup.md — Health check:** Matches `docker-compose.prod.yml` (`curl -f http://localhost:8080/review` every 30s, 3 retries, 5s timeout).

17. **prod-setup.md — Resource limits:** Match `docker-compose.prod.yml` (2 CPU, 2G memory, 0.5 CPU reservation, 512M memory reservation).

18. **prod-setup.md — Staging vs prod comparison:** Matches actual compose files.

19. **staging-setup.md — Resource limits:** Match `docker-compose.staging.yml` (2 CPU, 4G memory, 0.5 CPU reservation, 512M memory reservation).

20. **staging-setup.md — Restart policy:** `on-failure:5` matches `docker-compose.staging.yml`.

21. **GitHub Action workflow:** `.github/workflows/review.yml` matches the PRD's example, with minor improvement (`reopened` trigger type added, `-fsSL` curl flags).

22. **config/.claude/skills/pr-review/SKILL.md:** Skill name, description, and review steps are consistent with actual implementation.

23. **CLAUDE.md — Two `.claude/` directories distinction:** Accurately documented and matches the actual project structure.

24. **CLAUDE.md — `net/http` only:** Confirmed — the code uses `http.NewServeMux()` with Go 1.22+ enhanced ServeMux (no router framework). Note: `gorilla/websocket` is used for WebSocket support, not routing.

25. **CLAUDE.md — No `pkg/` directory:** Confirmed — all code is in `internal/`, `cmd/`, `web/`, and `tests/`.

26. **CLAUDE.md — Ephemeral execution:** Confirmed — `os.MkdirTemp("", "nano-review-*")` with `defer os.RemoveAll(dir)`.

27. **prod-setup.md — Security checklist:** All items are accurate and aligned with actual implementation.

---

## File-by-File Findings

### `docs/PRD.md`
- **Critical:** Non-goals section is outdated (C3), Dockerfile is outdated (C4), `.claude/settings.json` is outdated (C5)
- **Non-Critical:** SKILL.md embed is outdated (N1), worker command is outdated (N2), clone step missing `--single-branch` (N3), directory structure incomplete (N4)
- **Acceptable:** API spec, error handling, logging config, security model

### `docs/api-documentation.md`
- **Non-Critical:** Auth on read endpoints is inaccurate (N8)
- **Acceptable:** All request/response models, status/conclusion values, retry behavior, error responses

### `docs/roadmap.md`
- **Non-Critical:** Priority 2 (Timeout), Priority 3 (Retry), and Priority 7 (History & Metrics) are implemented (N12, N13)

### `docs/references/dev-setup.md`
- **Non-Critical:** Uses `exec` instead of `run` (N5), missing env vars (N6), missing endpoints (N7), incomplete project structure (N15), logs section incomplete (N16)

### `docs/references/staging-setup.md`
- **Non-Critical:** Missing `review-data` volume (N14)

### `docs/references/prod-setup.md`
- **Acceptable:** All commands, resource limits, health check, SSL options, security checklist

### `CLAUDE.md`
- **Critical:** Claims no Go code exists (C1), source layout is incomplete (C2)
- **Non-Critical:** Missing env vars (N9), missing endpoints (N10), missing interfaces (N11)
- **Acceptable:** Build commands, Docker section, key decisions, file writing direction, two `.claude/` distinction

### `.env.example`
- **Acceptable:** All variable names match code, no real secrets, placeholder values

### `config/.claude/skills/pr-review/SKILL.md`
- **Acceptable:** Skill definition is comprehensive and matches how it's invoked in `worker.go`
