#!/usr/bin/env bash
set -euo pipefail
exec "$(dirname "$0")/setup-native.sh" --env-file .env.stage
