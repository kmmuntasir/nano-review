# Go Testing Rules

## Overview

Go has testing built into the standard library. Use `testing` package for unit tests. Consider `testcontainers-go` for integration tests involving Docker.

## Test Organization

```
internal/api/
    handler.go
    handler_test.go        # Co-located with source
internal/reviewer/
    worker.go
    worker_test.go
```

Tests live alongside the code they test, in `*_test.go` files within the same package.

## Unit Tests

### Table-Driven Tests

The preferred pattern for most test scenarios:

```go
func TestValidatePayload(t *testing.T) {
    tests := []struct {
        name    string
        payload ReviewPayload
        wantErr bool
    }{
        {
            name:    "valid payload",
            payload: ReviewPayload{RepoURL: "git@github.com:owner/repo.git", PRNumber: 42, BaseBranch: "main", HeadBranch: "feature/x"},
            wantErr: false,
        },
        {
            name:    "missing repo URL",
            payload: ReviewPayload{PRNumber: 42, BaseBranch: "main", HeadBranch: "feature/x"},
            wantErr: true,
        },
        {
            name:    "missing PR number",
            payload: ReviewPayload{RepoURL: "git@github.com:owner/repo.git", BaseBranch: "main", HeadBranch: "feature/x"},
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidatePayload(tt.payload)
            if (err != nil) != tt.wantErr {
                t.Errorf("ValidatePayload() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### Interfaces and Mocks

Define interfaces where you need to swap implementations. Use manual mocks or `gomock`/`mockgen` for generated mocks.

```go
// In production code — define interface at the consumer
type ClaudeRunner interface {
    Run(ctx context.Context, dir string, args ...string) (output string, exitCode int, err error)
}

// In tests — manual mock
type mockClaudeRunner struct {
    output   string
    exitCode int
    err      error
}

func (m *mockClaudeRunner) Run(_ context.Context, _ string, _ ...string) (string, int, error) {
    return m.output, m.exitCode, m.err
}
```

### Testing HTTP Handlers

Use `httptest` for handler tests:

```go
func TestReviewHandler(t *testing.T) {
    body := `{"repo_url":"git@github.com:owner/repo.git","pr_number":42,"base_branch":"main","head_branch":"feature/x"}`

    req := httptest.NewRequest(http.MethodPost, "/review", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-Webhook-Secret", "test-secret")
    w := httptest.NewRecorder()

    handler(w, req)

    resp := w.Result()
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        t.Errorf("expected status 200, got %d", resp.StatusCode)
    }

    var result ReviewResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        t.Fatal(err)
    }
    if result.Status != "accepted" {
        t.Errorf("expected status 'accepted', got '%s'", result.Status)
    }
}
```

### Testing with Temp Directories

Use `t.TempDir()` for ephemeral test fixtures — automatically cleaned up:

```go
func TestCloneRepo(t *testing.T) {
    dir := t.TempDir()
    // ... test operations using dir ...
    // dir is automatically removed after test
}
```

## Integration Tests

Tests that require Docker or external services should use build tags:

```go
//go:build integration

package reviewer_test

import "testing"

func TestEndToEndReview(t *testing.T) {
    // Requires running Docker daemon
}
```

Run with: `go test -tags=integration ./...`

## Running Tests

> **Go is not installed on the host machine.** All test commands must run inside the Docker container via `docker compose run --rm` (targets the builder stage, which has the Go toolchain). See `docs/references/dev-setup.md` for full setup instructions.

```bash
# All tests
docker compose run --rm nano-review go test ./...

# Specific package
docker compose run --rm nano-review go test ./internal/api/

# Verbose with coverage
docker compose run --rm nano-review go test -v -cover ./...

# Coverage profile
docker compose run --rm nano-review go test -coverprofile=coverage.out ./...
docker compose run --rm nano-review go tool cover -html=coverage.out -o coverage.html

# Integration tests only
docker compose run --rm nano-review go test -tags=integration ./...
```

## Best Practices

### Test Naming

- Test functions: `TestFuncName` (e.g., `TestValidatePayload`, `TestStartReview`).
- Sub-tests: descriptive strings in `t.Run()` (e.g., `"returns 401 on missing secret"`).

### Helpers

Extract common setup into `testhelper.go` (non-`_test.go` so it's available across test files):

```go
// internal/api/testhelper.go
package api

func newTestServer(secret string, reviewer ReviewStarter) *httptest.Server {
    mux := http.NewServeMux()
    RegisterHandlers(mux, secret, reviewer)
    return httptest.NewServer(mux)
}
```

### Assertions

Use the standard library `t.Error`, `t.Errorf`, `t.Fatal`, `t.Fatalf`. Avoid assertion libraries for MVP — they add dependencies for marginal convenience.

### Race Detection

Always run tests with race detector:

```bash
go test -race ./...
```

## Coverage Targets

- Business logic (reviewer, handler): >80%
- Models/validation: 100%
- Integration: critical flows only
