#!/usr/bin/env bash
set -euo pipefail

# Install nano-review as a systemd service.
# Usage: install-systemd.sh --env-file .env.prod --name nano-review

ENV_FILE=".env"
SERVICE_NAME="nano-review"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --env-file)
            ENV_FILE="$2"
            shift 2
            ;;
        --name)
            SERVICE_NAME="$2"
            shift 2
            ;;
        *)
            echo "Unknown argument: $1" >&2
            exit 1
            ;;
    esac
done

cd "$(dirname "$0")/.."
INSTALL_DIR="$(pwd)"
ENV_FILE_ABS="$INSTALL_DIR/$ENV_FILE"

echo "Installing systemd service: $SERVICE_NAME"
echo "Environment file: $ENV_FILE"
echo "Install directory: $INSTALL_DIR"
echo ""

# --- Pre-flight checks -------------------------------------------------------

if [[ ! -f "$ENV_FILE_ABS" ]]; then
    echo "Error: $ENV_FILE_ABS not found. Run the appropriate setup script first." >&2
    exit 1
fi

if [[ ! -x "$INSTALL_DIR/bin/nano-review" ]]; then
    echo "Error: $INSTALL_DIR/bin/nano-review not found. Run make native-build first." >&2
    exit 1
fi

if ! id -u appuser &>/dev/null; then
    echo "Error: appuser not found. Run the setup script first." >&2
    exit 1
fi

if [ "$EUID" -ne 0 ]; then
    echo "Error: This script must be run as root (or with sudo)." >&2
    exit 1
fi

# --- Ensure appuser can reach the install directory --------------------------
# When the repo lives inside /root/ (or another restricted directory),
# appuser cannot traverse the path. Open each directory component for traversal.
echo "Ensuring appuser can access $INSTALL_DIR ..."
path=""
IFS='/' read -ra parts <<< "$INSTALL_DIR"
for part in "${parts[@]}"; do
    [[ -z "$part" ]] && continue
    path="$path/$part"
    if [[ -d "$path" ]]; then
        chmod a+x "$path" 2>/dev/null || true
    fi
done

# Ensure data and logs directories exist and are owned by appuser
mkdir -p "$INSTALL_DIR/data" "$INSTALL_DIR/logs"
chown -R appuser:appuser "$INSTALL_DIR/data" "$INSTALL_DIR/logs"

# Ensure appuser can read the env file (may contain secrets — restrict to appuser)
chown root:appuser "$ENV_FILE_ABS"
chmod 640 "$ENV_FILE_ABS"

# --- Generate service file ----------------------------------------------------
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

cat > "$SERVICE_FILE" << EOF
[Unit]
Description=Nano Review - AI-powered PR code review
After=network.target

[Service]
Type=simple
User=appuser
Group=appuser
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${ENV_FILE_ABS}
ExecStart=${INSTALL_DIR}/bin/nano-review
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

# Graceful shutdown — matches main.go signal handler
TimeoutStopSec=30
KillSignal=SIGTERM

[Install]
WantedBy=multi-user.target
EOF

echo "✅ Service file written to $SERVICE_FILE"

# --- Activate -----------------------------------------------------------------
systemctl daemon-reload
systemctl enable "$SERVICE_NAME"

if systemctl is-active --quiet "$SERVICE_NAME"; then
    echo "Restarting $SERVICE_NAME ..."
    systemctl restart "$SERVICE_NAME"
else
    echo "Starting $SERVICE_NAME ..."
    systemctl start "$SERVICE_NAME"
fi

echo ""
echo "✅ $SERVICE_NAME is $(systemctl is-active "$SERVICE_NAME")"
echo ""
echo "Useful commands:"
echo "  systemctl status $SERVICE_NAME     # Check status"
echo "  journalctl -u $SERVICE_NAME -f     # Follow logs"
echo "  systemctl restart $SERVICE_NAME    # Restart after code update"
echo "  systemctl stop $SERVICE_NAME       # Stop"
echo ""
echo "Update workflow:"
echo "  git pull && make native-build && systemctl restart $SERVICE_NAME"
