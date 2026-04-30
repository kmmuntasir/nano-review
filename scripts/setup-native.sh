#!/bin/bash

# Determine if we need sudo
SUDO_CMD=""
if [ "$EUID" -ne 0 ]; then
    # Not running as root, we need sudo
    SUDO_CMD="sudo"
fi

# Function to detect the package manager and install a standard package
install_system_package() {
    PACKAGE=$1
    
    # Handle the fact that Golang is named 'golang' on apt, but 'go' on brew
    if [ "$PACKAGE" == "go" ] && command -v apt-get &> /dev/null; then
        PACKAGE="golang"
    fi

    if command -v apt-get &> /dev/null; then
        echo "Detected apt. Installing $PACKAGE..."
        $SUDO_CMD apt-get update
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

echo "Starting system checks..."
echo "-------------------------"

# 1. Check and install Git
if ! command -v git &> /dev/null; then
    echo "❌ Git is missing. Attempting to install..."
    install_system_package git
else
    echo "✅ Git is already installed."
fi

# 2. Check and install Golang
if ! command -v go &> /dev/null; then
    echo "❌ Golang is missing. Attempting to install..."
    install_system_package go
else
    echo "✅ Golang is already installed."
fi

# 3. Check and install Claude Code CLI
if ! command -v claude &> /dev/null; then
    echo "❌ Claude Code CLI is missing. Installing the latest native version..."
    # Anthropic's official native installation command for macOS and Linux/WSL
    curl -fsSL https://claude.ai/install.sh | bash
else
    echo "✅ Claude Code CLI is already installed."
fi

# 4. Check and install jq (required by RTK hooks)
if ! command -v jq &> /dev/null; then
    echo "❌ jq is missing. Attempting to install..."
    install_system_package jq
else
    echo "✅ jq is already installed."
fi

# 5. Check and install Node.js (required by Caveman hooks)
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

# 6. Install Caveman plugin (merges hooks into ~/.claude/settings.json)
echo "Installing Caveman plugin..."
curl -fsSL https://raw.githubusercontent.com/JuliusBrussee/caveman/main/hooks/install.sh | bash
echo "✅ Caveman plugin installed."

# 7. Install RTK (Rust Token Killer)
if ! command -v rtk &> /dev/null; then
    echo "❌ RTK is missing. Installing..."
    curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh
    # Ensure RTK is on PATH for current session
    export PATH="$HOME/.local/bin:$PATH"
else
    echo "✅ RTK is already installed."
fi

# 8. Configure RTK hook (merges PreToolUse hook into ~/.claude/settings.json)
echo "Configuring RTK hook..."
rtk init -g --auto-patch

# 9. Verify installations
echo ""
echo "Verifying Caveman + RTK..."
node --version
rtk --version
echo "✅ Caveman + RTK verified."

echo "-------------------------"
echo "Setup complete! All dependencies are ready."