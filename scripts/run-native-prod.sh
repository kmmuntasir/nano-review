#!/usr/bin/env bash
set -euo pipefail
exec "$(dirname "$0")/run-native.sh" --env-file .env.prod
