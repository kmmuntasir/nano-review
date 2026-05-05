#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

ENV_FILE=".env"
if [[ "${1:-}" == "--env-file" && -n "${2:-}" ]]; then
    ENV_FILE="$2"
fi

if [[ ! -f "$ENV_FILE" ]]; then
    echo "Error: $ENV_FILE not found. Run ./scripts/setup-native.sh first." >&2
    exit 1
fi

if [[ ! -x ./bin/nano-review ]]; then
    echo "Error: ./bin/nano-review not found. Run ./scripts/setup-native.sh first." >&2
    exit 1
fi

WORKDIR="$(pwd)"
ENV_FILE_ABS="$(pwd)/$ENV_FILE"

# Drop privileges to appuser when running as root.
# Claude CLI refuses --dangerously-skip-permissions as root.
# Sources env file *after* dropping privileges so env vars survive the switch.
if [ "$EUID" -eq 0 ] && id -u appuser &>/dev/null; then
    if command -v sudo &>/dev/null; then
        exec sudo -u appuser bash -c "cd '$WORKDIR' && set -a && source '$ENV_FILE_ABS' && set +a && exec ./bin/nano-review"
    elif command -v su &>/dev/null; then
        exec su -s /bin/bash appuser -c "cd '$WORKDIR' && set -a && source '$ENV_FILE_ABS' && set +a && exec ./bin/nano-review"
    else
        echo "Error: neither sudo nor su available to drop privileges to appuser." >&2
        exit 1
    fi
else
    set -a
    source "$ENV_FILE"
    set +a
    exec ./bin/nano-review
fi
