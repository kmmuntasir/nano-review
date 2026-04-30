#!/bin/bash

# Prevent interactive prompts during package installation
export DEBIAN_FRONTEND=noninteractive

# Determine if we need sudo
SUDO_CMD=""
if [ "$EUID" -ne 0 ]; then
    # Not running as root, we need sudo
    SUDO_CMD="sudo"
fi

# Pre-seed tzdata to prevent interactive prompt
if command -v apt-get &> /dev/null; then
    echo "tzdata tzdata/Areas select Etc" | $SUDO_CMD debconf-set-selections 2>/dev/null || true
    echo "tzdata tzdata/Zones/Etc select UTC" | $SUDO_CMD debconf-set-selections 2>/dev/null || true
fi

_PKG_UPDATED=false

pkg_update_once() {
    if [ "$_PKG_UPDATED" = true ]; then
        return 0
    fi

    if command -v apt-get &> /dev/null; then
        echo "Updating apt package index..."
        $SUDO_CMD apt-get update -qq
    elif command -v dnf &> /dev/null; then
        echo "Updating dnf package cache..."
        $SUDO_CMD dnf makecache -y --quiet 2>/dev/null || true
    fi

    _PKG_UPDATED=true
}

ensure_local_bin_in_path() {
    local bin_dir="$1"

    # Add to current session
    export PATH="$bin_dir:$PATH"

    # Persist to shell profile (idempotent) — only for appuser on VPS
    if [ "$bin_dir" = "/home/appuser/.local/bin" ]; then
        local profile_file=""
        if [ -f "/home/appuser/.zshrc" ]; then
            profile_file="/home/appuser/.zshrc"
        elif [ -f "/home/appuser/.bashrc" ]; then
            profile_file="/home/appuser/.bashrc"
        fi
        if [ -n "$profile_file" ] && ! grep -qF '/home/appuser/.local/bin' "$profile_file"; then
            echo "" >> "$profile_file"
            echo 'export PATH="/home/appuser/.local/bin:$PATH"' >> "$profile_file"
        fi
    fi
}

# Function to detect the package manager and install a standard package
install_system_package() {
    PACKAGE=$1

    # Handle the fact that Golang is named 'golang' on apt, but 'go' on brew
    if [ "$PACKAGE" == "go" ] && command -v apt-get &> /dev/null; then
        PACKAGE="golang"
    fi

    if command -v apt-get &> /dev/null; then
        echo "Detected apt. Installing $PACKAGE..."
        pkg_update_once
        $SUDO_CMD apt-get install -y "$PACKAGE"
    elif command -v brew &> /dev/null; then
        echo "Detected Homebrew. Installing $PACKAGE..."
        # Homebrew explicitly prefers NOT to be run with sudo
        brew install "$PACKAGE"
    elif command -v dnf &> /dev/null; then
        echo "Detected dnf. Installing $PACKAGE..."
        $SUDO_CMD dnf install -y "$PACKAGE"
    elif command -v pacman &> /dev/null; then
        echo "Detected pacman. Installing $PACKAGE..."
        $SUDO_CMD pacman -S --noconfirm "$PACKAGE"
    else
        echo "Error: Could not find a supported package manager (apt, brew, dnf, pacman)."
        echo "Please install $PACKAGE manually."
        exit 1
    fi
}

# Run a command as appuser (handles root vs non-root)
run_as_appuser() {
    if [ "$EUID" -eq 0 ]; then
        su - appuser -c "$*"
    else
        sudo -u appuser bash -c "$*"
    fi
}

echo "Starting system checks..."
echo "-------------------------"

# 1. Create appuser (same user as Docker) for Claude Code compatibility.
# Claude CLI refuses --dangerously-skip-permissions when run as root.
# Create early so all user-level tools install under appuser.
if ! id -u appuser &>/dev/null; then
    echo "Creating appuser..."
    $SUDO_CMD useradd -m -s /bin/bash appuser
    echo "✅ appuser created."
else
    echo "✅ appuser already exists."
fi

# Add appuser's .local/bin to PATH for tool discovery
ensure_local_bin_in_path "/home/appuser/.local/bin"

# 2. Check and install Git (system package — needs root)
if ! command -v git &> /dev/null; then
    echo "❌ Git is missing. Attempting to install..."
    install_system_package git
else
    echo "✅ Git is already installed."
fi

# 3. Check and install Golang (system package — needs root)
if ! command -v go &> /dev/null; then
    echo "❌ Golang is missing. Attempting to install..."
    install_system_package go
else
    echo "✅ Golang is already installed."
fi

# 4. Install Claude Code CLI as appuser (not root)
if ! run_as_appuser 'command -v claude &>/dev/null'; then
    echo "Installing Claude Code CLI as appuser..."
    run_as_appuser 'CI=1 curl -fsSL https://claude.ai/install.sh | bash'
    echo "✅ Claude Code CLI installed for appuser."
else
    echo "✅ Claude Code CLI already installed for appuser."
fi

# Copy Claude Code config to appuser home (mirrors Docker COPY step)
if [ -d "config/.claude" ]; then
    echo "Installing Claude Code config for appuser..."
    $SUDO_CMD rm -rf /home/appuser/.claude/skills /home/appuser/.claude/settings.json
    $SUDO_CMD cp -r config/.claude/ /home/appuser/.claude/
    $SUDO_CMD chown -R appuser:appuser /home/appuser/.claude/
    if [ -d "/home/appuser/.claude/skills" ]; then
        echo "✅ Claude Code config installed for appuser."
    else
        echo "⚠️  Skills directory not found after copy, creating manually..."
        $SUDO_CMD mkdir -p /home/appuser/.claude/skills
        $SUDO_CMD cp -r config/.claude/skills/* /home/appuser/.claude/skills/ 2>/dev/null || true
        $SUDO_CMD chown -R appuser:appuser /home/appuser/.claude/skills
        if [ -d "/home/appuser/.claude/skills/pr-review" ]; then
            echo "✅ Claude Code config installed (manual copy)."
        else
            echo "❌ Failed to install skills. pr-review skill will not be available."
        fi
    fi
fi

# 5. Check and install jq (system package — needs root, required by RTK hooks)
if ! command -v jq &> /dev/null; then
    echo "❌ jq is missing. Attempting to install..."
    install_system_package jq
else
    echo "✅ jq is already installed."
fi

# 6. Check and install Node.js (system package — needs root, required by Caveman hooks)
if ! command -v node &> /dev/null; then
    echo "❌ Node.js is missing. Attempting to install..."
    if command -v brew &> /dev/null; then
        brew install node
    elif command -v apt-get &> /dev/null; then
        curl -fsSL https://deb.nodesource.com/setup_24.x | $SUDO_CMD bash -
        $SUDO_CMD apt-get install -y nodejs
    elif command -v dnf &> /dev/null; then
        $SUDO_CMD dnf install -y nodejs
    elif command -v pacman &> /dev/null; then
        $SUDO_CMD pacman -S --noconfirm nodejs npm
    else
        echo "Error: Could not install Node.js. Install Node.js 24.x LTS manually."
        exit 1
    fi
else
    echo "✅ Node.js is already installed."
fi

# 7. Install Caveman plugin as appuser (merges hooks into appuser's ~/.claude/settings.json)
echo "Installing Caveman plugin for appuser..."
run_as_appuser 'curl -fsSL https://raw.githubusercontent.com/JuliusBrussee/caveman/main/hooks/install.sh | bash'
echo "✅ Caveman plugin installed for appuser."

# 8. Install RTK (Rust Token Killer) as appuser
if ! run_as_appuser 'command -v rtk &>/dev/null'; then
    echo "❌ RTK is missing. Installing for appuser..."
    run_as_appuser 'curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh'
    echo "✅ RTK installed for appuser."
else
    echo "✅ RTK is already installed for appuser."
fi

# 9. Configure RTK hook as appuser (merges PreToolUse hook into appuser's ~/.claude/settings.json)
echo "Configuring RTK hook for appuser..."
run_as_appuser 'export PATH="/home/appuser/.local/bin:$PATH" && echo "y" | rtk init -g --auto-patch'

# 10. Verify installations
echo ""
echo "Verifying Caveman + RTK for appuser..."
node --version
run_as_appuser 'export PATH="/home/appuser/.local/bin:$PATH" && rtk --version'
echo "✅ Caveman + RTK verified."

# 11. Create data and logs directories, chown to appuser
mkdir -p ./data ./logs
if id -u appuser &>/dev/null; then
    $SUDO_CMD chown -R appuser:appuser ./data ./logs
fi
echo "✅ Data and logs directories created."

# 12. Generate .env if missing
if [[ ! -f .env ]]; then
    echo "Generating .env with native defaults..."
    cp .env.example .env
    # Ensure CLAUDE_CODE_PATH points to appuser's Claude binary
    if grep -q '^CLAUDE_CODE_PATH=' .env; then
        sed -i 's|^CLAUDE_CODE_PATH=.*|CLAUDE_CODE_PATH=/home/appuser/.local/bin/claude|' .env
    else
        echo "CLAUDE_CODE_PATH=/home/appuser/.local/bin/claude" >> .env
    fi
    # Set native paths
    if grep -q '^# NANO_DATA_DIR=' .env; then
        sed -i 's|^# NANO_DATA_DIR=.*|NANO_DATA_DIR=./data|' .env
    fi
    if grep -q '^# NANO_LOG_DIR=' .env; then
        sed -i 's|^# NANO_LOG_DIR=.*|NANO_LOG_DIR=./logs|' .env
    fi
    if grep -q '^# DATABASE_PATH=' .env; then
        sed -i 's|^# DATABASE_PATH=.*|DATABASE_PATH=./data/reviews.db|' .env
    fi
    echo "✅ .env generated. Edit .env to set WEBHOOK_SECRET, ANTHROPIC_AUTH_TOKEN, GITHUB_PAT."
else
    # Ensure CLAUDE_CODE_PATH is set correctly even if .env exists
    if ! grep -q '^CLAUDE_CODE_PATH=/home/appuser/.local/bin/claude' .env; then
        if grep -q '^CLAUDE_CODE_PATH=' .env; then
            sed -i 's|^CLAUDE_CODE_PATH=.*|CLAUDE_CODE_PATH=/home/appuser/.local/bin/claude|' .env
        else
            echo "CLAUDE_CODE_PATH=/home/appuser/.local/bin/claude" >> .env
        fi
        echo "✅ CLAUDE_CODE_PATH updated in .env."
    fi
fi

# 13. Build binary
echo "Building nano-review..."
go build -o ./bin/nano-review ./cmd/server
echo "✅ Binary built to ./bin/nano-review."

echo "-------------------------"
echo "Setup complete! Run 'make native-run' to start."

# Source profile so PATH changes take effect in the calling shell
if [ -f "$HOME/.bashrc" ]; then
    source "$HOME/.bashrc" 2>/dev/null || true
elif [ -f "$HOME/.zshrc" ]; then
    source "$HOME/.zshrc" 2>/dev/null || true
fi
