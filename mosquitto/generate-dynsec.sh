#!/bin/sh
# Set Default Admin Credentials for Dynamic Security Plugin Configuration
DEFAULT_DYNSEC_ADMIN=admin
DEFAULT_DYNSEC_ADMIN_PASSWORD=admin

# Set values if provided via Environment Variables in the Docker Init Container
MQTT_DYNSEC_ADMIN_USER=${MQTT_DYNSEC_ADMIN_USER:-$DEFAULT_DYNSEC_ADMIN}
MQTT_DYNSEC_ADMIN_PASSWORD=${MQTT_DYNSEC_ADMIN_PASSWORD:-$DEFAULT_DYNSEC_ADMIN_PASSWORD}

# Check the mode
MODE=${1:-full}

if [ "$MODE" = "init-only" ]; then
    # Only initialize the dynamic-security.json file (doesn't need a running broker)
    echo "Initializing dynamic-security.json file..."
    mosquitto_ctrl dynsec init /mosquitto/data/dynamic-security.json ${MQTT_DYNSEC_ADMIN_USER} ${MQTT_DYNSEC_ADMIN_PASSWORD}
    exit $?
fi

# Configure mode - requires a running broker
if [ "$MODE" != "configure" ] && [ "$MODE" != "full" ]; then
    echo "Unknown mode: $MODE"
    exit 1
fi

# If full mode, initialize first
if [ "$MODE" = "full" ]; then
    echo "Initializing dynamic-security.json file..."
    mosquitto_ctrl dynsec init /mosquitto/data/dynamic-security.json ${MQTT_DYNSEC_ADMIN_USER} ${MQTT_DYNSEC_ADMIN_PASSWORD}
    if [ $? -ne 0 ]; then
        echo "Failed to initialize dynamic-security.json"
        exit 1
    fi
fi

# Connection options for mosquitto_ctrl (using TLS on localhost)
CTRL_OPTS="-h 127.0.0.1 -p 8883 --cafile /mosquitto/certs/ca.crt -u ${MQTT_DYNSEC_ADMIN_USER} -P ${MQTT_DYNSEC_ADMIN_PASSWORD}"

# Device service user and role creation is now handled by the device service itself
# via ProvisionDeviceService() function. This script only initializes the dynamic-security.json
# file when run in "init-only" mode.

# The following code is commented out because the device service handles its own provisioning:
# - Device service user creation
# - device_service_role creation and ACLs
# - Role assignment to device service user

# If you need to manually create the device service user, uncomment the code below:

# Add user with provided credentials, defaulting as needed
# MQTT_USERNAME=${MQTT_USERNAME:-device-service}
# MQTT_PASSWORD=${MQTT_PASSWORD:-}
#
# # If password is not provided, generate a random one (16 alphanum chars)
# if [ -z "$MQTT_PASSWORD" ]; then
#     MQTT_PASSWORD=$(openssl rand -base64 12 | tr -d "=+/" | cut -c1-16)
#     echo "No MQTT_PASSWORD provided, generated random password: $MQTT_PASSWORD"
# fi
#
# # Create the user
# echo "Creating user: $MQTT_USERNAME"
# # Create client first (without password to avoid -p flag conflict with port)
# mosquitto_ctrl $CTRL_OPTS dynsec createClient "$MQTT_USERNAME"
# if [ $? -ne 0 ]; then
#     echo "Failed to create user"
#     exit 1
# fi
#
# # Set the password separately using setClientPassword
# echo "Setting password for user: $MQTT_USERNAME"
# mosquitto_ctrl $CTRL_OPTS dynsec setClientPassword "$MQTT_USERNAME" "$MQTT_PASSWORD"
# if [ $? -ne 0 ]; then
#     echo "Failed to set password for user"
#     exit 1
# fi
#
# # Create healtcheck_role for test/health topic access
# echo "Creating role: healtcheck_role for test/health topic access"
# mosquitto_ctrl $CTRL_OPTS dynsec createRole healtcheck_role
# if [ $? -ne 0 ]; then
#     echo "Failed to create role healtcheck_role"
#     exit 1
# fi
#
# # Allow subscribe/publish to test/health
# # aclspec format: <acltype> <topicFilter> allow|deny
# echo "Adding ACL for healtcheck_role to allow subscribe,publish for topic test/health"
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL healtcheck_role publishClientSend test/health allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add publish ACL for healtcheck_role"
#     exit 1
# fi
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL healtcheck_role subscribePattern test/health allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add subscribe ACL for healtcheck_role"
#     exit 1
# fi
#
# # Create device_service_role for specific device topic access
# echo "Creating role: device_service_role for device service topic access"
# mosquitto_ctrl $CTRL_OPTS dynsec createRole device_service_role
# if [ $? -ne 0 ]; then
#     echo "Failed to create role device_service_role"
#     exit 1
# fi
#
# # Add subscribe ACLs for device_service_role
# # aclspec format: <acltype> <topicFilter> allow|deny
# echo "Adding subscribe ACLs for device_service_role"
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL device_service_role subscribePattern '/devices/#/heartbeat' allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add subscribe ACL for /devices/#/heartbeat"
#     exit 1
# fi
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL device_service_role subscribePattern '/devices/#/usage' allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add subscribe ACL for /devices/#/usage"
#     exit 1
# fi
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL device_service_role subscribePattern '/devices/#/request/authorize' allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add subscribe ACL for /devices/#/request/authorize"
#     exit 1
# fi
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL device_service_role subscribePattern '/devices/#/request/invoice' allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add subscribe ACL for /devices/#/request/invoice"
#     exit 1
# fi
#
# # Add publish ACLs for device_service_role
# echo "Adding publish ACLs for device_service_role"
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL device_service_role publishClientSend '/devices/#/config' allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add publish ACL for /devices/#/config"
#     exit 1
# fi
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL device_service_role publishClientSend '/devices/#/control' allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add publish ACL for /devices/#/control"
#     exit 1
# fi
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL device_service_role publishClientSend '/devices/#/balance' allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add publish ACL for /devices/#/balance"
#     exit 1
# fi
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL device_service_role publishClientSend '/devices/#/response/authorize' allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add publish ACL for /devices/#/response/authorize"
#     exit 1
# fi
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL device_service_role publishClientSend '/devices/#/response/invoice' allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add publish ACL for /devices/#/response/invoice"
#     exit 1
# fi
# mosquitto_ctrl $CTRL_OPTS dynsec addRoleACL device_service_role publishClientSend '/devices/#/events/invoice' allow 1
# if [ $? -ne 0 ]; then
#     echo "Failed to add publish ACL for /devices/#/events/invoice"
#     exit 1
# fi
#
# # Assign both roles to the created user
# echo "Assigning role healtcheck_role to user $MQTT_USERNAME"
# mosquitto_ctrl $CTRL_OPTS dynsec addClientRole "$MQTT_USERNAME" healtcheck_role 1
# if [ $? -ne 0 ]; then
#     echo "Failed to assign role healtcheck_role to user"
#     exit 1
# fi
#
# echo "Assigning role device_service_role to user $MQTT_USERNAME"
# mosquitto_ctrl $CTRL_OPTS dynsec addClientRole "$MQTT_USERNAME" device_service_role 1
# if [ $? -ne 0 ]; then
#     echo "Failed to assign role device_service_role to user"
#     exit 1
# fi

echo "Dynamic security configuration completed successfully"
