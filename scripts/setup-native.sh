#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

has() { command -v "$1" &>/dev/null; }

# --- Prerequisites ---
missing=()
go_version() { go version 2>/dev/null | grep -oP 'go\d+\.\d+' | sed 's/go//'; }
if ! has go; then
    missing+=("go")
else
    major_minor=$(go_version)
    major=${major_minor%%.*}
    minor=${major_minor#*.}
    if (( major < 1 || (major == 1 && minor < 23) )); then
        missing+=("go (>=1.23 required, found $(go version))")
    fi
fi
has git || missing+=("git")
has claude || missing+=("claude")

if (( ${#missing[@]} )); then
    echo -e "${RED}Missing prerequisites:${NC}"
    for m in "${missing[@]}"; do
        echo "  - $m"
    done
    echo ""
    echo "Install instructions:"
    echo "  go:      https://go.dev/dl/"
    echo "  git:     https://git-scm.com/downloads"
    echo "  claude:  https://docs.anthropic.com/en/docs/claude-code/overview"
    exit 1
fi

echo -e "${GREEN}Prerequisites satisfied.${NC}"

# --- Directories ---
mkdir -p ./data ./logs ./bin
echo "Created ./data ./logs ./bin"

# --- Bootstrap .env ---
if [[ ! -f .env ]]; then
    cp .env.example .env
    echo "Copied .env.example -> .env"
else
    echo ".env already exists, skipping copy"
fi

native_defaults=(
    "NANO_DATA_DIR=./data"
    "NANO_LOG_DIR=./logs"
    "CLAUDE_CODE_PATH=claude"
    "DATABASE_PATH=./data/reviews.db"
    "AUTH_ENABLED=false"
)

for entry in "${native_defaults[@]}"; do
    key="${entry%%=*}"
    if ! grep -q "^${key}=" .env; then
        echo "$entry" >> .env
        echo "Appended $key to .env"
    else
        echo "$key already set in .env, skipping"
    fi
done

# --- Link Claude config ---
# --- Install Claude config ---
copy_config() {
    local src="$1" dst="$2" label="$3"
    if [[ -f "$dst" ]]; then
        echo "$label already exists at $dst, skipping"
    else
        mkdir -p "$(dirname "$dst")"
        cp "$src" "$dst"
        echo "Installed $label -> $dst"
    fi
}

copy_config "config/.claude/skills/pr-review/SKILL.md" "$HOME/.claude/skills/pr-review/SKILL.md" "pr-review skill"
copy_config "config/.claude/settings.json" "$HOME/.claude/settings.json" "Claude settings"

# --- Build ---
echo ""
echo "Building nano-review..."
CGO_ENABLED=0 go build -o ./bin/nano-review ./cmd/server
echo -e "${GREEN}Build successful: ./bin/nano-review${NC}"

echo ""
echo -e "${GREEN}Setup complete!${NC}"
echo ""
echo "Next steps:"
echo "  1. Edit .env and fill in WEBHOOK_SECRET, ANTHROPIC_AUTH_TOKEN, GITHUB_PAT"
echo "  2. Run: ./scripts/run-native.sh"
