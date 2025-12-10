#!/bin/bash
# Script to copy measurement log files from remote environment to local folder
#
# Usage:
#   ./copy-measurements.sh user@hostname
#   ./copy-measurements.sh user@hostname -p 2222
#
#   Help:   ./copy-measurements.sh help

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

REMOTE_DIR="~/lnpay"
LOCAL_DIR="./measurements"

# Show usage/help
show_usage() {
    echo "Copy Measurements Script"
    echo "======================"
    echo ""
    echo "Usage:"
    echo "  ./copy-measurements.sh <ssh_target> [ssh_options]"
    echo ""
    echo "Examples:"
    echo "  ./copy-measurements.sh user@hostname"
    echo "  ./copy-measurements.sh user@hostname -p 2222"
    echo ""
    echo "This script will copy all measurement log files from the remote"
    echo "machine to a local ./measurements/ directory."
    echo ""
    exit 0
}

# Parse arguments
SSH_TARGET="${1:-}"

# Check if help requested
if [ "$SSH_TARGET" = "help" ] || [ "$SSH_TARGET" = "-h" ] || [ "$SSH_TARGET" = "--help" ]; then
    show_usage
fi

# Require SSH target
if [ -z "$SSH_TARGET" ]; then
    echo -e "${RED}ERROR: SSH target required${NC}"
    echo ""
    show_usage
fi

# Collect remaining SSH options (e.g., -p 2222)
SSH_OPTS="${@:2}"

echo -e "${BLUE}Copying Measurement Logs${NC}"
echo "=============================="
echo "Source: $SSH_TARGET:$REMOTE_DIR"
echo "Destination: $LOCAL_DIR"
[ -n "$SSH_OPTS" ] && echo "SSH Options: $SSH_OPTS"
echo ""

# Create local measurements directory
mkdir -p "$LOCAL_DIR"

# List of log/CSV files to copy
LOG_FILES=(
    "docker_stats.csv"
    "mpstat.csv"
    "pidstat.csv"
    "vmstat.log"
    "vmstat.csv"
    "iostat.csv"
    "measurement_stdout.log"
    "measurement_stderr.log"
)

# Copy each log file if it exists on remote
COPIED_COUNT=0
MISSING_COUNT=0

for log_file in "${LOG_FILES[@]}"; do
    remote_path="$REMOTE_DIR/$log_file"
    
    # Check if file exists on remote
    if ssh $SSH_OPTS "$SSH_TARGET" "test -f $remote_path" 2>/dev/null; then
        echo "Copying $log_file..."
        scp $SSH_OPTS "$SSH_TARGET:$remote_path" "$LOCAL_DIR/"
        COPIED_COUNT=$((COPIED_COUNT + 1))
    else
        echo -e "${YELLOW}⚠ $log_file not found on remote (skipping)${NC}"
        MISSING_COUNT=$((MISSING_COUNT + 1))
    fi
done

echo ""
echo -e "${GREEN}=========================================="
echo -e "Copy Complete!${NC}"
echo -e "${GREEN}=========================================="
echo ""
echo "Files copied: $COPIED_COUNT"
[ $MISSING_COUNT -gt 0 ] && echo -e "${YELLOW}Files not found: $MISSING_COUNT${NC}"
echo ""
echo "Measurement logs are now in: $LOCAL_DIR"
echo ""

