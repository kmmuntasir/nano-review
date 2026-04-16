# Nano Review - Future Features Roadmap

## Priority 1: PR Status Checks via Check Runs API

**Problem:** The GitHub Action returns HTTP 200 immediately, so the PR pipeline always shows "success" regardless of the review outcome. Branch protection rules cannot gate merges on the AI review result.

**Solution:** Use the [GitHub Check Runs API](https://docs.github.com/en/rest/checks/runs) to create a check run when a review starts, keep it in progress, and mark it completed with a conclusion when the review finishes.

### Flow

```
POST /review received
       |
       v
  Create Check Run (status: "in_progress")   <-- gates branch protection
       |
       v
  Clone repo, run Claude Code review
       |
       v
  Update Check Run (status: "completed")     <-- unblocks branch protection
       +-- conclusion: "success"            (no issues found)
       +-- conclusion: "failure"            (issues found / changes requested)
       +-- conclusion: "cancelled"          (review aborted)
       +-- conclusion: "timed_out"          (exceeded max duration)
```

### API Calls

1. **Review start** — Create check run via GitHub API:
   ```
   POST /repos/{owner}/{repo}/commits/{head_sha}/check-runs
   {
     "name": "nano-review",
     "status": "in_progress",
     "head_sha": "<head_commit_sha>",
     "started_at": "<timestamp>"
   }
   ```

2. **Review end** — Update the same check run:
   ```
   PATCH /repos/{owner}/{repo}/check-runs/{check_run_id}
   {
     "status": "completed",
     "conclusion": "success" | "failure" | "cancelled" | "timed_out",
     "completed_at": "<timestamp>",
     "output": {
       "title": "Nano Review completed",
       "summary": "<review summary from Claude>",
       "text": "<detailed findings>"
     }
   }
   ```

### Implementation Notes

- The `repo_url` in the webhook payload already contains `owner/repo`. The `head_sha` can be obtained from `git rev-parse HEAD` after cloning, or passed in the webhook payload.
- The GitHub MCP server has tools for check runs (`create_check_run`, `update_check_run`). Alternatively, the Go worker can call the GitHub REST API directly using the `GITHUB_PAT`.
- Branch protection can be configured to require the `nano-review` check to pass before merging.
- The existing `GITHUB_PAT` with `repo` scope has sufficient permissions for check runs.

### Payload Change

The webhook payload should include `head_sha` to avoid an extra API call:

```json
{
  "repo_url": "https://github.com/owner/repo.git",
  "pr_number": 42,
  "base_branch": "main",
  "head_branch": "feature/add-auth",
  "head_sha": "a1b2c3d4e5f6..."
}
```

The GitHub Action already has access to this: `${{ github.event.pull_request.head.sha }}`.

---

## Priority 2: Review Timeout and Cancellation

**Problem:** A Claude Code review could hang indefinitely (unresponsive API, complex PR, agent loop). There is no timeout in the MVP.

**Solution:** Add a configurable review timeout. If the review exceeds `MAX_REVIEW_DURATION` (default: 5 minutes), terminate the Claude Code process and mark the check run as `timed_out`.

### Status: Implemented

Review timeout is fully implemented in `internal/reviewer/worker.go`. The worker creates a context with timeout via `context.WithTimeout` and enforces it during Claude Code execution. Configured via the `MAX_REVIEW_DURATION` environment variable with a default of 600 seconds (10 minutes) defined by `DefaultMaxReviewDuration`. On timeout, the review is marked as failed with an appropriate log entry.

### Scope

- Configurable via `MAX_REVIEW_DURATION` environment variable (default: `300` seconds).
- Use `context.WithTimeout` in the Go worker to kill the process group.
- Update check run with `timed_out` conclusion on timeout.
- Log the timeout with the run ID and PR number.

---

## Priority 3: Retry on Transient Failures

**Problem:** Claude Code CLI or the Anthropic API may fail with transient errors (rate limits, network blips). The MVP has no retry logic.

**Solution:** Add exponential backoff retry for known transient failures.

### Status: Implemented

Retry logic with exponential backoff is fully implemented in `internal/reviewer/worker.go`. The worker retries failed reviews with `time.Duration(1<<uint(attempt)) * time.Second` backoff between attempts. Configured via the `MAX_RETRIES` environment variable with a default of 2 defined by `DefaultMaxRetries`. Only transient errors are retried; deterministic failures and timeouts skip retry.

### Scope

- Retry up to `MAX_RETRIES` (default: 2) with backoff.
- Only retry on known transient errors (HTTP 429, 500, 502, 503, network timeouts).
- Do not retry on deterministic failures (invalid repo URL, auth errors, bad payload).
- Log each retry attempt with the run ID and error.

---

## Priority 4: Review Queue and Concurrency Control

**Problem:** The MVP spawns unlimited goroutines. A burst of PRs could exhaust system resources (disk I/O, memory, API rate limits).

**Solution:** Add a bounded review queue with configurable concurrency.

### Scope

- Use a buffered channel or worker pool pattern in Go.
- `MAX_CONCURRENT_REVIEWS` environment variable (default: `3`).
- Return HTTP 202 (`queued`) with a `retry_after` hint when the queue is full.
- Expose a `GET /health` endpoint with queue depth and active review count.
- Consider a persistent queue (e.g., SQLite) for durability across restarts (lower priority).

---

## Priority 5: Multi-Repository Support

**Problem:** Each Nano Review deployment should support multiple repositories with per-repo configuration.

### Status: Partially Implemented

The core multi-repo foundation is in place:
- `ReviewPayload` includes `repo_url` — the worker accepts any repository URL via `parseRepoURL()` in `internal/reviewer/worker.go`.
- Reviews are stored in SQLite with a `repo` column, enabling per-repo queries.
- `GET /reviews` supports filtering by `repo` query parameter.
- No per-repo hardcoded configuration — all repos use global settings.

### Remaining Scope

- Per-repo settings: `max_turns`, `max_review_duration`, `review_rules`, `enabled`/`disabled`.
- Move per-repo configuration to database or config file keyed by `owner/repo`.
- Admin API (`POST /repos`, `GET /repos`, `DELETE /repos/:id`) for managing repositories.
- Start with an in-memory config file, evolve to SQLite if persistence is needed.

---

## Priority 6: Configurable Review Rules

**Problem:** The review prompt is hardcoded in `pr-review.md`. All repositories get the same review behavior.

**Solution:** Allow per-repository or per-organization review rule configuration.

### Scope

- Support a `.nano-review.yml` file in the target repository (similar to `.eslintrc`, `tsconfig.json`).
- Configuration options:
  - `focus_areas`: Which categories to review (correctness, security, performance, maintainability).
  - `skip_patterns`: Glob patterns for files/directories to skip (e.g., `vendor/`, `*.generated.*`).
  - `language`: Hints about the primary language for better context.
  - `severity_threshold`: Minimum severity to post as inline comment vs summary.
- Fall back to default rules if no config file is present.
- Read the config file from the cloned repo before launching Claude Code, and inject it into the prompt.

---

## Priority 7: Review History and Metrics

**Problem:** No visibility into past reviews. No way to track review quality or measure improvement over time.

**Solution:** Persist review results and expose basic metrics.

### Status: Implemented

Review history and metrics are fully implemented. SQLite storage with WAL mode is implemented in `internal/storage/sqlite.go`, persisting review records with `run_id`, `repo`, `pr_number`, `status`, `conclusion`, `duration_ms`, `created_at`, and `claude_output`. Three read endpoints are implemented in `internal/api/handler.go`: `GET /reviews` (list with query params for `repo`, `status`, `limit`, `offset`), `GET /reviews/{run_id}` (single review detail), and `GET /metrics` (aggregate stats including success rate, average duration, and reviews today).

### Scope

- Store review results in SQLite (lightweight, no external dependency).
- Schema: `reviews` table with `run_id`, `repo`, `pr_number`, `status`, `conclusion`, `duration_ms`, `created_at`, `claude_output`.
- Simple read-only API: `GET /reviews`, `GET /reviews/:run_id`.
- Basic metrics: average review duration, success/failure rate, reviews per day.
- No dashboard UI in the near term — API-only is sufficient.

---

## Priority 8: Notification Support

**Problem:** Review results are only visible on the PR. Team members have no proactive notification channel.

**Solution:** Send notifications to external services on review completion.

### Scope

- Webhook notification: Fire a configurable webhook URL on review completion with a JSON payload containing the review result.
- Slack notification: Post a summary message to a Slack channel via webhook.
- GitHub comment is already the primary notification channel — this adds optional supplementary channels.
- Configuration via environment variables: `NOTIFICATION_WEBHOOK_URL`, `SLACK_WEBHOOK_URL`.
- Keep it simple — no template engine, just a structured JSON payload.
