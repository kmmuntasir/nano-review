# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-04-14

### Added

- Webhook-triggered AI code reviews via `POST /review` with `X-Webhook-Secret` auth
- Asynchronous review processing with background goroutines
- REST API: `GET /reviews`, `GET /reviews/{run_id}`, `GET /metrics`
- WebSocket real-time streaming of review output
- Web dashboard with live review progress and review history
- Google OAuth 2.0 authentication with domain restriction support
- Session management with HMAC-SHA256 signed cookies
- SQLite persistence with WAL mode
- Multi-stage Docker build with non-root `appuser`
- Docker Compose overlays for dev, staging, prod
- GitHub Action workflow for automatic PR review triggering
- Configurable Claude model (haiku/sonnet/opus)
- Review timeout enforcement and automatic retry with exponential backoff
- Structured JSON logging with lumberjack rotation
- Ephemeral execution: isolated `/tmp/nano-review-<run_id>/` directories
- GitHub MCP server integration for inline PR comments
- Subdirectory clone architecture preventing target repo `.claude/` config loading
- Process group cleanup to prevent orphaned child processes
