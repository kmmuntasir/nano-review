---
trigger: model_decision
description: Ruleset that MUST be followed when executing ANY git command
---

# Git Guidelines

## Sacred Rule:
- NEVER run a `git` command without user's explicit approval.

## Merge Policy:
- **Rebase and Merge ONLY** — This repository uses "Rebase and Merge" policy
- **No merge commits** — Never use `git merge`
- **No squash merging** — Never use `--squash` flag
- **No local branch merging** — Do not merge branches locally; all merging happens via PR rebase on GitHub

## Project Slug:
- PROJECTSLUG is a shortened name for the project to be used as an abbreviation for several things, example: JIRA tickets.
- PROJECTSLUG for this project is: **NANO**
- Can be found in the `./project-metadata.md` file.

## Branch Naming:
- Format: type/PROJECTSLUG-TICKET_NUMBER-hyphenated-short-description
- Example: `feature/NANO-123-add-review-timeout`, `bugfix/NANO-234-fix-clone-failure`
- Exception: Release branches should be named like this: `release/1.2.3`, no description or ticket number, just the version number.
- Use imperative, hyphenated style for description.
- Never assume ticket number. If missing, omit it.
- Note: If the project uses Trello, then Card Number should be used in place of Ticket number.

## Commit Messages:
- ALWAYS use single line commit message
- Format: PROJECTSLUG-TICKET_NUMBER: message
- Example: `NANO-123: Add review timeout handling`
- Extract ticket number from branch name
- If ticket is not identifiable, omit prefix and write message only.

## .gitignore
Ensure these entries exist. Never commit sensitive build artifacts:
- `*.exe`, `*.exe~, `*.dll`, `*.so`, `*.dylib`
- `*.test`, `*.out`
- `/vendor/`
- `.env` (actual secrets, not `.env.example`)
- `/app/logs/`
