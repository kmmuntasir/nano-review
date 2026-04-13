---
name: breakdown-plan-into-tasks
description: Break down a large implementation plan into small, parallelizable tasks for individual developers. Use when user requests to break down a plan into tasks.
---

# Task Breakdown Skill

Read the provided plan file thoroughly. Then follow the two-phase process below.

## Phase 1: Codebase Analysis

Before breaking down any tasks, **analyze the codebase** to build accurate understanding and fill knowledge gaps the plan may leave implicit. This step prevents tasks that reference nonexistent files, wrong abstractions, or outdated patterns.

Specifically:

- **Verify every file path and module** the plan mentions — confirm they exist, or note that they need to be created
- **Map current architecture** — understand the existing patterns, conventions, and interfaces the plan builds on top of
- **Identify hidden coupling** — shared types, utilities, or config that the plan doesn't explicitly call out but that tasks will touch
- **Check for prior art** — search for existing implementations that partially cover what the plan describes (partial refactor, abandoned branch, utility function already written)

Use up to **3 parallel subagents** (via the Agent tool) to speed up this phase and keep the main context window clean. Example split:

| Subagent | Responsibility |
|----------|---------------|
| Subagent 1 | Verify file/module existence, map directory structure, check build/config files |
| Subagent 2 | Trace data flow and interfaces the plan references — read relevant source files |
| Subagent 3 | Search for prior art, existing utilities, and hidden coupling across the codebase |

Each subagent should return a concise summary of findings. Use those summaries — not raw exploration — to inform the breakdown.

## Phase 2: Task Breakdown

Using the plan plus the codebase analysis, break the work down into small, self-contained tasks that individual developers can pick up independently.

You may continue using up to **3 parallel subagents** during this phase to draft batches of tasks concurrently. For example:

| Subagent | Responsibility |
|----------|---------------|
| Subagent 1 | Draft Batch 1 tasks (no dependencies) |
| Subagent 2 | Draft Batch 2 tasks (depends on Batch 1) |
| Subagent 3 | Draft Batch 3+ tasks and the visual dependency diagram |

Merge the subagent outputs into the final document, resolving any conflicts or gaps.

## Output

Write the results in a new file alongside the plan, named `{plan-filename}-tasks.md`.

## Task Format

Each task must include:

- **Title** — concise, action-oriented (e.g., "Extract WebSocket manager into ES module")
- **Description** — detailed enough for a developer unfamiliar with the plan to execute. Include source references (file paths, line numbers, function names, codeblocks), what to create/modify, and relevant context
- **Acceptance Criteria** — specific, verifiable checklist items
- **Subtasks** — only if the task is complex enough to warrant them
- **Dependencies** — exact task numbers this depends on, or "None"

## Parallelization Strategy

Include a batch-based execution model at the top of the document:

1. **Organize tasks into batches** based on dependency order — all tasks within a batch can run in parallel with zero merge conflicts
2. **Include a visual batch diagram** showing the dependency flow
3. **State merge order rules** — which batches must merge before others can start
4. **Provide a summary table** — columns: `#`, Batch, Target File, Dependencies, Can Parallel With
5. **Suggest developer assignment tracks** — 2–3 developer assignment paths through the batches

## Key Principles

- **One task = only a few files (tightly coupled set)** — minimize merge conflict surface
- **Dependencies are explicit** — every dependency listed by task number
