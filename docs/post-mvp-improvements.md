# Nano Review — Post-MVP Improvement Report

**Date**: 2026-04-23
**Baseline**: Production deployment running stable. All core features (webhook → queue → clone → Claude review → inline comments) operational.

---

## 1. High-Impact Features

### 1.1 GitHub Check Runs API

**Problem**: Reviews post comments but cannot gate merges. Branch protection rules can't enforce "review must pass."

**What to build**:
- Create a Check Run (`status: in_progress`) when review starts
- Annotate with findings as the review runs
- Set `conclusion: success | failure | timed_out` on completion
- Requires `head_sha` in webhook payload (add to `ReviewPayload`)

**Value**: Turns Nano Review from advisory to blocking — required for serious adoption.

**Effort**: Medium. GitHub Checks API is REST-based, well-documented. New `internal/github/checks.go` package.

---

### 1.2 Per-Repository Configuration

**Problem**: All repos share global `MAX_REVIEW_DURATION`, `MAX_RETRIES`, `MAX_CONCURRENT_REVIEWS`. Teams can't customize behavior.

**What to build**:
- `.nano-review.yml` file support in target repos
- Configurable fields: `focus_areas`, `skip_patterns`, `severity_threshold`, `max_turns`, `model`
- Repo config merges with global defaults (repo overrides win)
- Admin API (`POST /admin/repos`, `GET /admin/repos/{id}`) for centralized management
- New `repos` table in SQLite

**Effort**: High. Schema migration, config parsing, admin endpoints, UI.

---

### 1.3 GitHub App Mode (replaces PAT + webhook secret)

**Problem**: Current auth uses a personal access token (PAT) and shared webhook secret. Doesn't scale to multi-org or marketplace distribution.

**What to build**:
- GitHub App registration with webhook secret verification (HMAC-SHA256)
- Installation flow per-repo or per-org
- Short-lived installation tokens (auto-refresh, no PAT stored)
- Fine-grained permissions: `checks:write`, `pull_requests:write`, `contents:read`

**Value**: Required for open-source release or multi-tenant SaaS. Eliminates PAT rotation risk.

**Effort**: High. New `internal/github/app.go`, webhook signature verification rewrite, token refresh loop, installation storage.

---

## 2. Observability & Operations

### 2.1 Prometheus Metrics Export

**Problem**: `/metrics` endpoint returns JSON. No integration with standard monitoring stacks.

**What to build**:
- `GET /metrics` in Prometheus exposition format (or new `/metrics/prometheus`)
- Counters: `nano_reviews_total`, `nano_reviews_failed_total`
- Histograms: `nano_review_duration_seconds`, `nano_queue_wait_seconds`
- Gauges: `nano_queue_depth`, `nano_active_reviews`
- Standard `prometheus/client_golang` integration

**Effort**: Low. One new file, one dependency.

### 2.2 Structured Error Tracking

**Problem**: Errors logged to file only. No alerting on spikes.

**What to build**:
- Sentry integration (or compatible: Honeybadger, Bugsnag)
- Capture panics, unhandled errors, transient failures
- Attach `run_id`, `repo`, `pr_number` as Sentry tags

**Effort**: Low. Add `go.sentry.io/sentry` SDK, init in `main.go`.

### 2.3 Database Backup & Rotation

**Problem**: SQLite database has no backup strategy. Disk fill or corruption loses all review history.

**What to build**:
- Periodic SQLite backup via `VACUUM INTO` or `.backup` command
- Configurable retention (e.g., keep last 30 days of backups)
- S3/r2 upload for offsite storage
- Add `BACKUP_INTERVAL` and `BACKUP_RETENTION_DAYS` env vars

**Effort**: Medium. New goroutine in `main.go`, storage config.

---

## 3. Security Hardening

### 3.1 OAuth Login Rate Limiting

**Problem**: `GET /auth/login` has no rate limit. Vulnerable to OAuth abuse/DoS.

**Fix**: Add per-IP rate limiter middleware (e.g., `golang.org/x/time/rate` or in-memory sliding window). 10 requests/minute per IP.

**Effort**: Low. One middleware function.

### 3.2 WebSocket Origin Validation

**Problem**: If `WS_ALLOWED_ORIGINS` is unset, all origins are accepted. Production misconfiguration risk.

**Fix**: Make origin validation mandatory when `AUTH_ENABLED=true`. Fail startup if origins not configured.

**Effort**: Low. Validation in `main.go` startup.

### 3.3 Repository URL Allowlisting

**Problem**: `parseRepoURL` accepts any URL. Potential SSRF or resource exhaustion vector.

**Fix**: Whitelist `github.com` and configured enterprise GitHub hosts. Reject everything else.

**Effort**: Low. Add validated domains to config.

### 3.4 Secret Redaction Audit

**Problem**: GitHub PAT injected at runtime but error paths may leak it into logs.

**Fix**: Audit all `slog.Error` paths in `worker.go`. Add a `slog.Handler` wrapper that regex-redacts tokens/keys from all log output.

**Effort**: Medium. One slog wrapper, audit pass.

---

## 4. Testing Gaps

### 4.1 Unit Test Coverage

**Current state**: Integration tests cover OAuth, WebSocket auth, protected routes. Missing:

| Package | Coverage | Priority |
|---------|----------|----------|
| `internal/reviewer/worker.go` | 0% | Critical |
| `internal/reviewer/queue.go` | 0% | Critical |
| `internal/storage/sqlite.go` | 0% | High |
| `internal/reviewer/stream.go` | 0% | High |
| `internal/api/handler.go` | 0% | High |
| `internal/auth/auth.go` | Partial | Medium |

**Target**: >80% on business logic (`reviewer`, `storage`, `api`). Use `ClaudeRunner` mock for worker tests, in-memory SQLite for storage tests.

### 4.2 End-to-End Smoke Test

**Problem**: No automated test that sends a real webhook and verifies comment posted.

**What to build**: `tests/e2e/` with build tag `e2e`. Spins up Docker container, sends webhook with real PAT against a test repo, polls `/reviews/{id}` until complete, verifies GitHub comment exists.

**Effort**: High. Requires test repo with known PR, CI secrets management.

---

## 5. Architecture Improvements

### 5.1 Modularize `main.go`

**Problem**: `cmd/server/main.go` is ~483 lines — does env loading, DI, MCP config generation, route registration, graceful shutdown all in one function.

**Fix**: Extract into functions:
- `loadConfig() *Config`
- `newMCPConfig(pat string) string`
- `newRouter(cfg Config) *http.ServeMux`
- `startBackgroundTasks(cfg Config)`

**Effort**: Low. Pure refactor, no behavior change.

### 5.2 Review Result Parsing

**Problem**: Claude's raw text output is stored as-is. No structured representation of findings.

**What to build**:
- Parse Claude output into structured findings (file, line, severity, message)
- Store findings in new `findings` table linked to `reviews`
- API returns structured findings alongside raw output
- Dashboard renders findings as a checklist

**Value**: Enables severity filtering, metrics per category, and cleaner UI.

**Effort**: Medium. New table, parsing logic, API changes.

### 5.3 Review Deduplication

**Problem**: Same PR can trigger multiple reviews (push after review started, re-open PR).

**What to build**:
- Track `(repo, pr_number, head_sha)` as a unique key
- Cancel in-progress review if new commit pushed
- Or skip if review already completed for same SHA

**Effort**: Medium. Query before enqueue, cancel signal via context.

---

## 6. Frontend Enhancements

### 6.1 Review Filtering & Search

**Problem**: Dashboard lists reviews chronologically. No search by repo name, author, or date range.

**What to build**: Client-side filtering (repo name fuzzy search, status filter, date picker). Add query params for shareable filtered views.

**Effort**: Low-Medium.

### 6.2 Review Diff Viewer

**Problem**: Review output references files and lines but no way to see the actual diff.

**What to build**: Fetch diff from GitHub API (`GET /repos/{owner}/{repo}/pulls/{number}/files`), render inline alongside review comments.

**Effort**: Medium. New API endpoint, frontend diff rendering.

### 6.3 Mobile Responsive Polish

**Current state**: Functional but not optimized for mobile.

**What to build**: Responsive breakpoint tuning, touch-friendly controls, collapsible sidebar.

**Effort**: Low.

---

## 7. Distribution & Adoption

### 7.1 GitHub Action Marketplace

**Problem**: Users must set up their own GitHub Action workflow.

**What to build**:
- Publish a reusable GitHub Action (`uses: kmmuntasir/nano-review-action@v1`)
- Action sends webhook, polls for completion, sets check status
- One-click setup for consumers

**Effort**: Medium. New repo for the action, YAML workflow template.

### 7.2 Terraform/Helm Provider

**Problem**: Infrastructure setup is manual (Docker Compose files).

**What to build**: Terraform module for deploying on any cloud. Helm chart for Kubernetes.

**Effort**: High. Separate repos.

---

## 8. Performance

### 8.1 Metrics Query Caching

**Problem**: `GET /metrics` aggregates over all reviews. Slows as data grows.

**Fix**: Cache metrics result with TTL (5 min). Invalidate on new review completion.

**Effort**: Low. Add `sync.Mutex` + timestamp cache.

### 8.2 Database Pagination Hardening

**Problem**: `limit` param accepts up to 200 but no server-side hard cap beyond that.

**Fix**: Hard cap at 100. Return `X-Total-Count` header for pagination.

**Effort**: Low.

---

## Priority Matrix

| # | Feature | Impact | Effort | Priority |
|---|---------|--------|--------|----------|
| 1.1 | GitHub Check Runs | High | Medium | **P0** |
| 3.1 | OAuth Rate Limiting | High | Low | **P0** |
| 4.1 | Unit Test Coverage | High | Medium | **P0** |
| 3.2 | WebSocket Origin Mandatory | Medium | Low | **P1** |
| 3.3 | Repo URL Allowlisting | Medium | Low | **P1** |
| 2.1 | Prometheus Metrics | Medium | Low | **P1** |
| 5.3 | Review Deduplication | Medium | Medium | **P1** |
| 1.2 | Per-Repo Config | High | High | **P2** |
| 2.2 | Error Tracking | Medium | Low | **P2** |
| 5.1 | Modularize main.go | Low | Low | **P2** |
| 5.2 | Structured Findings | Medium | Medium | **P2** |
| 6.1 | Dashboard Search/Filter | Medium | Low | **P2** |
| 1.3 | GitHub App Mode | High | High | **P3** |
| 2.3 | Database Backup | Medium | Medium | **P3** |
| 7.1 | GitHub Action Marketplace | High | Medium | **P3** |
| 6.2 | Diff Viewer | Medium | Medium | **P3** |
| 4.2 | E2E Smoke Test | Medium | High | **P3** |
| 7.2 | Terraform/Helm | Medium | High | **P4** |

---

## Summary

The MVP is architecturally solid — clean interfaces, proper concurrency, good error handling. The biggest gaps aren't in code quality but in **product capability** (Check Runs, per-repo config) and **operational maturity** (monitoring, backup, test coverage).

Recommended next phase (P0 + P1): ~2-3 weeks of focused work to harden security, add Check Runs API, and get unit test coverage above 80%. That makes Nano Review production-grade for serious team adoption.
