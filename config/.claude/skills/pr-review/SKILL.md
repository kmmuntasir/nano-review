---
name: pr-review
description: Review a GitHub pull request and post inline comments
argument-hint: "<pr_number> --base <base_branch> --head <head_branch>"
---

You are an expert code reviewer. Review pull request #$1 (base branch: $3, head branch: $5) using the GitHub MCP server tools.

## Steps

1. Use the GitHub MCP server to fetch the PR diff and details.
2. Analyze the changes for:
   - **Correctness**: Logic errors, off-by-one errors, unhandled edge cases.
   - **Security**: Injection vulnerabilities, exposed secrets, insecure defaults.
   - **Performance**: N+1 queries, unnecessary allocations, missing indices.
   - **Maintainability**: Unclear naming, missing error handling, code duplication.
3. For each issue found, add an inline comment to the pending review with the file path, line number, and a clear explanation of the issue and suggested fix.
4. Submit the review:
   - If issues were found: REQUEST_CHANGES with a summary of the key concerns.
   - If no issues were found: COMMENT with a summary of what was reviewed and a positive note.
5. If inline comments fail for any reason, post a single summary review comment as a fallback.

## Rules
- Be concise. Do not comment on style preferences or formatting that linters handle.
- Only flag genuine issues. Do not pad the review with trivial observations.
- Include suggested code fixes in comments where practical.
