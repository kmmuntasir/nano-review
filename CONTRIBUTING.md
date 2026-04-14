# Contributing to Nano Review

Thank you for your interest in contributing! This guide covers the development workflow, code style, and pull request process.

## Development Setup

Nano Review runs entirely inside Docker. You do not need Go installed on your host machine.

1. **Fork** the repository and clone your fork
2. Copy the environment template and fill in required values:

   ```bash
   cp .env.example .env
   ```

   Edit `.env` and set at minimum:
   - `WEBHOOK_SECRET` — shared secret for GitHub webhook authentication
   - `ANTHROPIC_AUTH_TOKEN` — Anthropic API key for Claude Code
   - `GITHUB_PAT` — GitHub personal access token with `repo` scope

3. Start the development server:

   ```bash
   make dev
   ```

   This builds the Docker image and starts the server in the foreground.

## Make Targets

| Target | Description |
|--------|-------------|
| `make dev` | Build and run (foreground) |
| `make dev-d` | Build and run (detached) |
| `make dev-logs` | Tail container logs |
| `make dev-down` | Stop and remove containers |
| `make dev-restart` | Restart containers |
| `make dev-build` | Rebuild image (no cache) |
| `make test` | Run tests with race detector |
| `make test-cover` | Run tests with HTML coverage report |
| `make lint` | Run `go vet` and `go fmt` |
| `make fmt` | Format code |
| `make clean` | Remove containers, volumes, and local images |
| `make help` | Show all available targets |

## Running Tests

Go tooling runs inside the Docker container's builder stage (the runtime image does not include the Go toolchain):

```bash
docker compose run --rm nano-review go test -race ./...
```

For a specific package:

```bash
docker compose run --rm nano-review go test -race ./internal/api/
```

For an HTML coverage report:

```bash
docker compose run --rm nano-review go test -race -coverprofile=coverage.out ./...
docker compose run --rm nano-review go tool cover -html=coverage.out -o coverage.html
```

Integration tests (require Docker daemon):

```bash
docker compose run --rm nano-review go test -tags=integration ./...
```

## Code Style

- **Formatting**: Use `gofmt` / `go fmt` — no configuration needed.
- **Static analysis**: Run `go vet ./...` before submitting.
- **Imports**: Standard library, then third-party, then local packages (enforced by `gofmt`).
- **Error handling**: Always handle errors explicitly. Wrap with `fmt.Errorf("context: %w", err)`. Never use `_ = someFunc()`.
- **Logging**: Use structured logging via `log/slog`. Never use `fmt.Println` or `log.Println`.
- **Testing**: Prefer table-driven tests. Use `t.TempDir()` for ephemeral fixtures. Always run with `-race`.
- **Naming**: Follow [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments) — `PascalCase` for exported, `camelCase` for unexported, all-caps for acronyms (`URL`, `ID`, `HTTP`).
- **Interfaces**: Define at the consumer, not the implementer. Keep interfaces small.

## Pull Request Process

1. Create a branch from `main` following the naming convention:

   ```
   type/NANO-<ticket>-hyphenated-description
   ```

   Examples: `feature/NANO-42-add-review-timeout`, `bugfix/NANO-99-fix-clone-failure`

2. Keep each PR focused on a single concern.

3. Run checks before pushing:

   ```bash
   make test && make lint
   ```

4. Commit messages use the format:

   ```
   NANO-<ticket>: message
   ```

   Example: `NANO-42: Add review timeout handling`

5. **Rebase and Merge only** — no merge commits, no squash merging. All merging happens via PR rebase on GitHub.

## Reporting Bugs

Open a [GitHub Issue](https://github.com/kmmuntasir/nano-review/issues) and include:

- A clear description of the bug
- Steps to reproduce
- Expected vs. actual behavior
- Relevant logs (redact any secrets)
- Your environment (Docker version, OS)

## Security Vulnerabilities

Do not report security vulnerabilities through public GitHub Issues. See [SECURITY.md](./SECURITY.md) for the responsible disclosure process.

## License

By contributing, you agree that your contributions will be licensed under the [GNU General Public License v3.0](./LICENSE).
