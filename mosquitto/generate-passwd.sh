#!/bin/bash
# Generate password file for Mosquitto MQTT broker
# Creates a password file with credentials for the device service

set -e

CONFIG_DIR="$(cd "$(dirname "$0")" && pwd)/config"
PASSWD_FILE="$CONFIG_DIR/passwd"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo "Password file generation script for Mosquitto MQTT broker"
echo "========================================================="
echo ""

# Check if config directory exists
if [ ! -d "$CONFIG_DIR" ]; then
    echo "Creating config directory: $CONFIG_DIR"
    mkdir -p "$CONFIG_DIR"
fi

# Check if password file already exists
if [ -f "$PASSWD_FILE" ]; then
    echo -e "${YELLOW}Password file already exists: $PASSWD_FILE${NC}"
    echo "To regenerate, delete it first: rm $PASSWD_FILE"
    exit 0
fi

# Get username and password from environment or use defaults
USERNAME="${MQTT_USERNAME:-device-service}"
PASSWORD="${MQTT_PASSWORD:-}"

# If password not provided, generate a random one
if [ -z "$PASSWORD" ]; then
    # Generate a random password (16 characters, alphanumeric)
    PASSWORD=$(openssl rand -base64 12 | tr -d "=+/" | cut -c1-16)
    echo -e "${YELLOW}No password provided, generating random password${NC}"
fi

echo "Creating password file for user: $USERNAME"
echo ""

# Check if mosquitto_passwd is available, otherwise use Docker
if command -v mosquitto_passwd &> /dev/null; then
    # Use local mosquitto_passwd
    echo "Using local mosquitto_passwd command"
    mosquitto_passwd -c "$PASSWD_FILE" "$USERNAME" <<< "$PASSWORD" <<< "$PASSWORD"
elif command -v docker &> /dev/null; then
    # Use Docker to run mosquitto_passwd
    echo "Using Docker to run mosquitto_passwd"
    docker run --rm \
        -v "$CONFIG_DIR:/config" \
        eclipse-mosquitto:2-openssl \
        sh -c "echo -e '$PASSWORD\n$PASSWORD' | mosquitto_passwd -c /config/passwd $USERNAME"
else
    echo -e "${RED}Error: mosquitto_passwd command not found and Docker is not available${NC}"
    echo "Please install one of the following:"
    echo "  - mosquitto-clients package:"
    echo "    * macOS: brew install mosquitto"
    echo "    * Ubuntu/Debian: sudo apt-get install mosquitto-clients"
    echo "  - Docker: to use Docker-based password generation"
    exit 1
fi

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓${NC} Password file created: $PASSWD_FILE"
    echo ""
    echo -e "${GREEN}Credentials:${NC}"
    echo "  Username: $USERNAME"
    echo "  Password: $PASSWORD"
    echo ""
    echo "Set these in your docker-compose.test.yml:"
    echo "  MQTT_USERNAME=$USERNAME"
    echo "  MQTT_PASSWORD=$PASSWORD"
    echo ""
    echo -e "${YELLOW}Note: Keep these credentials secure!${NC}"
else
    echo -e "${RED}✗${NC} Failed to create password file"
    exit 1
fi

