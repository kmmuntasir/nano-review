# RTK (Rust Token Killer) Rules

## Golden Rule
**ALWAYS prefix terminal commands with `rtk`** when executing them through `run_command` or similar tools.

RTK intercepts common development commands and compresses their output to save tokens and improve context relevance.

## Recommended Commands
Use `rtk` for the following workflows:

### Git Operations
- `rtk git status`
- `rtk git diff`
- `rtk git log`
- `rtk git add`
- `rtk git commit`

### Go & Docker
- `rtk go build ./...`
- `rtk go test ./...`
- `rtk go vet ./...`
- `rtk golangci-lint run`
- `rtk docker compose up`
- `rtk docker compose logs`
- `rtk docker build`

### Tooling
- `rtk gh pr list`
- `rtk gh pr create`

## Benefits
- **Token Efficiency**: 60-90% reduction in output tokens.
- **Speed**: Faster context processing.
- **Clarity**: Filters out noise and ASCII art.

---
*Note: If a command is not explicitly supported by RTK, it will pass through unchanged. It is always safe to use the `rtk` prefix.*
