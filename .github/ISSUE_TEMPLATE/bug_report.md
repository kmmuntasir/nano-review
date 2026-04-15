---
name: Bug report
about: Report a bug or unexpected behavior in nano-review
labels: bug
title: '[BUG] '
---

## Description
Clear, concise description of the bug.

## Steps to Reproduce
1. Go to '...'
2. Click on '....'
3. Scroll down to '....'
4. See error

## Expected Behavior
What you expected to happen.

## Actual Behavior
What actually happened (include error messages, stack traces, etc.).

## Environment
- **nano-review version**: (e.g., v1.0.0 or commit hash)
- **Docker version**: (run `docker --version`)
- **Go version**: (run inside container: `docker compose run --rm nano-review go version`)
- **OS**: (e.g., Ubuntu 22.04, macOS Sonoma)
- **Deployment**: (local dev, Docker Compose, production)

## Logs
Relevant log excerpts from `/app/logs/` or Docker logs:
```bash
docker compose logs nano-review
```

```
paste logs here
```

## Additional Context
Any other relevant information (screenshots, related issues, suggestions for fix).