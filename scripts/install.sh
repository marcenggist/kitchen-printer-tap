#!/bin/bash
#
# install.sh - Install kitchen-printer-tap service
#
# This script installs the tapd binary, configuration, and systemd service.
# It also creates the dedicated user and sets appropriate permissions.
#
# Usage: sudo ./install.sh
#

set -euo pipefail

# Configuration
SERVICE_USER="kptap"
SERVICE_GROUP="kptap"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/kitchen-printer-tap"
DATA_DIR="/var/lib/kitchen-printer-tap"
SYSTEMD_DIR="/etc/systemd/system"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Check root
if [[ $EUID -ne 0 ]]; then
    echo "Error: This script must be run as root" >&2
    exit 1
fi

echo "Installing kitchen-printer-tap..."
echo ""

# Check for binary
BINARY_PATH="$PROJECT_DIR/bin/tapd"
if [[ ! -f "$BINARY_PATH" ]]; then
    echo "Error: Binary not found at $BINARY_PATH" >&2
    echo "Please build first with: make build" >&2
    exit 1
fi

# Install dependencies
echo "Installing dependencies..."
apt-get update -qq
apt-get install -y -qq libpcap-dev bridge-utils

# Create service user
if ! id "$SERVICE_USER" &>/dev/null; then
    echo "Creating service user $SERVICE_USER..."
    useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
fi

# Add user to required groups for packet capture
usermod -aG pcap "$SERVICE_USER" 2>/dev/null || true

# Create directories
echo "Creating directories..."
mkdir -p "$CONFIG_DIR"
mkdir -p "$DATA_DIR"
mkdir -p "$INSTALL_DIR"

# Install binary
echo "Installing binary..."
cp "$BINARY_PATH" "$INSTALL_DIR/tapd"
chmod 755 "$INSTALL_DIR/tapd"

# Set capabilities for packet capture (instead of running as root)
echo "Setting capabilities..."
setcap cap_net_raw,cap_net_admin=eip "$INSTALL_DIR/tapd"

# Install configuration
echo "Installing configuration..."
if [[ ! -f "$CONFIG_DIR/config.yaml" ]]; then
    cp "$PROJECT_DIR/configs/config.yaml" "$CONFIG_DIR/config.yaml"
    echo "  Installed default config.yaml"
else
    echo "  Config already exists, not overwriting"
    cp "$PROJECT_DIR/configs/config.yaml" "$CONFIG_DIR/config.yaml.example"
    echo "  Installed config.yaml.example for reference"
fi

# Set permissions
echo "Setting permissions..."
chown -R "$SERVICE_USER:$SERVICE_GROUP" "$DATA_DIR"
chmod 750 "$DATA_DIR"
chown root:$SERVICE_GROUP "$CONFIG_DIR"
chmod 750 "$CONFIG_DIR"
chown root:$SERVICE_GROUP "$CONFIG_DIR/config.yaml"
chmod 640 "$CONFIG_DIR/config.yaml"

# Install systemd service
echo "Installing systemd service..."
cp "$PROJECT_DIR/systemd/kitchen-printer-tap.service" "$SYSTEMD_DIR/"
systemctl daemon-reload

echo ""
echo "Installation complete!"
echo ""
echo "Next steps:"
echo "  1. Edit configuration: sudo nano $CONFIG_DIR/config.yaml"
echo "  2. Set up bridge:      sudo $SCRIPT_DIR/setup-bridge.sh --persist"
echo "  3. Start service:      sudo systemctl start kitchen-printer-tap"
echo "  4. Enable on boot:     sudo systemctl enable kitchen-printer-tap"
echo ""
echo "Useful commands:"
echo "  View status:  sudo systemctl status kitchen-printer-tap"
echo "  View logs:    sudo journalctl -u kitchen-printer-tap -f"
echo "  Health check: curl http://127.0.0.1:8088/health"
