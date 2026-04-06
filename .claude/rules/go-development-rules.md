# Go Development Rules

## General

This project is a Go 1.23+ microbackend that runs inside Docker. Follow [Effective Go](https://go.dev/doc/effective_go) and [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments) as primary references.

### Project Structure

Follow the standard Go project layout conventions:

```
cmd/server/main.go        # Entry point
internal/api/             # HTTP handlers (unexported, project-internal)
internal/reviewer/        # Review worker logic
config/.claude/           # Claude Code config copied into Docker image
```

- `internal/` packages cannot be imported by external projects — use this for all project-specific code.
- `cmd/` contains entry points. Keep `main.go` thin — just wiring dependencies and starting the server.
- No `pkg/` directory unless reusable libraries are needed (unlikely for this project).

### Package Conventions

- Package names: short, lowercase, no underscores (`api`, `reviewer`, not `api_server`).
- One package per directory.
- Avoid `util` or `helper` packages — put functions where they belong.
- Keep interfaces small and defined where they are consumed, not where they are implemented.

### Error Handling

- Always handle errors explicitly. Never use `_ = someFunc()`.
- Wrap errors with context: `fmt.Errorf("cloning repo %s: %w", url, err)`.
- Use `errors.Is` and `errors.As` for error inspection.
- HTTP handlers should return structured JSON error responses, not panic.

```go
// Good
if err := reviewer.StartReview(payload); err != nil {
    log.Error("failed to start review", "run_id", runID, "error", err)
}
```

### Logging

Use structured JSON logging. Logs are written to `/app/logs/` with rotation via lumberjack.

```go
type Logger interface {
    Info(msg string, keysAndValues ...any)
    Error(msg string, keysAndValues ...any)
    With(keysAndValues ...any) Logger
}
```

- Always include `run_id` in log entries related to a review.
- Key log events: webhook received, git clone started/completed/failed, Claude Code started/completed/failed, cleanup.
- Never use `fmt.Println` or `log.Println` — use the structured logger.

### Concurrency

- Use goroutines for async review processing (one per request in MVP).
- Use `context.Context` for cancellation and timeouts.
- Always defer cleanup (`os.RemoveAll`) — it must run regardless of success/failure.
- Use `sync.WaitGroup` if waiting on multiple goroutines.

### Environment Configuration

All config via environment variables. No config files, no flags (except what Docker passes).

| Variable | Required | Default |
|---|---|---|
| `PORT` | No | `8080` |
| `WEBHOOK_SECRET` | Yes | — |
| `ANTHROPIC_AUTH_TOKEN` | Yes | — |
| `GITHUB_PAT` | Yes | — |
| `CLAUDE_CODE_PATH` | No | auto-detected |
| `MAX_TURNS` | No | `30` |
| `ANTHROPIC_BASE_URL` | No | — |
| `API_TIMEOUT_MS` | No | — |
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | No | — |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | No | — |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | No | — |
| `CLAUDE_CODE_DISABLE_1M_CONTEXT` | No | — |

### Docker

- Multi-stage build: Go builder → minimal runtime image with `git`, `curl`, `openssh-client`, and Claude Code CLI.
- `CGO_ENABLED=0` for static binary.
- Docker Compose overlays: `docker-compose.yml` (dev), `docker-compose.staging.yml`, `docker-compose.prod.yml`.
- Log directory mounted as named volume: `review-logs:/app/logs`.

### HTTP Server

- Use the standard library `net/http` — no router frameworks for MVP (single endpoint).
- Validate webhook secret on every request.
- Return structured JSON responses with appropriate status codes (200, 400, 401, 500).
- Respond immediately (<1s), then process asynchronously.

### Security

- No secrets in code — all via environment variables.
- SSH key scoped to target repo (deploy key, read-only).
- GitHub PAT with minimal `repo` scope.
- Ephemeral temp directories — always force-delete after use.
