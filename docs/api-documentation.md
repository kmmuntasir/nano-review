# API Documentation

Base URL: `http://localhost:8080` (configurable via `PORT` environment variable)

All request and response bodies use `Content-Type: application/json`.

---

## POST /review

Start an asynchronous PR code review.

### Authentication

| Header | Required | Description |
|--------|----------|-------------|
| `X-Webhook-Secret` | Yes | Must match the `WEBHOOK_SECRET` environment variable |

### Request Model

`ReviewPayload`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repo_url` | `string` | Yes | Git repository URL. Supports HTTPS (`https://github.com/owner/repo.git`) and SSH (`git@github.com:owner/repo.git`) formats. |
| `pr_number` | `integer` | Yes | Pull request number. Must be non-zero. |
| `base_branch` | `string` | Yes | Target branch of the pull request. |
| `head_branch` | `string` | Yes | Source branch of the pull request. |

### Sample Request

```bash
curl -X POST http://localhost:8080/review \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Secret: your-webhook-secret" \
  -d '{
    "repo_url": "https://github.com/owner/repo.git",
    "pr_number": 42,
    "base_branch": "main",
    "head_branch": "feature/awesome-feature"
  }'
```

### Response Model

`AcceptResponse` (200 OK)

| Field | Type | Description |
|-------|------|-------------|
| `status` | `string` | Always `"accepted"`. |
| `run_id` | `string` | UUID identifying this review run. Use it to poll status via `GET /reviews/{run_id}`. |

### Sample Response (200 OK)

```json
{
  "status": "accepted",
  "run_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### Error Responses

| Status | Error | Description |
|--------|-------|-------------|
| 401 | `invalid or missing webhook secret` | The `X-Webhook-Secret` header is missing or does not match. |
| 400 | `invalid JSON body` | The request body is not valid JSON. |
| 400 | `invalid review payload: repo_url is required` | A required field is missing or invalid. Other variants: `pr_number is required`, `base_branch is required`, `head_branch is required`. |
| 405 | `method not allowed` | HTTP method is not POST. |
| 500 | `internal server error` | An unexpected server error occurred. |

### Notes

- The response returns immediately. The review runs asynchronously in a background goroutine.
- The repo is cloned into an ephemeral temp directory (`/tmp/nano-review-<run_id>/<repo>/`), which is force-deleted after the review completes or fails.
- Transient failures (rate limits, network errors) are retried up to `MAX_RETRIES` times (default: 2) with exponential backoff (1s, 2s, 4s, ...).
- The entire review is subject to a timeout of `MAX_REVIEW_DURATION` (default: 10 minutes).

---

## GET /reviews

List review records with optional filtering and pagination.

### Authentication

None required.

### Query Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `repo` | `string` | No | — | Filter by repository URL (exact match). |
| `status` | `string` | No | — | Filter by status. See [Status Values](#status-values). |
| `limit` | `integer` | No | — | Maximum number of results to return. |
| `offset` | `integer` | No | — | Number of results to skip (for pagination). |

### Sample Request

```bash
curl "http://localhost:8080/reviews?status=completed&limit=10"
```

```bash
curl "http://localhost:8080/reviews?repo=https://github.com/owner/repo.git&status=failed"
```

### Response Model

`ListReviewsResponse` (200 OK)

| Field | Type | Description |
|-------|------|-------------|
| `reviews` | `array` of `ReviewRecord` | List of review records matching the filter. |
| `count` | `integer` | Number of records in this page. |

### Sample Response (200 OK)

```json
{
  "reviews": [
    {
      "run_id": "550e8400-e29b-41d4-a716-446655440000",
      "repo": "https://github.com/owner/repo.git",
      "pr_number": 42,
      "base_branch": "main",
      "head_branch": "feature/awesome-feature",
      "status": "completed",
      "conclusion": "success",
      "duration_ms": 45123,
      "attempts": 1,
      "claude_output": "Review completed successfully...",
      "created_at": "2026-04-09T10:30:00Z",
      "completed_at": "2026-04-09T10:31:15Z"
    }
  ],
  "count": 1
}
```

### Error Responses

| Status | Error | Description |
|--------|-------|-------------|
| 500 | `internal server error` | A database or server error occurred. |

---

## GET /reviews/{run_id}

Retrieve a single review record by its run ID.

### Authentication

None required.

### Path Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `run_id` | `string` | Yes | UUID identifying the review run. |

### Sample Request

```bash
curl http://localhost:8080/reviews/550e8400-e29b-41d4-a716-446655440000
```

### Response Model

`ReviewRecord` (200 OK)

| Field | Type | Description |
|-------|------|-------------|
| `run_id` | `string` | Unique identifier for this review run (UUID). |
| `repo` | `string` | Repository URL. |
| `pr_number` | `integer` | Pull request number. |
| `base_branch` | `string` | Target branch. Omitted if empty. |
| `head_branch` | `string` | Source branch. Omitted if empty. |
| `status` | `string` | Current status of the review. See [Status Values](#status-values). |
| `conclusion` | `string` | Final outcome. Omitted if review has not completed. See [Conclusion Values](#conclusion-values). |
| `duration_ms` | `integer` | Total duration of the review in milliseconds. |
| `attempts` | `integer` | Number of execution attempts (including retries). |
| `claude_output` | `string` | Full Claude Code CLI output. Omitted if empty. |
| `created_at` | `string` | ISO 8601 timestamp when the review was created. |
| `completed_at` | `string` | ISO 8601 timestamp when the review finished. Omitted if the review is still pending or running. |

### Sample Response (200 OK)

```json
{
  "run_id": "550e8400-e29b-41d4-a716-446655440000",
  "repo": "git@github.com:owner/repo.git",
  "pr_number": 42,
  "base_branch": "main",
  "head_branch": "feature/awesome-feature",
  "status": "completed",
  "conclusion": "success",
  "duration_ms": 45123,
  "attempts": 1,
  "claude_output": "Review completed successfully...",
  "created_at": "2026-04-09T10:30:00Z",
  "completed_at": "2026-04-09T10:31:15Z"
}
```

### Error Responses

| Status | Error | Description |
|--------|-------|-------------|
| 400 | `run_id is required` | The `run_id` path parameter is missing. |
| 404 | `review not found` | No review exists with the given `run_id`. |
| 500 | `internal server error` | A database or server error occurred. |

---

## GET /metrics

Retrieve aggregate statistics about all reviews.

### Authentication

None required.

### Sample Request

```bash
curl http://localhost:8080/metrics
```

### Response Model

`Metrics` (200 OK)

| Field | Type | Description |
|-------|------|-------------|
| `total_reviews` | `integer` | Total number of reviews stored in the database. |
| `success_count` | `integer` | Reviews that completed successfully. |
| `failure_count` | `integer` | Reviews that failed after all retries. |
| `timed_out_count` | `integer` | Reviews that exceeded the maximum duration. |
| `cancelled_count` | `integer` | Reviews that were cancelled. |
| `avg_duration_ms` | `float` | Average duration in milliseconds across completed reviews. |
| `reviews_today` | `integer` | Number of reviews created today (UTC). |

### Sample Response (200 OK)

```json
{
  "total_reviews": 150,
  "success_count": 120,
  "failure_count": 15,
  "timed_out_count": 10,
  "cancelled_count": 5,
  "avg_duration_ms": 43256.7,
  "reviews_today": 23
}
```

### Error Responses

| Status | Error | Description |
|--------|-------|-------------|
| 500 | `internal server error` | A database or server error occurred. |

---

## Common Types

### ErrorResponse

Returned by all endpoints on error.

| Field | Type | Description |
|-------|------|-------------|
| `error` | `string` | Human-readable error message. |

```json
{
  "error": "invalid or missing webhook secret"
}
```

---

## Enumerations

### Status Values

| Value | Description |
|-------|-------------|
| `pending` | Review accepted, waiting to start. |
| `running` | Claude Code CLI is executing. |
| `completed` | Review finished successfully. |
| `failed` | Review failed (non-transient error or retries exhausted). |
| `timed_out` | Review exceeded `MAX_REVIEW_DURATION`. |
| `cancelled` | Review was cancelled (context cancelled). |

### Conclusion Values

| Value | Description |
|-------|-------------|
| `success` | Claude Code CLI completed successfully (exit code 0). |
| `failure` | Claude Code CLI failed after all retry attempts. |
| `timed_out` | Review exceeded the maximum allowed duration. |
| `cancelled` | Review was cancelled before completion. |

---

## Retry Behavior

Transient failures are automatically retried by the reviewer worker:

- **Exit code 2**: Claude Code CLI rate limit / overload errors.
- **Network errors**: DNS failures, connection refused, timeouts.
- **HTTP errors**: 429 (rate limit), 500/502/503 (server errors).
- **Output patterns**: `rate limit`, `too many requests`, `overloaded`, `502 bad gateway`, `503 service unavailable`, `500 internal server error`, `temporary failure`, `connection reset`, `econnreset`, `etimedout`.

| Setting | Default | Description |
|---------|---------|-------------|
| `MAX_RETRIES` | `2` | Maximum number of retry attempts. |
| Backoff strategy | Exponential | 1s, 2s, 4s, ... |

Context cancellation and timeouts are never retried.
