#!/bin/bash
#
# rollback-bridge.sh - Remove bridge and restore standalone interfaces
#
# This script removes the br0 bridge and restores eth0/eth1 to their
# original standalone state.
#
# Usage: sudo ./rollback-bridge.sh [--remove-persistent]
#   --remove-persistent: Also remove persistent configuration files
#

set -euo pipefail

# Configuration
BRIDGE_NAME="br0"
ETH_SWITCH="eth0"
ETH_PRINTER="eth1"
REMOVE_PERSIST=false

# Parse arguments
for arg in "$@"; do
    case $arg in
        --remove-persistent)
            REMOVE_PERSIST=true
            shift
            ;;
        *)
            ;;
    esac
done

# Check root
if [[ $EUID -ne 0 ]]; then
    echo "Error: This script must be run as root" >&2
    exit 1
fi

echo "Rolling back bridge configuration..."

# Check if bridge exists
if ! ip link show "$BRIDGE_NAME" &>/dev/null; then
    echo "Bridge $BRIDGE_NAME does not exist, nothing to rollback"
else
    # Bring down the bridge
    echo "Bringing down bridge..."
    ip link set "$BRIDGE_NAME" down 2>/dev/null || true

    # Remove interfaces from bridge
    echo "Removing interfaces from bridge..."
    ip link set "$ETH_SWITCH" nomaster 2>/dev/null || true
    ip link set "$ETH_PRINTER" nomaster 2>/dev/null || true

    # Delete the bridge
    echo "Deleting bridge..."
    ip link delete "$BRIDGE_NAME" 2>/dev/null || true
fi

# Restore interfaces
echo "Restoring interfaces..."
for iface in "$ETH_SWITCH" "$ETH_PRINTER"; do
    if ip link show "$iface" &>/dev/null; then
        # Disable promiscuous mode
        ip link set "$iface" promisc off 2>/dev/null || true
        # Bring interface up
        ip link set "$iface" up 2>/dev/null || true
    fi
done

# Remove persistent configuration if requested
if $REMOVE_PERSIST; then
    echo "Removing persistent configuration..."

    if [[ -f /etc/network/interfaces.d/kitchen-printer-tap ]]; then
        rm -f /etc/network/interfaces.d/kitchen-printer-tap
        echo "  Removed /etc/network/interfaces.d/kitchen-printer-tap"
    fi

    if [[ -f /etc/sysctl.d/99-kitchen-printer-tap.conf ]]; then
        rm -f /etc/sysctl.d/99-kitchen-printer-tap.conf
        echo "  Removed /etc/sysctl.d/99-kitchen-printer-tap.conf"
    fi
fi

echo ""
echo "Bridge rollback complete!"
echo ""
echo "Interface status:"
ip -br link show "$ETH_SWITCH" 2>/dev/null || echo "  $ETH_SWITCH: not found"
ip -br link show "$ETH_PRINTER" 2>/dev/null || echo "  $ETH_PRINTER: not found"
echo ""
echo "Note: You may need to reconfigure network settings for eth0/eth1"
echo "depending on your original configuration (DHCP, static IP, etc.)"
echo ""
echo "To restore DHCP on an interface:"
echo "  sudo dhclient eth0"
