---
name: orchestrate-vibe-kanban
description: Orchestrate parallel Vibe Kanban workspace execution with dependency-aware scheduling and auto rebase-merge into main
argument-hint: <task details or markdown snippet with task IDs>
disable-model-invocation: true
allowed-tools: Bash(git *) Read Write Edit Glob
---

# Orchestrate Vibe Kanban

Run multiple Vibe Kanban tasks in parallel, respecting dependency order. Each task gets its own workspace. On completion, the workspace branch is rebased onto main and fast-forward merged locally. A cron job polls every 60s until all tasks are done.

## Input

The user provides `$ARGUMENTS` as a freeform message or markdown snippet containing **task/issue IDs** (UUIDs or simple IDs like "KMM-123"). They may also include context like organization, project, or repository names.

**Task IDs are mandatory** — never assume or invent them. If no task IDs are found in the input, ask the user to provide them.

Other parameters (org ID, project ID, repo ID) may be omitted. Auto-discover them from the Vibe Kanban board when missing.

---

## Phase 1: Discover Context

### 1.1 Parse task IDs from input

Scan `$ARGUMENTS` for:
- UUID patterns (`xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`)
- Simple ID patterns (`KMM-\d+`, `NANO-\d+`, or similar project-prefixed IDs)

Store all found IDs as `TASK_IDS[]`. If none found, ask the user — do not proceed without task IDs.

Also check for:
- Organization name or ID
- Project name or ID
- Repository name or ID
- Executor preference (default: `CLAUDE_CODE`)

### 1.2 Auto-discover missing context

Call these in parallel:

1. `list_organizations` — find org ID
2. `list_repos` — find repo ID
3. `list_projects(organization_id=<org_id>)` — find project ID

**Resolution rules:**

| Param | If provided | If missing |
|-------|------------|------------|
| Org ID | Use directly | Use first (or only) org from `list_organizations` |
| Project ID | Use directly | Match project name from input against `list_projects`, or use first project |
| Repo ID | Use directly | Match repo name from input against `list_repos`, or use the repo associated with the current workspace |
| Executor | Use directly | Default: `CLAUDE_CODE` |

If multiple orgs/projects/repos exist and none matches the input, list the options and ask the user to clarify.

### 1.3 Resolve simple IDs to UUIDs

If any `TASK_IDS` entry is a simple ID (e.g., "KMM-123"), resolve it:

Call `list_issues(project_id=<project_id>, simple_id=<simple_id>)` for each — make all calls in parallel.

Store the resolved UUID. If a simple ID doesn't resolve, report it and exclude from orchestration.

### 1.4 Fetch all issue details

For each resolved task UUID, call `get_issue(issue_id=<id>)` — make all calls in parallel.

From each response, extract:

```
ISSUES[issue_id] = {
  simple_id:     e.g. "KMM-123"
  title:         issue title
  description:   first 300 chars of description
  current_status: status string from board
  blockers:      []   (populated next step)
}
```

### 1.5 Build dependency graph

Examine the `relationships` array from each `get_issue` response.

Relationship semantics: `create_issue_relationship(issue_id=A, related_issue_id=B, type="blocking")` means **A blocks B**. If issue B's relationships contain an entry linking to A with type "blocking", then A blocks B.

For each issue, populate `ISSUES[issue_id].blockers` with the list of issue UUIDs that block it.

For blockers not in `ISSUES`, call `get_issue` to check their status. If status is "Done", consider the blocker resolved.

### 1.6 Classify issues

For each issue in `ISSUES`:

- **READY**: current status is not "Done" AND all blockers are resolved (status "Done" or external and Done)
- **BLOCKED**: has at least one unresolved blocker
- **DONE**: current status is already "Done"

---

## Phase 2: Start Initial Workspaces

For all READY issues, start workspaces **in parallel** (make all `start_workspace` calls at once).

For each READY issue:

```
start_workspace(
  name:       "{simple_id}: {title}",
  executor:   EXECUTOR,
  repositories: [{"repo_id": REPO_ID, "branch": "main"}],
  prompt:     "Implement the following task:\n\n## {title}\n\n{description}",
  issue_id:   <issue_id>
)
```

From each response, extract:
- `workspace_id` — the workspace UUID
- `branch` — the auto-generated branch name (e.g., `vk/XXXX-...`)

Update tracking:

```
ISSUES[issue_id].status       = "running"
ISSUES[issue_id].workspace_id = <from response>
ISSUES[issue_id].branch       = <from response>
```

---

## Phase 3: Persist State and Schedule Polling

### 3.1 Write state file

Write to `.claude/orchestration-state.json` (gitignored):

```json
{
  "version": 1,
  "org_id": "<ORG_ID>",
  "project_id": "<PROJECT_ID>",
  "repo_id": "<REPO_ID>",
  "executor": "<EXECUTOR>",
  "project_dir": "<absolute path to project root>",
  "started_at": "<ISO 8601 now>",
  "cron_job_id": null,
  "issues": {
    "<issue_id>": {
      "simple_id": "KMM-123",
      "title": "...",
      "status": "running|pending|done|failed|merging",
      "blocked_by": ["<blocker_id>", ...],
      "workspace_id": "uuid or null",
      "branch": "vk/... or null",
      "merge_attempts": 0,
      "error": null
    }
  }
}
```

### 3.2 Create cron job

Call `CronCreate`:

- **cron**: `* * * * *` (every minute)
- **recurring**: true
- **durable**: true
- **prompt**: The continuation protocol text below

### 3.2.1 Cron continuation prompt

Use this text as the cron prompt:

```
ORCHESTRATION CONTINUATION

Read the orchestration state file at .claude/orchestration-state.json. If the file does not exist, stop immediately — orchestration is complete or cancelled.

Follow these steps IN ORDER:

## Step 1: Check running workspaces

For each issue where status is "running":

1. Call `list_sessions(workspace_id=<issue.workspace_id>)`
2. Determine workspace completion:
   - No active sessions / latest session has terminal status → DONE
   - Sessions still running → SKIP, check again next cycle
3. If workspace is DONE:
   a. Check if workspace branch has changes:
      - Run: `git diff --quiet main..<branch>` (use `project_dir` from state as cwd)
      - Exit code 0 = no changes, 1 = has changes
   b. If NO changes: mark issue "done", call `update_issue(status="Done")`, call `update_workspace(archived=true)`, skip merge
   c. If HAS changes — rebase and merge locally:
      - Run from project_dir:
        1. `git checkout <branch>`
        2. `git rebase main`
           - If rebase succeeds: proceed
           - If rebase conflicts: `git rebase --abort`, mark issue "failed" with error "rebase conflict", STOP for this issue
        3. `git checkout main`
        4. `git merge --ff-only <branch>`
           - If merge succeeds: proceed
           - If merge fails: mark issue "failed" with error "fast-forward merge failed", STOP for this issue
        5. `git branch -d <branch>` (cleanup)
   d. After successful merge:
      - Call `update_issue(issue_id=<id>, status="Done")`
      - Call `update_workspace(workspace_id=<id>, archived=true)`
      - Set issue status to "done" in state file
4. If workspace FAILED:
   - Set issue status to "failed" in state file, record error

## Step 2: Retry merging issues

For each issue where status is "merging":

1. Increment merge_attempts in state
2. Retry the rebase + merge from Step 1.3.c
3. If succeeds: mark "done", update issue, archive workspace
4. If fails and merge_attempts >= 3: mark "failed", record error
5. If fails and attempts remain: keep "merging", retry next cycle

## Step 3: Start newly unblocked tasks

For each issue where status is "pending":

1. Check if ALL issue IDs in its blocked_by list have status "done" in state
2. If ALL blockers are "done" (or blocked_by is empty):
   a. Call `start_workspace`:
      - name: "{simple_id}: {title}"
      - executor: from state
      - repositories: [{"repo_id": state.repo_id, "branch": "main"}]
      - prompt: "Implement the following task:\n\n## {title}\n\n{description}"
      - issue_id: <issue_id>
   b. Save workspace_id and branch from response
   c. Set issue status to "running"
3. Start ALL newly unblocked workspaces in parallel

## Step 4: Check for completion

Count issues by status:
- If ALL issues are "done" or "failed":
  1. Call `CronDelete(id=state.cron_job_id)`
  2. Report final summary to user
  3. Delete the state file
  4. STOP — orchestration complete
- Else: continue to Step 5

## Step 5: Update state

Write the updated state back to .claude/orchestration-state.json. Ensure cron_job_id is preserved. The next cron cycle picks up where this one left off.
```

### 3.3 Save cron job ID

After `CronCreate` returns, update the state file's `cron_job_id` with the returned job ID.

### 3.4 Report initial status

Report to user:

```
Orchestration started. Monitoring N tasks (polling every 60s).

Running (X tasks):
  - KMM-123: Title... [branch: vk/...]
  - KMM-124: Title... [branch: vk/...]

Pending / blocked (Y tasks):
  - KMM-125: Title... (waiting on: KMM-123)
  - KMM-126: Title... (waiting on: KMM-124)

State: .claude/orchestration-state.json
Cron job: <cron_job_id>

Orchestration auto-completes when all tasks finish.
To stop: delete cron job with CronDelete or remove state file.
```

---

## Cancellation

To stop orchestration:

1. `CronDelete(id=<cron_job_id>)`
2. Remove `.claude/orchestration-state.json`
3. Running workspaces continue independently — not cancelled, just untracked

---

## Error Handling

| Scenario | Action |
|----------|--------|
| `start_workspace` fails | Mark "failed", continue with others |
| Rebase conflict | Abort rebase, mark "failed" — manual resolution needed |
| Fast-forward merge fails | Mark "merging", retry up to 3 cycles, then "failed" |
| State file missing | Stop immediately (orchestration cancelled) |
| Unresolvable simple ID | Report, exclude from orchestration |
| External blocker not "Done" | Issue stays "pending" until blocker resolves |
| Ambiguous org/project/repo | Ask user to clarify |

---

## Important Rules

1. **Task IDs are always from user input** — never assume or auto-select tasks
2. **Auto-discover context** — org/project/repo IDs resolved from board when not provided
3. **Local git merge only** — rebase workspace branch onto main, then fast-forward merge. No GitHub API.
4. **Parallelize workspaces** — start all ready workspaces at once
5. **State file is source of truth** — read it at start of each cron cycle
6. **Idempotent** — each cron cycle safe to run multiple times
7. **Don't block on failures** — one task failing shouldn't stop others
8. **Archive completed workspaces** — keeps workspace list clean
9. **Cron auto-expires after 7 days** — long orchestrations may need manual restart
