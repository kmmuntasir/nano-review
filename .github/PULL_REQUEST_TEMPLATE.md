## Description
Briefly describe the changes made in this PR.

## Related Issue
Closes #(issue number)

## Type of Change
- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] Refactoring (internal improvement, no external behavior change)
- [ ] Documentation update
- [ ] CI/CD configuration change

## How Has This Been Tested?
Describe the testing performed for this change. Include:
- Unit tests added/updated
- Integration tests run
- Manual testing steps

**Run tests inside Docker:**
```bash
docker compose run --rm nano-review go test -race ./...
docker compose run --rm nano-review go vet ./...
```

## Checklist
- [ ] Code follows the [Go style guide](.claude/rules/go-style-guide.md)
- [ ] Commit messages follow the format: `NANO-TICKET: message` (see [.claude/rules/git-guidelines.md](.claude/rules/git-guidelines.md))
- [ ] Tests pass: `docker compose run --rm nano-review go test -race ./...`
- [ ] No linter errors: `docker compose run --rm nano-review go vet ./...`
- [ ] Code is formatted: `docker compose run --rm nano-review go fmt ./...`
- [ ] No secrets or credentials committed
- [ ] Documentation updated (if applicable)

## Docker Validation
- [ ] Docker build succeeds: `docker compose up --build`
- [ ] Service starts without errors
- [ ] Health checks pass (if applicable)