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

echo "-------------------------"
echo "Setup complete! All dependencies are ready."