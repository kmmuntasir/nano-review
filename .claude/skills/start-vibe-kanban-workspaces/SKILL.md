---
name: start-vibe-kanban-workspaces
description: Start Vibe Kanban workspaces from a task file. Use when user wants to launch workspaces on their Vibe Kanban dashboard from a markdown task breakdown file.
---

# Start Vibe Kanban Workspaces Skill

Read a task plan file and start a workspace for each specified task on the Vibe Kanban dashboard.

## Inputs

The user must provide:

1. **Task plan file path** — A markdown file containing numbered tasks (e.g., `docs/tasks/my-plan.md`)
2. **Which tasks to start** — Either specific task numbers (e.g., "start tasks 1, 3, 5"), a range (e.g., "start tasks 1-4"), or "all" to start every task in the file

If the file path is not clear, ask the user. If the user does not specify which tasks, ask before proceeding.

## Execution Steps

Follow these steps exactly, in order.

### Step 1: Read the task plan file

Read the entire task plan file using the Read tool. Read the full file — never truncate. If the file is large, read in chunks until complete.

### Step 2: Parse tasks from the file

Parse the markdown and extract every task. Tasks are typically structured as:

```
### Task N: <title>
...description...
```

For each task, extract:
- **Task number** (from heading, e.g., "Task 5")
- **Title** (from heading, e.g., "Extract stream parser")
- **Full description** — everything under the task heading, including code blocks, tables, bullet points, acceptance criteria, and dependencies. Copy verbatim.

Store all parsed tasks in an ordered list. Count them and report total to user.

### Step 3: Filter tasks to start

Apply the user's selection:
- **Specific numbers** — keep only those task numbers (e.g., "1, 3, 5")
- **Range** — keep tasks in that range inclusive (e.g., "1-4" means tasks 1, 2, 3, 4)
- **"all"** — keep every task

Report which tasks will be started and confirm with user before proceeding if the count is large (5+). List each task number and title in a compact table.

### Step 4: Discover repositories

Call `list_repos` MCP tool. Save all repos (name + UUID). These are needed for workspace creation.

If no repos are found, stop and tell the user.

### Step 5: Start workspaces

For **each selected task**, call `start_workspace` with:

| Parameter | Value |
|-----------|-------|
| `name` | Format: `Task N: <title>` (e.g., `"Task 5: Extract stream parser"`) |
| `prompt` | The full task description parsed in Step 2. Copy verbatim — never summarize or truncate. |
| `executor` | `"CLAUDE_CODE"` unless user specifies a different executor |
| `repositories` | All repos from Step 4, each with `branch` set to the current git branch. Construct as: `[{"repo_id": "<uuid>", "branch": "<current-branch>"}]` |

Get the current branch name by running `git branch --show-current` via Bash.

**Rules:**
- Start workspaces one at a time. Wait for each `start_workspace` call to complete before the next.
- Save every returned `workspace_id` for the final report.
- Report progress after each: "Started workspace for Task N: <title>"

### Step 6: Final report

Report a summary with all workspace IDs.

```
Done. Started N workspaces.

Repository: <repo_name> (<repo_id>)
Branch: <branch_name>

Workspaces:
  #   Title                           Workspace ID
  ─── ─────────────────────────────── ──────────────────────────────────────
  1   Task 1: <title>                 <workspace_id_1>
  2   Task 3: <title>                 <workspace_id_2>
  ...
```

Include repo IDs so user can reference them for future operations.

## Error Handling

- If `start_workspace` fails for a task, report the error, skip that task, continue with next. Do not stop entire process.
- If file cannot be read, ask user to verify path.
- If a task number in user's selection does not exist in the file, report it and skip.

## Important Rules

1. **Never truncate** — Every workspace prompt must contain the complete task description from the plan file.
2. **Never summarize** — Copy descriptions, acceptance criteria, code blocks verbatim.
3. **One at a time** — Start each workspace individually. Wait for each call to complete before proceeding.
4. **Save workspace IDs** — Store every returned ID for the final report.
5. **Title format** — Always prefix workspace name with "Task N: " for easy identification.
