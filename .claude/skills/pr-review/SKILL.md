---
name: pr-review
description: Comprehensive Go PR review with concurrency safety, error handling, Docker awareness, security checks, and code quality assessment. Use when user requests to review a pull request or compare branches for code review.
---

# PR Review Skill

When the user requests a **PR review** or to **compare branches**:

### Branch Defaults

- **Source branch**: The current local branch. Determine with `git branch --show-current`.
- **Target branch**: `main`, unless the user explicitly specifies a different branch.
- If the user specifies both branches, use those values.

### Pre-Review: Branch Synchronisation

Before starting the review, both branches must be up-to-date and the source must be rebased onto the target (this project uses **Rebase and Merge** on GitHub).

**Standard mode** (online):

```bash
# 1. Fetch all remotes
git fetch --all

# 2. Reset target to origin
git checkout <target-branch> && git reset --hard origin/<target-branch>

# 3. Reset source to origin
git checkout <source-branch> && git reset --hard origin/<source-branch>

# 4. Rebase source onto target
git rebase <target-branch>
```

**Offline mode**: If the user says **"offline"** when invoking this skill, skip steps 1-3 entirely. Only run the rebase (step 4) against the local copy of the target branch. This allows reviewing purely local state without network access.

**Conflict handling**: If the rebase in step 4 produces merge conflicts, **stop the entire review**. Abort the rebase (`git rebase --abort`), inform the user of the conflicts, and do not proceed with any review steps.

**If rebase succeeds**: Proceed to the review steps below.

### Parallel Subagent Strategy

This review can be accelerated using **up to 3 parallel subagents** (via the `Agent` tool). Instead of processing everything sequentially in the main context, split independent review tasks across subagents to save context window and speed up the process. Example parallelisation:

| Subagent | Scope | Agent Type |
|----------|-------|------------|
| 1 | Diff analysis + architecture review | `general-purpose` |
| 2 | Go-specific checks (concurrency, error handling, security) | `general-purpose` |
| 3 | Test coverage assessment + code quality checklist | `general-purpose` |

**When to parallelise:** Always use parallel subagents when the diff is non-trivial (more than a few files). For tiny diffs (1-2 files, cosmetic changes), a single-pass review is fine.

**How to parallelise:** Launch all independent subagents in a single message using multiple `Agent` tool calls. Each subagent should receive the diff (via `git diff`) and its specific review scope. After all subagents return, synthesize their findings into the final review summary (step 6).

## 1. Run Complete Diff

Compare the source branch against the target branch and analyze the **actual code changes**, not just commit messages.

```bash
git diff target..source
git log target..source --oneline
```

## 2. Identify Change Types

Determine what each change represents:
- Feature addition
- Bug fix
- Refactor
- Cleanup
- Potential breaking change

Note: missing tests, incomplete docs, inconsistencies.

## 3. Assess Code Quality & Impact

Evaluate:
- **Correctness**: Does the code work as intended?
- **Readability**: Is the code understandable?
- **Maintainability**: Will this be easy to modify later?
- **Architectural Alignment**: Does it follow the project's patterns?
- **Performance Implications**: Any performance concerns?
- **Security Considerations**: Any vulnerabilities?

Check whether tests adequately cover the changes.

## 4. Go-Specific Review Items

### Error Handling
- Are errors checked and handled explicitly? No `_ = someFunc()`.
- Are errors wrapped with context using `fmt.Errorf("doing X: %w", err)`?
- Are sentinel errors used with `errors.Is` / `errors.As` where appropriate?
- Do HTTP handlers return structured JSON error responses with proper status codes?

### Concurrency
- Are goroutines properly managed? Is there a `WaitGroup` or channel for coordination?
- Is `context.Context` propagated correctly for cancellation and timeouts?
- Is there shared mutable state without proper synchronisation (`sync.Mutex`, channels)?
- Is there potential for goroutine leaks (unbuffered channels blocking, missing `done` channels)?

### Resource Cleanup
- Are temp directories cleaned up with `defer os.RemoveAll()`?
- Are file handles, network connections, and other resources properly closed?
- Are `io.Copy`, `io.ReadAll` calls bounded or using `io.LimitReader` where appropriate?

### Interface Design
- Are interfaces small and defined at the consumer, not the implementation?
- Are there unnecessary abstractions or `interface{}` / `any` usage where a concrete type exists?
- Does the code avoid getter/setter methods in favour of exported fields or descriptive methods?

### Structured Logging
- Is `log/slog` used instead of `fmt.Println` or `log.Println`?
- Are log entries structured with key-value pairs (especially `run_id` for review-related logs)?
- Are sensitive values (secrets, tokens) never logged?

### Docker & Deployment
- Are secrets only read from environment variables, never hardcoded?
- Does the Dockerfile use multi-stage builds with `CGO_ENABLED=0`?
- Are only necessary runtime dependencies included in the final image?

### HTTP Handlers
- Is the webhook secret validated on every request?
- Are request methods checked before processing?
- Is JSON decoding/validation done before passing to business logic?
- Are responses immediate (<1s) with async processing where appropriate?

### Naming & Style
- Does the code follow Go naming conventions (PascalCase exported, camelCase unexported)?
- Are acronyms kept consistent (`URL`, `ID`, `HTTP`, `API` — all caps)?
- Are package names short, lowercase, without underscores?
- Is there proper import grouping (stdlib, third-party, local)?

## 5. Test Coverage

- Are there table-driven tests for functions with multiple cases?
- Do tests use `t.TempDir()` for ephemeral fixtures?
- Are interfaces mocked at the consumer (manual mocks or `gomock`/`mockgen`)?
- Are HTTP handlers tested with `httptest`?
- Do tests verify error cases, not just happy paths?
- Is the race detector used (`go test -race ./...`)?

## 6. Provide Senior-Level Review Summary

Offer direct, actionable feedback:
- Call out risks
- Highlight strengths
- Suggest improvements
- Indicate whether changes are ready to merge or need revisions

## 7. Aim for Practical, High-Value Feedback

The goal is to emulate a real PR review from an experienced engineer — clear, specific, and focused on what matters.

## 8. Write a comprehensive PR review report

Write a comprehensive PR review report in a markdown file and save it in the `./docs/ai_generated` directory. The report should include:
- Summary of changes
- Code quality assessment
- Performance considerations
- Security implications
- Testing coverage
- Recommendations
- Whether changes are ready to merge or need revisions

---

## Go Code Review Checklist

### Architecture & Design
- [ ] Follows standard Go project layout (`cmd/`, `internal/`)
- [ ] Proper separation of concerns (API handlers vs business logic)
- [ ] Interfaces defined at consumers, not implementers
- [ ] No `util` or `helper` packages — code lives where it belongs
- [ ] `internal/` used for project-specific code (not importable externally)

### Error Handling
- [ ] All errors checked explicitly — no silent discards
- [ ] Errors wrapped with `fmt.Errorf("context: %w", err)`
- [ ] Sentinel errors used with `errors.Is` / `errors.As`
- [ ] HTTP handlers return structured JSON errors with correct status codes

### Concurrency
- [ ] Goroutines managed with `sync.WaitGroup` or channels
- [ ] `context.Context` propagated for cancellation/timeouts
- [ ] Shared state protected with `sync.Mutex` or isolated via channels
- [ ] No goroutine leaks (done channels, bounded buffers)

### Performance
- [ ] No unnecessary allocations in hot paths
- [ ] I/O operations bounded or streamed (not loaded entirely into memory)
- [ ] `io.LimitReader` used for untrusted input
- [ ] No N+1 patterns in database or API calls

### Security
- [ ] No secrets in code — all via environment variables
- [ ] Webhook secret validated on every request
- [ ] Input validation at system boundaries
- [ ] SSH keys and PATs scoped with minimal permissions
- [ ] Ephemeral execution with forced cleanup

### Code Quality
- [ ] Follows Go naming conventions and style
- [ ] No `init()` functions unless absolutely necessary
- [ ] No `panic` in library/server code — returns errors
- [ ] No `reflect` usage unless no alternative exists
- [ ] Functions kept short and focused (<50 lines)
- [ ] Early returns to reduce nesting

### Logging
- [ ] Uses `log/slog` with structured key-value pairs
- [ ] Sensitive values never logged
- [ ] `run_id` included in review-related log entries
- [ ] Log rotation configured (lumberjack for file output)

### Testing
- [ ] Table-driven tests for multi-case functions
- [ ] `t.TempDir()` for ephemeral test fixtures
- [ ] HTTP handlers tested with `httptest`
- [ ] Error cases covered alongside happy paths
- [ ] Race detector clean (`go test -race ./...`)

### Docker
- [ ] Multi-stage build with `CGO_ENABLED=0`
- [ ] Only runtime dependencies in final image
- [ ] Log directory mounted as named volume
- [ ] No secrets in image layers
