#!/usr/bin/env bash
set -euo pipefail
exec "$(dirname "$0")/install-systemd.sh" --env-file .env.prod --name nano-review
