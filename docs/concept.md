# Headless Asynchronous PR Reviewer Micro Backend

- A github repo will be set up with Github Actions to make an api call to a server, on each pull request. The api call will pass on the Repo SSH Url, PR number, base branch and the head branch. The server will respond HTTP Ok immediately, so the Github Action run duration will be very short.

- The Setup:
  - The server will be an Ubuntu machine.
  - The server will already be configured with git and SSH access to that github repository.
  - The server will already have Claude Code CLI installed and configured with API Key for authentication.
  - The Claude Code installation in the server will be configured with some global Claude Agents/Skills and the official Github MCP server (configured to access that repository).
  
- The server's api will:
  - Receive the call and parse the data (Repo Url, PR number, base branch and the head branch). 
  - Clone the repository in a /tmp/<a-unique-run-id> directory
  - Trigger a headless instance of Claude Code CLI in bypass permission mode, inject a pre-configured system prompt that will utilize a /pr-review skill to review that specific pull request.
  - The /pr-review skill will make the Claude Code CLI Headless instance to use the official github MCP server to write inline comments to that PR and request for changes if needed, or write a complete review report if no changes are required. If for some reason inline comments fail, the agent will put a summary review report as a fallback.
  - Ephemeral Cleanup: To prevent storage exhaustion, the system must forcefully delete the temporary directory after completing this cycle.

# Thoughts
- This micro backend will use Docker. We need to set up development, staging and production dockerfiles and/or compose files as necessary.
- The docker build process should handle the installation of curl, git, claude code cli (with the updated shell installer, not the deprecated npm installer) and any other necessary dependencies, along with all the configurations (ssh or anything else).
- Env values (`ANTHROPIC_BASE_URL` or similar values) and Secrets (`ANTHROPIC_API_KEY`, `GITHUB_PAT` etc) should not be hardcoded.
- The source code will have a `.claude` directory that will be copied to the docker container's home directory so that the internal claude code installation will have those configurations (Agents, Skills, MCPs, Rules, etc) by default.
- The micro backend can be written in Golang for lighter system footprint.
- The webhook should be protected using some kind of secret so that unauthenticated calls can be prevented.