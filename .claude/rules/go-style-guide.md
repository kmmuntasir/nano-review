# Go Style Guide

## Formatting

- Use `gofmt` or `go fmt` — no configuration needed, it's the standard.
- Line length: Go has no strict limit, but keep lines readable (~120 chars practical max).
- Tabs for indentation, spaces for alignment.
- Trailing commas in composite literals are required by `gofmt`.

## Naming Conventions

Follow [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments):

### Packages
- Short, lowercase, no underscores: `api`, `reviewer`, `config`.
- No `util`, `common`, `base` packages.

### Exported vs Unexported
- Exported (public): `PascalCase` — visible outside the package.
- Unexported (private): `camelCase` — visible only within the package.
- All types, functions, constants, and variables follow this rule.

```go
// Exported
type ReviewPayload struct { ... }
func StartReview(ctx context.Context, p ReviewPayload) error

// Unexported
type reviewResult struct { ... }
func validatePayload(p ReviewPayload) error
```

### Interfaces
- Single-method interfaces: method name + `-er` suffix (e.g., `Runner`, `Closer`, `Logger`).
- Multi-method interfaces: descriptive noun (e.g., `ReviewStarter`, `ClaudeRunner`).

### Acronyms
- Keep acronyms consistent case: `URL`, `ID`, `HTTP`, `API` (all caps).
- Example: `repoURL`, `prNumber`, `apiHandler`, not `repoUrl`, `prNum`.

### Constants
- Group related constants with `iota` or const blocks.
- No `ALL_CAPS` for Go constants — use `PascalCase` for exported, `camelCase` for unexported.

```go
const (
    DefaultPort     = "8080"
    DefaultMaxTurns = 30
)
```

### Error Variables

Use `var` for sentinel errors:

```go
var (
    ErrInvalidPayload = errors.New("invalid review payload")
    ErrCloneFailed    = errors.New("git clone failed")
    ErrUnauthorized   = errors.New("invalid or missing webhook secret")
)
```

## Import Organization

`goimports` (or `gofmt`) handles this automatically:

```go
import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "os/exec"

    "github.com/google/uuid"
)
```

1. Standard library
2. Third-party packages
3. Local project packages (not applicable here — single module)

Groups separated by blank lines. `gofmt` enforces this.

## Code Structure

### Functions

- Keep functions short and focused (<50 lines).
- Return early to reduce nesting.
- Receivers: use pointer receivers for types that need mutation, value receivers for small immutable types.

```go
func (w *Worker) StartReview(ctx context.Context, p ReviewPayload) (string, error) {
    if p.RepoURL == "" {
        return "", ErrInvalidPayload
    }

    runID := uuid.New().String()
    go w.processReview(ctx, runID, p)
    return runID, nil
}
```

### Error Handling

```go
// Wrap with context
if err := gitClone(ctx, repoURL, dir); err != nil {
    return fmt.Errorf("git clone %s into %s: %w", repoURL, dir, err)
}

// Sentinel errors
if errors.Is(err, ErrUnauthorized) {
    http.Error(w, `{"error":"invalid or missing webhook secret"}`, http.StatusUnauthorized)
    return
}
```

### Structs

- Group related fields, separate groups with blank lines.
- Use field tags for JSON serialization.

```go
type ReviewPayload struct {
    RepoURL    string `json:"repo_url"`
    PRNumber   int    `json:"pr_number"`
    BaseBranch string `json:"base_branch"`
    HeadBranch string `json:"head_branch"`
}
```

### String Formatting

- Use `%s`, `%d`, `%v` — not `+` concatenation for complex strings.
- Use `%w` for error wrapping.
- Use `%q` for quoted strings in error messages.

## Project-Specific Patterns

### Structured Logging

```go
slog.Info("review accepted",
    "run_id", runID,
    "pr_number", payload.PRNumber,
    "repo", payload.RepoURL,
)

slog.Error("claude execution failed",
    "run_id", runID,
    "exit_code", exitCode,
    "error", err,
)
```

### HTTP Handler Pattern

```go
func HandleReview(secret string, starter ReviewStarter) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
            return
        }

        if r.Header.Get("X-Webhook-Secret") != secret {
            writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "invalid or missing webhook secret"})
            return
        }

        var payload ReviewPayload
        if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
            writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON body"})
            return
        }

        runID, err := starter.StartReview(r.Context(), payload)
        if err != nil {
            writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
            return
        }

        writeJSON(w, http.StatusOK, AcceptResponse{Status: "accepted", RunID: runID})
    }
}
```

## Linting

```bash
go vet ./...           # Standard static analysis
go fmt ./...           # Auto-format
golangci-lint run      # Extended linting (if configured)
```

## Things to Avoid

- `init()` functions unless absolutely necessary.
- Global mutable state — prefer dependency injection.
- `interface{}` or `any` when a specific type exists.
- Getter/setter methods (`GetFoo()` / `SetFoo()`) — use exported fields or methods with descriptive names.
- `reflect` package unless there's no alternative.
- `panic` in library/server code — return errors instead.
