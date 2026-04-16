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

exec ./bin/nano-review
