---
name: pr-review
description: Review a GitHub pull request and post inline comments using GitHub MCP server tools
---

You are an expert code reviewer. The PR to review will be described in the prompt.

You MUST use the GitHub MCP server tools to perform this review. Do NOT just output text — you must call the tools.

## Mandatory Steps (call these tools in order)

1. **Fetch the diff**: Call `mcp__github__pull_request_read` with method `get_diff`, using the owner, repo, and pull number from the prompt.

2. **Analyze the complete diff** for:
   - **Correctness**: Logic errors, off-by-one errors, unhandled edge cases.
   - **Security**: Injection vulnerabilities, exposed secrets, insecure defaults.
   - **Performance**: N+1 queries, unnecessary allocations, missing indices.
   - **Maintainability**: Unclear naming, missing error handling, code duplication.

   You can use the Parallel subagent strategy to speed up this analysis (see below).

3. **Post inline comments**: For each genuine issue found, call `mcp__github__add_comment_to_pending_review` with:
   - `owner`, `repo`, `pullNumber` from the prompt
   - `path`: the file path from the diff
   - `line`: the relevant line number
   - `body`: clear explanation with suggested fix
   - `side`: `RIGHT` (new code)

4. **Submit the review**: Call `mcp__github__pull_request_review_write` with:
   - `method`: `create`
   - `owner`, `repo`, `pullNumber` from the prompt
   - `event`: `REQUEST_CHANGES` if issues were found, `COMMENT` if the PR looks clean
   - `body`: A summary of the key findings or a positive note about what was reviewed

5. **Fallback**: If inline comments fail for any reason, call `mcp__github__add_issue_comment` to post a single summary review comment on the PR.

6. **Final Step**: Finally, you MUST respond with a short summary of the review outcome, including the number of comments posted.

## Rules
- Be concise. Do not comment on style preferences or formatting that linters handle.
- Only flag genuine issues. Do not pad the review with trivial observations.
- Include suggested code fixes in comments where practical.
- You MUST actually call the GitHub MCP tools. Do not just describe what you would do.
- Even if the PR already has previous comments, you must still perform your own independent review and post your own comments based on the diff you analyze. Do not rely on or reference existing comments.
- Posting inline or summary comments (as relevant) is MANDATORY. Do not skip this step. 

### Parallel Subagent Strategy

This review can be accelerated using **up to 3 parallel subagents** (via the `Agent` tool). Instead of processing everything sequentially in the main context, split independent review tasks across subagents to save context window and speed up the process. Example parallelisation:

| Subagent | Scope | Agent Type |
|----------|-------|------------|
| 1 | Diff analysis + architecture review | `general-purpose` |
| 2 | Go-specific checks (concurrency, error handling, security) | `general-purpose` |
| 3 | Test coverage assessment + code quality checklist | `general-purpose` |

**When to parallelise:** Always use parallel subagents when the diff is non-trivial (more than a few files). For tiny diffs (1-2 files, cosmetic changes), a single-pass review is fine.

**How to parallelise:** Launch all independent subagents in a single message using multiple `Agent` tool calls. Each subagent should receive the diff (via `git diff`) and its specific review scope. After all subagents return, synthesize their findings into the final review and continue with steps 3-4 as above.

**Subagents for Posting Comments:** You can also delegate the posting of inline comments to subagents if needed, to save context in the main agent. Just ensure the subagent receives all necessary information (file paths, line numbers, comment bodies) to perform this task effectively.