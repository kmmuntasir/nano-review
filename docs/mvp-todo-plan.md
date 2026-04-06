# Nano Review MVP — Step-by-Step Todo Plan

## Overview

Build a single-endpoint Go webhook server (`POST /review`) that validates an incoming webhook, returns `200 {"status": "accepted", "run_id": "<uuid>"}` immediately, and spawns an async goroutine that clones the repo, runs Claude Code CLI, and cleans up.

**Current state**: Greenfield. Zero Go source code. Only documentation, rules, and `.gitignore` exist.

**Two external deps only**: `github.com/google/uuid`, `gopkg.in/natefinsh/lumberjack.v2`

---

## Round 1 — Core Implementation (3 Parallel Tracks)

Each track owns independent packages/directories. Run as parallel worktree agents.

---

### Track A: API Layer & Core Types

**Owns**: `go.mod`, `internal/api/`

- [x] **A1. Initialize Go module**
  - `go mod init github.com/kmmuntasir/nano-review`
  - Pin Go 1.23+ in `go.mod`
  - `go get github.com/google/uuid`
  - `go get gopkg.in/natefinish/lumberjack.v2`

- [x] **A2. Create `internal/api/errors.go`**
  - Sentinel errors: `ErrInvalidPayload`, `ErrUnauthorized`, `ErrCloneFailed`
  - `var` block with `errors.New()`

- [x] **A3. Create `internal/api/models.go`**
  - `ReviewPayload` struct: `RepoURL`, `PRNumber`, `BaseBranch`, `HeadBranch` (json tags)
  - `AcceptResponse` struct: `Status`, `RunID` (json tags)
  - `ErrorResponse` struct: `Error` (json tag)
  - `ValidatePayload(p ReviewPayload) error` — returns `ErrInvalidPayload` if any field missing/zero

- [x] **A4. Create `internal/api/models_test.go`**
  - Table-driven tests: valid payload, missing repo URL, missing PR number, missing base branch, missing head branch, all fields missing
  - Standard `t.Errorf`, no assertion libraries

- [x] **A5. Create `internal/api/handler.go`**
  - `ReviewStarter` interface: `StartReview(ctx context.Context, p ReviewPayload) (string, error)`
  - `HandleReview(secret string, starter ReviewStarter) http.HandlerFunc`
  - Validates: POST only → `X-Webhook-Secret` header → JSON decode → `ValidatePayload` → `starter.StartReview()`
  - Returns `AcceptResponse` (200) or `ErrorResponse` (400/401/405/500)
  - `writeJSON(w, status, v)` helper

- [x] **A6. Create `internal/api/handler_test.go`**
  - Table-driven tests with `httptest.NewRecorder`
  - Manual mock for `ReviewStarter`
  - Cases: valid 200, wrong method 405, missing secret 401, wrong secret 401, invalid JSON 400, missing fields 400, starter error 500
  - Target >80% handler coverage

- [x] **A7. Verify API package**
  - `rtk go test -race ./internal/api/` — all pass
  - `rtk go vet ./internal/api/` — clean

---

### Track B: Reviewer Worker & Logging

**Owns**: `internal/reviewer/`

- [x] **B1. Create `internal/reviewer/worker.go`**
  - `ClaudeRunner` interface: `Run(ctx, dir, args...) (output, exitCode, err)`
  - `Logger` interface: `Info(msg, keysAndValues...)`, `Error(msg, keysAndValues...)`, `With(keysAndValues...) Logger`
  - `Worker` struct: `claude ClaudeRunner`, `logger Logger`, `gitPath string`, `claudePath string`
  - `NewWorker(claude ClaudeRunner, logger Logger, gitPath, claudePath string) *Worker`
  - `StartReview(ctx, payload) (string, error)` — generates UUID, launches goroutine, returns runID
  - `processReview(ctx, runID, payload)` — async goroutine:
    1. `os.MkdirTemp("", "nano-review-*")`
    2. `defer os.RemoveAll(dir)`
    3. `git clone --branch <head_branch> --single-branch <repo_url> <dir>` via `os/exec`
    4. `<claudePath> -p "/pr-review"` via `ClaudeRunner`
    5. Log each step with `run_id`
    6. Handle errors at each step (log + continue to cleanup via defer)

- [x] **B2. Create `internal/reviewer/worker_test.go`**
  - Manual mock `ClaudeRunner` (configurable output/exitCode/error)
  - Manual mock `Logger` (track calls for verification)
  - Tests:
    - `StartReview` returns non-empty runID without blocking
    - `processReview` calls git clone with correct args
    - `processReview` calls ClaudeRunner with correct args
    - Cleanup runs on clone failure (temp dir removed)
    - Cleanup runs on Claude failure
    - Context cancellation propagates to git and claude commands
  - Use `t.TempDir()` for filesystem tests

- [x] **B3. Create `internal/reviewer/logger.go`**
  - `slogLogger` struct implementing `Logger` interface, wrapping `*slog.Logger`
  - `NewLogger(filePath string) (Logger, error)` — multi-writer: stdout (text) + file (JSON)
  - File via lumberjack: 10MB max, 7-day max age, 3 backups, compress rotated
  - `With()` returns new `slogLogger` with added key-value pairs

- [x] **B4. Verify reviewer package**
  - `rtk go test -race ./internal/reviewer/` — all pass
  - `rtk go vet ./internal/reviewer/` — clean

---

### Track C: Infrastructure & Configuration

**Owns**: `Dockerfile`, `docker-compose*.yml`, `.env.example`, `config/`, `.github/`

- [x] **C1. Create `config/.claude/settings.json`**
  - MCP server config for GitHub integration
  - Reference `GITHUB_PAT` and `ANTHROPIC_AUTH_TOKEN` env vars

- [x] **C2. Create `config/.claude/skills/pr-review/SKILL.md`**
  - Production skill definition that runs inside Docker
  - Copy from existing `.claude/skills/pr-review/SKILL.md` if available

- [x] **C3. Create `.env.example`**
  ```
  WEBHOOK_SECRET=your-webhook-secret-here
  ANTHROPIC_AUTH_TOKEN=your-auth-token-here
   GITHUB_PAT=your-github-pat-here
  PORT=8080
  CLAUDE_CODE_PATH=/usr/local/bin/claude
  MAX_TURNS=30
  ```

- [x] **C4. Create `Dockerfile`**
  - Stage 1 (builder): `golang:1.23-bookworm`, `CGO_ENABLED=0 go build -o /nano-review ./cmd/server`
  - Stage 2 (runtime): `ubuntu:24.04`, install `git`, `curl`, `openssh-client`, `ca-certificates`
  - Install Claude Code CLI in runtime stage
  - Copy binary from builder, copy `config/.claude/` → `/app/.claude/`
  - Create `/app/logs/`, expose PORT, `ENTRYPOINT ["/nano-review"]`

- [x] **C5. Create Docker Compose files**
  - `docker-compose.yml` — dev: build from Dockerfile, env file, `review-logs` volume at `/app/logs`
  - `docker-compose.staging.yml` — overlay: restart policy, resource limits
  - `docker-compose.prod.yml` — overlay: production limits, health check

- [x] **C6. Create `.github/workflows/review.yml`**
  - Trigger: `pull_request` (opened, synchronize, reopened)
  - Extract PR metadata, POST to Nano Review endpoint as JSON
  - `WEBHOOK_SECRET` from GitHub Secrets

- [x] **C7. Verify infrastructure**
  - `rtk docker compose config` validates

---

## Round 2 — Integration & Wiring

Depends on all Round 1 tracks completing. Single agent on main branch.

- [x] **R2-1. Merge all Round 1 worktrees into main**
  - Rebase merge each worktree branch
  - Resolve any import conflicts (should be minimal — tracks own separate packages)

- [x] **R2-2. Create `cmd/server/main.go`**
  - Read env vars: `WEBHOOK_SECRET`, `PORT` (default 8080), `ANTHROPIC_AUTH_TOKEN`, `GITHUB_PAT`, `CLAUDE_CODE_PATH`, `MAX_TURNS`
  - Validate required env vars at startup — fail fast
  - Wire: `NewLogger()` → `NewWorker()` → `HandleReview()` → `http.NewServeMux()` → `http.Server{}`
  - Register: `mux.HandleFunc("POST /review", handler)`
  - Graceful shutdown: `SIGINT`/`SIGTERM` via `os.Signal`, `server.Shutdown()` with 10s timeout
  - Log startup and shutdown — thin main, no business logic

- [x] **R2-3. Run full verification**
  - `rtk go test -race ./...` — all pass
  - `rtk go vet ./...` — clean
  - `rtk go fmt ./...` — clean
  - `rtk go build -o /nano-review ./cmd/server` — succeeds

- [x] **R2-4. Update `.gitignore`**
  - Ensure: `*.exe`, `*.dll`, `*.so`, `*.dylib`, `*.test`, `*.out`, `/vendor/`, `.env`, `/app/logs/`

- [x] **R2-5. Docker build verification**
  - `rtk docker compose build` — succeeds

---

## Acceptance Criteria

- [x] `go test -race ./...` passes
- [x] `go vet ./...` clean
- [x] `go build ./cmd/server` succeeds
- [x] `docker compose build` succeeds
- [x] `docker compose config` validates
- [x] API handler coverage >80%
- [x] All code follows `internal/` layout, no `pkg/`
- [x] No `init()`, no `panic`, no `fmt.Println`, no assertion libraries
