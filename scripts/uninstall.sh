#!/bin/bash
#
# Alexander S3 Storage - Uninstaller (Linux/macOS)
#

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/alexander"
DATA_DIR="/var/lib/alexander"
SERVICE_USER="alexander"

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}[ERROR]${NC} This script must be run as root"
    exit 1
fi

echo ""
echo -e "${YELLOW}This will uninstall Alexander S3 Storage${NC}"
echo "The following will be removed:"
echo "  - Binaries from $INSTALL_DIR"
echo "  - Service configuration"
echo ""
read -p "Do you also want to remove data and config? [y/N] " -n 1 -r
echo ""
REMOVE_DATA=$REPLY

# Stop service
if [[ "$(uname -s)" == "Linux" ]]; then
    if systemctl is-active --quiet alexander 2>/dev/null; then
        log_info "Stopping alexander service..."
        systemctl stop alexander
    fi
    if [[ -f /etc/systemd/system/alexander.service ]]; then
        log_info "Removing systemd service..."
        systemctl disable alexander 2>/dev/null || true
        rm -f /etc/systemd/system/alexander.service
        systemctl daemon-reload
    fi
elif [[ "$(uname -s)" == "Darwin" ]]; then
    if [[ -f /Library/LaunchDaemons/com.alexander.server.plist ]]; then
        log_info "Stopping and removing launchd service..."
        launchctl unload /Library/LaunchDaemons/com.alexander.server.plist 2>/dev/null || true
        rm -f /Library/LaunchDaemons/com.alexander.server.plist
    fi
fi

# Remove binaries
log_info "Removing binaries..."
rm -f "$INSTALL_DIR/alexander-server"
rm -f "$INSTALL_DIR/alexander-admin"
rm -f "$INSTALL_DIR/alexander-migrate"

# Remove data if requested
if [[ $REMOVE_DATA =~ ^[Yy]$ ]]; then
    log_warn "Removing configuration and data..."
    rm -rf "$CONFIG_DIR"
    rm -rf "$DATA_DIR"
    
    # Remove user
    if id "$SERVICE_USER" &>/dev/null; then
        log_info "Removing service user..."
        userdel "$SERVICE_USER" 2>/dev/null || true
    fi
else
    log_info "Keeping configuration and data in:"
    echo "  - $CONFIG_DIR"
    echo "  - $DATA_DIR"
fi

echo ""
echo -e "${GREEN}Uninstallation complete!${NC}"
