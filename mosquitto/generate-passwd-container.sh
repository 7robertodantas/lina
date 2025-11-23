#!/bin/sh
# Generate password file for Mosquitto MQTT broker (container version)
# Creates a password file with credentials for the device service

set -e

PASSWD_FILE="/mosquitto/config/passwd"

# Get username and password from environment or use defaults
USERNAME="${MQTT_USERNAME:-device-service}"
PASSWORD="${MQTT_PASSWORD:-}"

# If password not provided, generate a random one
if [ -z "$PASSWORD" ]; then
    # Generate a random password (16 characters, alphanumeric)
    PASSWORD=$(openssl rand -base64 12 | tr -d "=+/" | cut -c1-16)
    echo "No password provided, generating random password"
fi

# Check if password file already exists
if [ -f "$PASSWD_FILE" ]; then
    echo "Password file already exists: $PASSWD_FILE"
    exit 0
fi

echo "Creating password file for user: $USERNAME"

# Use mosquitto_passwd (should be available in the container)
echo -e "$PASSWORD\n$PASSWORD" | mosquitto_passwd -c "$PASSWD_FILE" "$USERNAME"

if [ $? -eq 0 ]; then
    # Set proper permissions and ownership so mosquitto user can read it
    # mosquitto requires 0700 permissions (read/write/execute for owner only)
    # mosquitto user typically has UID 1883 in eclipse-mosquitto image
    if id -u mosquitto >/dev/null 2>&1; then
        chown mosquitto:mosquitto "$PASSWD_FILE"
        chmod 0700 "$PASSWD_FILE"
    else
        # If mosquitto user doesn't exist, use 0600 (read/write for owner only)
        chmod 0600 "$PASSWD_FILE"
    fi
    # Also ensure the config directory is accessible
    chmod 755 "$(dirname "$PASSWD_FILE")"
    
    echo "Password file created successfully: $PASSWD_FILE"
    echo "Username: $USERNAME"
    # echo "Password: $PASSWORD"
else
    echo "Failed to create password file"
    exit 1
fi

