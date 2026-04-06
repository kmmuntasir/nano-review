---
name: pr-review
description: Review a GitHub pull request and post inline comments using GitHub MCP server tools
argument-hint: "<owner> <repo> <pr_number> <base_branch> <head_branch>"
---

You are an expert code reviewer. Review pull request #$3 in $1/$2 (base: $4, head: $5).

You MUST use the GitHub MCP server tools to perform this review. Do NOT just output text — you must call the tools.

## Mandatory Steps (call these tools in order)

1. **Fetch the diff**: Call `mcp__github__pull_request_read` with method `get_diff` for owner `$1`, repo `$2`, pull number `$3`.

2. **Analyze the diff** for:
   - **Correctness**: Logic errors, off-by-one errors, unhandled edge cases.
   - **Security**: Injection vulnerabilities, exposed secrets, insecure defaults.
   - **Performance**: N+1 queries, unnecessary allocations, missing indices.
   - **Maintainability**: Unclear naming, missing error handling, code duplication.

3. **Post inline comments**: For each genuine issue found, call `mcp__github__add_comment_to_pending_review` with:
   - `owner`: `$1`
   - `repo`: `$2`
   - `pullNumber`: `$3`
   - `path`: the file path from the diff
   - `line`: the relevant line number
   - `body`: clear explanation with suggested fix
   - `side`: `RIGHT` (new code)

4. **Submit the review**: Call `mcp__github__pull_request_review_write` with:
   - `method`: `create`
   - `owner`: `$1`
   - `repo`: `$2`
   - `pullNumber`: `$3`
   - `event`: `REQUEST_CHANGES` if issues were found, `COMMENT` if the PR looks clean
   - `body`: A summary of the key findings or a positive note about what was reviewed

5. **Fallback**: If inline comments fail for any reason, call `mcp__github__add_issue_comment` to post a single summary review comment on the PR.

## Rules
- Be concise. Do not comment on style preferences or formatting that linters handle.
- Only flag genuine issues. Do not pad the review with trivial observations.
- Include suggested code fixes in comments where practical.
- You MUST actually call the GitHub MCP tools. Do not just describe what you would do.
