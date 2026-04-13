---
name: create-vibe-kanban-tasks
description: Create tasks in Vibe Kanban Board from a task plan file. Use when user wants to add tasks/issues to their Vibe Kanban dashboard from a markdown task breakdown file.
---

# Create Kanban Tasks Skill

Read a task plan file and create every task as a Vibe Kanban issue in the specified project, preserving all content, order, and dependencies.

## Inputs

The user must provide two things (either explicitly or implied by context):

1. **Task plan file path** — A markdown file containing numbered tasks with descriptions, acceptance criteria, and dependencies (e.g., `docs/task_plan/frontend-es-module-migration/frontend-esm-migration-plan-tasks.md`)
2. **Project name** — The Vibe Kanban project to create issues in (e.g., "Nano Review")

If the user says something like "create all these tasks in the todo column for the <project-name> project", extract the project name from their message. If the file path is not clear, ask the user.

## Execution Steps

Follow these steps exactly, in order. Do not skip or merge steps.

### Step 1: Read the task plan file

Read the entire task plan file using the Read tool. You MUST read the full file — never truncate. If the file is large, read it in chunks until you have the complete content.

### Step 2: Discover the Vibe Kanban project

Call these MCP tools in sequence:

1. `list_organizations` — get the organization ID
2. `list_organizations` result → take the first (or only) org's `id`
3. `list_projects` with `organization_id` set to that org ID
4. Find the project whose name matches the user-specified project name (case-insensitive match)
5. Save the `project_id` for all subsequent calls

If the project is not found, stop and tell the user which projects are available. Do not guess.

### Step 3: Parse all tasks from the file

Parse the markdown file and extract every task. Tasks are typically structured as:

```
### Task N: <title>
...description...
**Acceptance Criteria**
- [ ] item 1
- [ ] item 2
**Dependencies**: ...
```

For each task, extract:
- **Task number** (from heading, e.g., "Task 5")
- **Title** (from heading, e.g., "Extract `js/stream-parser.js` — JSONL stream parser")
- **Full description** — everything between the title heading and the Acceptance Criteria or Dependencies section, including code blocks, tables, and bullet points
- **Acceptance criteria** — all checkbox items, preserving the full text of each
- **Subtasks** — if present, all subtask items
- **Dependencies** — task numbers this task depends on (e.g., "Tasks 2, 4, 5" or "None")

Store all parsed tasks in an ordered list. Count them and report the total to the user before proceeding.

### Step 4: Create issues one at a time

For **each task** in order (Task 1, Task 2, Task 3, ...):

#### Build the description

Construct the issue description as a single markdown string with these sections:

````markdown
## Description

<full task description — include everything: paragraphs, tables, code blocks, lists, source references. Copy verbatim from the plan. Do NOT summarize or truncate.>

## Acceptance Criteria

- [ ] <criterion 1>
- [ ] <criterion 2>
...

## Dependencies

<dependency text from the plan, e.g., "Tasks 2, 4, 5" or "None">
````

If the task has subtasks, add them between Description and Acceptance Criteria:

````markdown
## Subtasks

- [ ] <subtask 1>
- [ ] <subtask 2>
...
````

#### Call create_issue

Use the `create_issue` MCP tool with these exact parameters:

| Parameter | Value |
|-----------|-------|
| `title` | **Required.** Format: `Task N: <title>` (e.g., `"Task 5: Extract js/stream-parser.js — JSONL stream parser"`) |
| `description` | **Required.** The full markdown description built above |
| `project_id` | **Required.** The UUID discovered in Step 2 |
| `priority` | Only set if the task plan explicitly mentions priority (e.g., "urgent", "high", "medium", "low"). Otherwise omit this parameter entirely — do not pass null. |
| `parent_issue_id` | Only set if the task is explicitly marked as a subtask of another task. Otherwise omit. |

**Critical rules for create_issue calls:**

- You MUST pass the `project_id` parameter. Never omit it.
- The `title` MUST include the task number prefix (e.g., "Task 1:", "Task 12:").
- The `description` MUST contain the full content. Never summarize, abbreviate, or truncate. Include all code blocks, tables, and lists verbatim.
- Save the returned `id` (issue UUID) for every created issue — you need these for linking dependencies in Step 5.

#### After each creation

Report progress to the user: "Created Task N: <title>"

### Step 5: Link dependencies

After ALL issues are created, iterate through the tasks again and create dependency relationships.

For each task that has dependencies listed (not "None"):

- Parse the dependency task numbers (e.g., "Tasks 2, 4, 5" → [2, 4, 5])
- For each dependency task number, call `create_issue_relationship` with:
  - `issue_id` — the UUID of the dependency task (the one being depended upon, i.e., the blocker)
  - `related_issue_id` — the UUID of the current task (the one that is blocked)
  - `relationship_type` — `"blocking"`

This means: "Task 2 blocks Task 8" → `issue_id` = Task 2's UUID, `related_issue_id` = Task 8's UUID.

Report: "Linked dependencies for Task N: blocked by Tasks X, Y, Z"

### Step 6: Set initial status

If the user specified a target column/status (e.g., "todo column"), update each created issue's status.

Call `update_issue` for each issue with:
- `issue_id` — the UUID
- `status` — the status name (e.g., `"todo"`)

The status name must match exactly what the project uses. If unsure, create the issues without setting status and inform the user they can move them manually.

### Step 7: Final report

After all tasks are created and dependencies linked, report:

```
Done. Created N tasks in project "<project-name>":
- Task 1: <title>
- Task 2: <title>
- ...
- Task N: <title>

Dependencies linked: X relationships
```

## Error Handling

- If `create_issue` fails for a task, report the error, skip that task, and continue with the next one. Do not stop the entire process.
- If a dependency references a task that failed to create, skip that dependency link and report it.
- If the project is not found, list available projects and stop.
- If the file cannot be read, ask the user to verify the path.

## Important Rules

1. **Never truncate** — Every description must contain the complete content from the plan file. This is the most important rule.
2. **Never skip tasks** — Create every task found in the file, from first to last.
3. **Never summarize** — Copy descriptions, acceptance criteria, and code blocks verbatim.
4. **Maintain order** — Create tasks in the order they appear in the file (Task 1, Task 2, ...).
5. **One at a time** — Create each issue individually (no batching). Wait for each `create_issue` call to complete before proceeding to the next.
6. **Save UUIDs** — Store every returned issue ID. You need them all for dependency linking.
7. **Title format** — Always prefix with "Task N: " so tasks are easily identifiable on the board.
