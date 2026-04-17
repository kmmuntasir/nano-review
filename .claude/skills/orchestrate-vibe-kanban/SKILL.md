---
name: orchestrate-vibe-kanban
description: Orchestrate parallel Vibe Kanban workspace execution with dependency-aware scheduling and auto rebase-merge into main (LOCAL ONLY — no remote operations) via MCP workspace prompts
argument-hint: <task details or markdown snippet with task IDs>
disable-model-invocation: true
allowed-tools: Bash(git *) Read Write Edit Glob mcp__vibe_kanban__*
---

# Orchestrate Vibe Kanban

Run multiple Vibe Kanban tasks in parallel, respecting dependency order. Each task gets its own workspace (git worktree). On completion, a merge prompt is sent to the workspace via `run_session_prompt` — the workspace rebases onto main, resolves any conflicts using AI, then the orchestrator fast-forwards main **locally**. No merge commits. **No remote operations — no `git push`, no `git pull`, no `git fetch`. Everything stays on disk.** A cron job polls every 60s until all tasks are done.

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

First, check if `project-metadata.local.md` exists in the project root. If it does, extract org ID, repo ID, and project ID directly from it — skip the MCP tool calls below.

If `project-metadata.local.md` does NOT exist or is missing some IDs, call these in parallel for each missing value:

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

Set `MAX_CONCURRENT_WORKSPACES = 5`.

For READY issues, start workspaces **in parallel** — up to the concurrency limit. If more than 5 issues are READY, start the first 5 (prioritize by sort order from the board). The rest remain READY and will be picked up by future cron cycles (Phase 3, Step 3).

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

Write to `.claude/orchestration-state.json` (gitignored) using **atomic writes**:

1. Write the full JSON to `.claude/orchestration-state.tmp.json`
2. Rename/move `.claude/orchestration-state.tmp.json` → `.claude/orchestration-state.json`

Never write directly to the state file. Always write to the `.tmp` file first, then atomically replace.

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
      "merge_session_id": null,
      "merge_execution_id": null,
      "merge_attempts": 0,
      "conflict_resolution": false,
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

## Step 0: Acquire lock

Check if `.claude/orchestration.lock` exists.

- If it does NOT exist: create it immediately with the current ISO 8601 timestamp as its sole content (e.g., `2026-04-17T14:32:00Z`). Proceed.
- If it DOES exist: read the timestamp from it. If the lock is older than 5 minutes, assume the previous cycle crashed — delete the stale lock file and proceed (create a new one with the current timestamp). If the lock is younger than 5 minutes, exit silently — a previous cron cycle is still running.

The lock file MUST be deleted at the very end of this cycle (after Step 5), even if errors occur. Treat this as a defer/cleanup operation — no matter what happens, the lock must be removed before the cycle ends.

## Read state

Read the orchestration state file at .claude/orchestration-state.json. If the file does not exist, delete the lock file and stop immediately — orchestration is complete or cancelled.

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
   c. If HAS changes — initiate MCP workspace merge:
      1. Call `create_session(workspace_id=<issue.workspace_id>, name="merge")` → get `merge_session_id`
      2. Send merge prompt via `run_session_prompt(session_id=<merge_session_id>, prompt=<MERGE_PROMPT>)` → get `merge_execution_id`
      3. Set issue status to "merging" in state
      4. Save merge_session_id and merge_execution_id in state
   d. After successful merge (see Step 2):
      - Call `update_issue(issue_id=<id>, status="Done")`
      - Call `update_workspace(workspace_id=<id>, archived=true)`
      - Set issue status to "done" in state file
4. If workspace FAILED:
   - Set issue status to "failed" in state file, record error

## Step 2: Poll merging workspaces

For each issue where status is "merging":

1. If merge_execution_id is set:
   a. Call `get_execution(execution_id=<issue.merge_execution_id>)`
   b. Check execution status:
      - **Running/in-progress** → SKIP, poll again next cycle
      - **Success/complete** → proceed to step 2.d
      - **Failed/error** → proceed to step 2.c
2. If no merge_execution_id (fallback — session created but prompt not yet sent):
   a. Retry `run_session_prompt` with the merge prompt
   b. Save merge_execution_id from response
   c. SKIP, poll next cycle
3. If execution FAILED:
   a. Increment merge_attempts in state
   b. If merge_attempts >= 3: mark issue "failed" with error "merge failed after 3 attempts", STOP for this issue
   c. If attempts remain: retry — create new merge session and re-send merge prompt, reset merge_session_id and merge_execution_id, keep status "merging"
4. If execution SUCCEEDED (the workspace AI successfully rebased):
   a. Switch execution context to the main project directory (`state.project_dir`).
   b. Execute the local-only merge commands from the main directory:
      - `git checkout main`
      - `git merge --ff-only <issue.branch>`
   c. If the fast-forward merge succeeds:
      - Call `update_issue(issue_id=<id>, status="Done")`
      - Call `update_workspace(workspace_id=<id>, archived=true)`
      - Set issue status to "done" in state file
   d. If the merge fails (e.g., not fast-forwardable due to diverged history), mark issue "failed" with error and the exact git output for manual review.
   **IMPORTANT**: Do NOT run `git pull`, `git push`, `git fetch`, or any remote git operation. The merge is local-only.
5. If execution output mentions "conflicts were detected" or "rebase was aborted" (check execution output text):
   a. Treat same as step 4.c above — send conflict resolution prompt via new session
   b. Do NOT increment merge_attempts — conflict resolution is a retry, not a failure

## Step 3: Start newly unblocked tasks

Calculate available slots: `available = MAX_CONCURRENT_WORKSPACES(5) - count of issues with status "running"`.

If `available <= 0`, skip this step entirely.

For each issue where status is "pending":

1. Check if ALL issue IDs in its blocked_by list have status "done" in state
2. If ALL blockers are "done" (or blocked_by is empty) AND slots remain:
   a. Call `start_workspace`:
      - name: "{simple_id}: {title}"
      - executor: from state
      - repositories: [{"repo_id": state.repo_id, "branch": "main"}]
      - prompt: "Implement the following task:\n\n## {title}\n\n{description}"
      - issue_id: <issue_id>
   b. Save workspace_id and branch from response
   c. Set issue status to "running"
   d. Decrement `available` by 1
3. Start all eligible workspaces in parallel (up to `available` limit)
4. Stop starting workspaces when `available` reaches 0 — remaining unblocked tasks stay "pending" for the next cycle

## Step 4: Check for completion

Count issues by status:
- If ALL issues are "done" or "failed":
  1. Call `CronDelete(id=state.cron_job_id)`
  2. Report final summary to user
  3. Delete the state file
  4. Delete `.claude/orchestration.lock`
  5. STOP — orchestration complete
- Else: continue to Step 5

## Step 5: Update state

Write the updated state to `.claude/orchestration-state.tmp.json`, then atomically rename it to `.claude/orchestration-state.json`. Ensure cron_job_id is preserved. Then delete `.claude/orchestration.lock`. The next cron cycle picks up where this one left off.
```

### 3.2.2 Merge prompt

Use this exact prompt when calling `run_session_prompt` for workspace merges:

```
MERGE PREPARATION — rebase your workspace branch onto local main.

Execute these steps IN ORDER. Do not skip any step.

## Step 1: Rebase workspace branch onto local main
Run: `git rebase main`

- If this succeeds cleanly (no conflicts), STOP and report success. The orchestrator will handle the actual merge.
- If rebase has CONFLICTS:
  1. Abort the rebase: `git rebase --abort`
  2. Report that conflicts were detected. DO NOT attempt to resolve conflicts yourself.
  3. STOP here — the orchestrator will send a follow-up prompt with conflict resolution instructions.

## Rules
- DO NOT run `git checkout main`. Git will block this because main is checked out in the primary worktree.
- DO NOT run `git merge`.
- DO NOT push to origin.
- DO NOT run `git fetch` or `git pull` — this operation is local-only.
- If anything fails, report the exact error output.
```

### 3.2.3 Conflict resolution prompt

Use this exact prompt when calling `run_session_prompt` after a rebase conflict is detected:

```
CONFLICT RESOLUTION — resolve rebase conflicts against main.

## Step 1: Rebase workspace branch
Run: `git rebase main`

This will produce conflicts. Resolve them as follows:
1. Run `git status` to list conflicted files.
2. For each file, combine changes intelligently, then run `git add <filepath>`.
3. Run `git rebase --continue`.
4. If you fail to resolve conflicts after 2 attempts, run `git rebase --abort` and report a fatal failure.

## Step 2: Stop and Report
Once the rebase is successfully completed, STOP and report success. The orchestrator will handle the actual merge into main.

## Rules
- DO NOT run `git checkout main`.
- DO NOT run `git merge`.
- DO NOT push to origin.
- DO NOT run `git fetch` or `git pull` — this operation is local-only.
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
3. Remove `.claude/orchestration.lock` (if it exists)
4. Running workspaces continue independently — not cancelled, just untracked

---

## Error Handling

| Scenario | Action |
|----------|--------|
| `start_workspace` fails | Mark "failed", continue with others |
| `run_session_prompt` merge fails | Retry up to 3 cycles with new session, then mark "failed" |
| Rebase conflict detected | Abort rebase, send conflict resolution prompt via new `run_session_prompt`, do NOT count as merge attempt |
| Conflict resolution fails (workspace AI can't resolve) | Increment merge_attempts, retry up to 3 cycles total, then mark "failed" |
| `get_execution` returns error | Retry next cycle, count toward merge_attempts |
| State file missing | Stop immediately (orchestration cancelled) |
| Unresolvable simple ID | Report, exclude from orchestration |
| External blocker not "Done" | Issue stays "pending" until blocker resolves |
| Ambiguous org/project/repo | Ask user to clarify |

---

## Important Rules

0. **LOCAL OPERATIONS ONLY** — This entire orchestration is local. Never run `git push`, `git pull`, `git fetch`, `git push origin`, or any other remote git command. Not in the orchestrator, not in workspace merge prompts, not anywhere. The orchestrator merges worktree branches into the local `main` via `git merge --ff-only`. Remote sync (push/pull) is the user's responsibility. Violating this rule is a critical failure.
1. **Task IDs are always from user input** — never assume or auto-select tasks
2. **Auto-discover context** — org/project/repo IDs resolved from board when not provided
3. **Split merge responsibility** — workspace AI handles `git rebase main` only (no checkout main, no merge, no push). Orchestrator handles `git checkout main` + `git merge --ff-only` from `project_dir`. No merge commits. **No remote git operations anywhere** — no `git push`, no `git pull`, no `git fetch`.
4. **Cap concurrent workspaces at 5** — never exceed `MAX_CONCURRENT_WORKSPACES`. Excess READY/pending tasks wait for slots to open.
5. **State file is source of truth** — read it at start of each cron cycle
6. **Atomic state writes** — always write to `.tmp` first, then rename. Never write directly to the state file.
7. **Lock file prevents cron overlaps** — acquire `.claude/orchestration.lock` (with current timestamp) at cycle start, release at cycle end. If lock exists and is older than 5 minutes, treat as stale (crashed cycle), delete and proceed. If younger than 5 minutes, exit silently.
8. **Idempotent** — each cron cycle safe to run multiple times
9. **Don't block on failures** — one task failing shouldn't stop others
10. **Archive completed workspaces** — keeps workspace list clean
11. **Cron auto-expires after 7 days** — long orchestrations may need manual restart
