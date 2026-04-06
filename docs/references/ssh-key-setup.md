# SSH Key Setup for Git Clone

## Problem Statement

The Nano Review service needs to clone private GitHub repositories via SSH (`git@github.com:...`). Currently, the container has `openssh-client` installed but no SSH keys configured, causing git clone failures:

```
error="git clone git@github.com:kmmuntasir/AeroSense.git into /tmp/nano-review-3035378944: context canceled"
```

## Why NOT to Use .env Files for SSH Keys

**Private SSH keys should NEVER be stored in environment variables.** Here's why:

| Issue | Explanation |
|---|---|
| **Newline characters** | SSH private keys contain embedded newlines (`\n`) which don't work reliably in env files |
| **Permissions** | SSH requires files to have `0600` permissions; env vars can't enforce this |
| **Accidental commits** | `.env` files can accidentally be committed to git, exposing secrets |
| **Log leakage** | Env vars can appear in logs, error messages, and process listings (`ps aux`) |
| **Docker inspect** | Anyone with Docker access can see all env vars via `docker inspect` |
| **Shell escaping** | Special characters in keys break shell parsing when passed as env vars |

## Nano Review Approach: Build-Time Key Injection

Nano Review uses **build-time key injection** — SSH keys are stored in the repository (but gitignored) and copied into the Docker image during the build process.

### Architecture

```
Repository (local only, gitignored)
└── keys/
    ├── .gitignore          # Excludes private keys
    ├── deploy_key          # Private key (gitignored)
    ├── deploy_key.pub      # Public key (safe to commit)
    └── README.md           # Setup instructions

        ↓ (during docker build)

Container
└── /root/.ssh/
    ├── config              # SSH configuration
    └── deploy_key          # Copied private key (0600 permissions)

        ↓ (used by)

git clone git@github.com:owner/repo.git
```

## Quick Start

```bash
# 1. Generate SSH key pair
cd keys/
ssh-keygen -t ed25519 -C "nano-review" -f deploy_key -N ""
chmod 600 deploy_key

# 2. Add public key to GitHub
cat deploy_key.pub
# Go to: repo → Settings → Deploy keys → Add deploy key

# 3. Build and run
docker compose up --build -d
```

## Detailed Instructions

See [keys/README.md](../../keys/README.md) for comprehensive setup instructions.

## Security

- Private keys are gitignored at two levels (`keys/.gitignore` and `.gitignore`)
- Keys have `0600` permissions inside the container
- Deploy keys are scoped to specific repositories
- Use read-only deploy keys when possible

## References

- [GitHub Deploy Keys Documentation](https://docs.github.com/en/developers/overview/managing-deploy-keys)
- [SSH Configuration Manual](https://man.openbsd.org/ssh_config)
