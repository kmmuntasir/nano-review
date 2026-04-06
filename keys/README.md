# SSH Keys for Git Clone

This directory contains SSH keys for cloning private GitHub repositories during the PR review process.

## ⚠️ Security Warning

**Private SSH keys are gitignored and should NEVER be committed to this repository.**

The `.gitignore` file in this directory ensures private keys are excluded from git.

## Setup Instructions

### 1. Generate a New SSH Key Pair

Generate an ed25519 key (recommended, modern and secure):

```bash
cd keys/
ssh-keygen -t ed25519 -C "nano-review" -f deploy_key -N ""
```

Or use RSA if ed25519 is not supported:

```bash
cd keys/
ssh-keygen -t rsa -b 4096 -C "nano-review" -f deploy_key -N ""
```

### 2. Add Public Key to GitHub

Copy the **public key** and add it as a deploy key in GitHub:

```bash
cat keys/deploy_key.pub
```

1. Go to your target repository on GitHub
2. Navigate to **Settings** → **Deploy keys**
3. Click **Add deploy key**
4. Paste the public key contents
5. Enable **Allow write access** (required for posting review comments)
6. Click **Add key**

### 3. Verify the Setup

Test that the SSH key works:

```bash
cd keys/
ssh -i deploy_key -T git@github.com
```

Expected output:
```
Hi username/repo! You've successfully authenticated...
```

## File Structure

```
keys/
├── .gitignore          # Excludes private keys from git
├── README.md           # This file
├── deploy_key          # Private key (gitignored, local only)
├── deploy_key.pub      # Public key (safe to commit)
└── example.pub         # Example public key format
```

## Key Rotation

To rotate a compromised key:

```bash
# 1. Generate new key
cd keys/
ssh-keygen -t ed25519 -C "nano-review" -f deploy_key -N ""

# 2. Add new public key to GitHub
# 3. Remove old key from GitHub
# 4. Rebuild Docker container
```

## Permissions

Private key files should have restrictive permissions:

```bash
chmod 600 keys/deploy_key
```

The Dockerfile will set correct permissions during the build process.
