#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ ! -f .env ]]; then
    echo "Error: .env not found. Run ./scripts/setup-native.sh first." >&2
    exit 1
fi

if [[ ! -x ./bin/nano-review ]]; then
    echo "Error: ./bin/nano-review not found. Run ./scripts/setup-native.sh first." >&2
    exit 1
fi

set -a
source .env
set +a

# Drop privileges to appuser when running as root.
# Claude CLI refuses --dangerously-skip-permissions as root.
if [ "$EUID" -eq 0 ] && id -u appuser &>/dev/null; then
    if command -v sudo &>/dev/null; then
        exec sudo -u appuser -- ./bin/nano-review
    elif command -v su &>/dev/null; then
        exec su -s /bin/bash appuser -c "cd '$(pwd)' && exec ./bin/nano-review"
    else
        echo "Error: neither sudo nor su available to drop privileges to appuser." >&2
        exit 1
    fi
else
    exec ./bin/nano-review
fi
