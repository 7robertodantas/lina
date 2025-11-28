package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// DynSecService handles dynamic security plugin operations via MQTT topic API
type DynSecService struct {
	client      mqtt.Client
	responseCh  chan map[string]interface{}
	commandID   int
	commandIDMu sync.Mutex
}

// NewDynSecService creates a new dynamic security service using MQTT topic API
func NewDynSecService(cfg Config) (*DynSecService, error) {
	broker := cfg.MQTTBroker
	useTLS := cfg.MQTTUseTLS

	var port int
	var protocol string
	if useTLS {
		port = cfg.MQTTTLSPort
		protocol = cfg.MQTTTLSProtocol
	} else {
		port = cfg.MQTTPort
		protocol = "tcp"
	}

	adminUser := cfg.MQTTDynSecAdminUser
	adminPass := cfg.MQTTDynSecAdminPassword
	clientID := fmt.Sprintf("dynsec-admin-%d", time.Now().Unix())

	opts := &MQTTConnectionOptions{
		ClientID:  clientID,
		Username:  adminUser,
		Password:  adminPass,
		UseTLS:    useTLS,
		Broker:    broker,
		Port:      port,
		Protocol:  protocol,
		Timeout:   30 * time.Second,
		KeepAlive: 60 * time.Second,
	}

	logger.InfoWithFields("Connecting to MQTT broker for dynamic security on southbound mqtt", map[string]interface{}{
		"protocol": protocol,
		"broker":   broker,
		"port":     port,
	})

	client, err := ConnectMQTT(opts, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MQTT broker: %w", err)
	}

	service := &DynSecService{
		client:     client,
		responseCh: make(chan map[string]interface{}, 100), // Increased buffer to handle multiple concurrent commands
		commandID:  1,
	}

	// Subscribe to response topic
	responseTopic := "$CONTROL/dynamic-security/v1/response"
	if token := client.Subscribe(responseTopic, 1, service.handleResponse); token.Wait() && token.Error() != nil {
		client.Disconnect(250)
		return nil, fmt.Errorf("failed to subscribe to response topic: %w", token.Error())
	}

	logger.InfoWithFields("Connected to MQTT broker and subscribed on southbound mqtt", map[string]interface{}{
		"response_topic": responseTopic,
	})

	return service, nil
}

// handleResponse handles responses from the dynamic security plugin
func (d *DynSecService) handleResponse(client mqtt.Client, msg mqtt.Message) {
	var response map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &response); err != nil {
		logger.Error("Failed to parse dynamic security response on southbound mqtt", err)
		return
	}
	select {
	case d.responseCh <- response:
	default:
		logger.Warn("Response channel full, dropping dynamic security response on southbound mqtt")
	}
}

// getNextCommandID returns the next command ID
func (d *DynSecService) getNextCommandID() int {
	d.commandIDMu.Lock()
	defer d.commandIDMu.Unlock()
	id := d.commandID
	d.commandID++
	return id
}

// isAlreadyExistsError checks if an error message indicates an "already exists" condition
// These are non-fatal errors that we can safely ignore
func isAlreadyExistsError(errMsg string) bool {
	errLower := strings.ToLower(errMsg)
	return strings.Contains(errLower, "already exists") ||
		strings.Contains(errLower, "role already exists") ||
		strings.Contains(errLower, "acl with this topic already exists") ||
		strings.Contains(errLower, "client already exists")
}

// listRoles lists all roles using the dynamic security API
func (d *DynSecService) listRoles() ([]string, error) {
	commandID := d.getNextCommandID()
	command := map[string]interface{}{
		"command": commandID,
		"commands": []map[string]interface{}{
			{
				"command": "listRoles",
			},
		},
	}

	// Drain old responses
	for {
		select {
		case <-d.responseCh:
		default:
			goto sendCommand
		}
	}
sendCommand:

	commandJSON, err := json.Marshal(command)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}

	controlTopic := "$CONTROL/dynamic-security/v1"
	logger.DebugWithFields("Listing roles on southbound mqtt", map[string]interface{}{
		"command_id": commandID,
	})

	token := d.client.Publish(controlTopic, 1, false, commandJSON)
	if !token.WaitTimeout(5 * time.Second) {
		return nil, fmt.Errorf("timeout publishing listRoles command")
	}
	if token.Error() != nil {
		return nil, fmt.Errorf("failed to publish listRoles command: %w", token.Error())
	}

	timeout := time.After(10 * time.Second)
	select {
	case response := <-d.responseCh:
		// Parse response format: response["responses"][0]["data"]["roles"]
		if resp, ok := response["responses"].([]interface{}); ok && len(resp) > 0 {
			if respMap, ok := resp[0].(map[string]interface{}); ok {
				// Check for error first
				if errMsg, hasErr := respMap["error"]; hasErr {
					return nil, fmt.Errorf("listRoles failed: %v", errMsg)
				}
				// Extract roles from data field
				if data, ok := respMap["data"].(map[string]interface{}); ok {
					if roles, ok := data["roles"].([]interface{}); ok {
						roleNames := make([]string, 0, len(roles))
						for _, role := range roles {
							// Roles can be strings or objects with rolename field
							if roleStr, ok := role.(string); ok {
								roleNames = append(roleNames, roleStr)
							} else if roleMap, ok := role.(map[string]interface{}); ok {
								if rolename, ok := roleMap["rolename"].(string); ok {
									roleNames = append(roleNames, rolename)
								}
							}
						}
						return roleNames, nil
					}
				}
			}
		}
		// Log the response for debugging
		responseJSON, _ := json.MarshalIndent(response, "", "  ")
		logger.WarnWithFields("Unexpected listRoles response format on southbound mqtt", map[string]interface{}{
			"response": string(responseJSON),
		})
		return nil, fmt.Errorf("unexpected response format from listRoles")
	case <-timeout:
		return nil, fmt.Errorf("timeout waiting for listRoles response")
	}
}

// listClients lists all clients using the dynamic security API
func (d *DynSecService) listClients() ([]string, error) {
	commandID := d.getNextCommandID()
	command := map[string]interface{}{
		"command": commandID,
		"commands": []map[string]interface{}{
			{
				"command": "listClients",
			},
		},
	}

	// Drain old responses
	for {
		select {
		case <-d.responseCh:
		default:
			goto sendCommand
		}
	}
sendCommand:

	commandJSON, err := json.Marshal(command)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}

	controlTopic := "$CONTROL/dynamic-security/v1"
	logger.InfoWithFields("Listing clients on southbound mqtt", map[string]interface{}{
		"command_id": commandID,
	})

	token := d.client.Publish(controlTopic, 1, false, commandJSON)
	if !token.WaitTimeout(5 * time.Second) {
		return nil, fmt.Errorf("timeout publishing listClients command")
	}
	if token.Error() != nil {
		return nil, fmt.Errorf("failed to publish listClients command: %w", token.Error())
	}

	timeout := time.After(10 * time.Second)
	select {
	case response := <-d.responseCh:
		// Parse response format: response["responses"][0]["data"]["clients"]
		if resp, ok := response["responses"].([]interface{}); ok && len(resp) > 0 {
			if respMap, ok := resp[0].(map[string]interface{}); ok {
				// Check for error first
				if errMsg, hasErr := respMap["error"]; hasErr {
					return nil, fmt.Errorf("listClients failed: %v", errMsg)
				}
				// Extract clients from data field
				if data, ok := respMap["data"].(map[string]interface{}); ok {
					if clients, ok := data["clients"].([]interface{}); ok {
						clientNames := make([]string, 0, len(clients))
						for _, client := range clients {
							// Clients are returned as strings (usernames)
							if clientStr, ok := client.(string); ok {
								clientNames = append(clientNames, clientStr)
							} else if clientMap, ok := client.(map[string]interface{}); ok {
								// Handle object format if it exists
								if username, ok := clientMap["username"].(string); ok {
									clientNames = append(clientNames, username)
								}
							}
						}
						return clientNames, nil
					}
				}
			}
		}
		// Log the response for debugging
		responseJSON, _ := json.MarshalIndent(response, "", "  ")
		logger.WarnWithFields("Unexpected listClients response format on southbound mqtt", map[string]interface{}{
			"response": string(responseJSON),
		})
		return nil, fmt.Errorf("unexpected response format from listClients")
	case <-timeout:
		return nil, fmt.Errorf("timeout waiting for listClients response")
	}
}

// roleExists checks if a role exists
func (d *DynSecService) roleExists(roleName string) (bool, error) {
	roles, err := d.listRoles()
	if err != nil {
		return false, err
	}
	for _, role := range roles {
		if role == roleName {
			return true, nil
		}
	}
	return false, nil
}

// clientExists checks if a client exists
func (d *DynSecService) clientExists(username string) (bool, error) {
	clients, err := d.listClients()
	if err != nil {
		return false, err
	}
	for _, client := range clients {
		if client == username {
			return true, nil
		}
	}
	return false, nil
}

// executeCommand sends a command to the dynamic security plugin and waits for response
func (d *DynSecService) executeCommand(command map[string]interface{}) error {
	commandID := d.getNextCommandID()
	command["command"] = commandID

	// Drain any old responses from the channel to ensure we get the response for this command
	// This prevents matching responses from previous commands
	drained := 0
	for {
		select {
		case <-d.responseCh:
			drained++
			if drained == 1 {
				logger.Debugf("Draining old responses before command %d on southbound mqtt", commandID)
			}
		default:
			// No more old responses
			if drained > 0 {
				logger.DebugWithFields("Drained old responses on southbound mqtt", map[string]interface{}{
					"drained":    drained,
					"command_id": commandID,
				})
			}
			goto sendCommand
		}
	}
sendCommand:

	commandJSON, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	controlTopic := "$CONTROL/dynamic-security/v1"
	logger.DebugWithFields("Publishing dynamic security command on southbound mqtt", map[string]interface{}{
		"command_id": commandID,
		"topic":      controlTopic,
		"command":    string(commandJSON),
	})

	token := d.client.Publish(controlTopic, 1, false, commandJSON)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("timeout publishing command")
	}
	if token.Error() != nil {
		return fmt.Errorf("failed to publish command: %w", token.Error())
	}

	// Wait for response with timeout
	// Note: Mosquitto Dynamic Security API doesn't echo back the command ID in responses,
	// so we accept the first response that arrives after sending the command.
	// This works because we process commands sequentially and we've drained old responses.
	timeout := time.After(10 * time.Second)

	select {
	case response := <-d.responseCh:
		// Log the full response for debugging
		responseJSON, _ := json.MarshalIndent(response, "", "  ")
		logger.DebugWithFields("Received dynamic security response on southbound mqtt", map[string]interface{}{
			"command_id": commandID,
			"response":   string(responseJSON),
		})

		// Check for errors in response (handle both single and batched commands)
		if resp, ok := response["responses"].([]interface{}); ok && len(resp) > 0 {
			// Check all responses in the batch for errors
			var errors []string
			var alreadyExistsCount int
			for i, r := range resp {
				if respMap, ok := r.(map[string]interface{}); ok {
					if errMsg, hasErr := respMap["error"]; hasErr {
						errStr := fmt.Sprintf("%v", errMsg)
						// Check if this is an "already exists" error (non-fatal)
						if isAlreadyExistsError(errStr) {
							alreadyExistsCount++
							logger.DebugWithFields("Command response (already exists) on southbound mqtt", map[string]interface{}{
								"command_id":     commandID,
								"response_index": i,
								"error":          errStr,
							})
						} else {
							errorJSON, _ := json.MarshalIndent(respMap, "", "  ")
							logger.ErrorWithFields("Command response failed on southbound mqtt", fmt.Errorf("%v", errMsg), map[string]interface{}{
								"command_id":     commandID,
								"response_index": i,
								"error":          string(errorJSON),
							})
							errors = append(errors, fmt.Sprintf("response %d: %v", i, errMsg))
						}
					}
				}
			}
			if len(errors) > 0 {
				return fmt.Errorf("command failed with %d error(s): %v", len(errors), errors)
			}
			if alreadyExistsCount > 0 {
				logger.InfoWithFields("Command completed with warnings on southbound mqtt", map[string]interface{}{
					"command_id":           commandID,
					"already_exists_count": alreadyExistsCount,
					"total_responses":      len(resp),
				})
			} else {
				logger.InfoWithFields("Command executed successfully on southbound mqtt", map[string]interface{}{
					"command_id":     commandID,
					"response_count": len(resp),
				})
			}
		} else {
			logger.InfoWithFields("Command executed successfully on southbound mqtt", map[string]interface{}{
				"command_id": commandID,
			})
		}
		return nil
	case <-timeout:
		return fmt.Errorf("timeout waiting for response to command %d", commandID)
	}
}

// ProvisionDevice provisions a new device with dynamic security
// It creates a role, client, and sets up ACL rules for the device
func (d *DynSecService) ProvisionDevice(deviceID, password string) error {
	logger.WithDeviceID(deviceID).
		Info("Provisioning device on southbound mqtt")

	roleName := fmt.Sprintf("device_%s_role", deviceID)
	clientUsername := deviceID
	clientPassword := password

	// Step 1: Check if role exists, create if missing
	logger.WithDeviceID(deviceID).
		DebugWithFields("Checking if role exists on southbound mqtt", map[string]interface{}{
			"role_name": roleName,
		})
	roleExists, err := d.roleExists(roleName)
	if err != nil {
		logger.WithDeviceID(deviceID).
			Warnf("Failed to check if role exists on southbound mqtt, will attempt to create: %v", err)
		roleExists = false // Assume it doesn't exist and try to create
	}

	if !roleExists {
		logger.WithDeviceID(deviceID).
			InfoWithFields("Creating role on southbound mqtt", map[string]interface{}{
				"role_name": roleName,
			})
		createRoleCmd := map[string]interface{}{
			"commands": []map[string]interface{}{
				{
					"command":  "createRole",
					"rolename": roleName,
				},
			},
		}
		if err := d.executeCommand(createRoleCmd); err != nil {
			return fmt.Errorf("failed to create role %s: %w", roleName, err)
		}
		logger.WithDeviceID(deviceID).
			InfoWithFields("Role created successfully on southbound mqtt", map[string]interface{}{
				"role_name": roleName,
			})
	} else {
		logger.WithDeviceID(deviceID).
			DebugWithFields("Role already exists, skipping creation on southbound mqtt", map[string]interface{}{
				"role_name": roleName,
			})
	}

	// Step 2: Add publish ACLs for the role (batch all publish ACLs in one command)
	publishTopics := []string{
		fmt.Sprintf("/devices/%s/heartbeat", deviceID),
		fmt.Sprintf("/devices/%s/usage", deviceID),
		fmt.Sprintf("/devices/%s/request/authorize", deviceID),
		fmt.Sprintf("/devices/%s/request/invoice", deviceID),
	}

	logger.WithDeviceID(deviceID).
		InfoWithFields("Adding publish ACLs for role on southbound mqtt", map[string]interface{}{
			"role_name": roleName,
			"count":     len(publishTopics),
		})
	publishACLCommands := make([]map[string]interface{}, 0, len(publishTopics))
	for _, topic := range publishTopics {
		logger.WithDeviceID(deviceID).
			DebugWithFields("Adding publish ACL for topic on southbound mqtt", map[string]interface{}{
				"topic": topic,
			})
		publishACLCommands = append(publishACLCommands, map[string]interface{}{
			"command":  "addRoleACL",
			"rolename": roleName,
			"acltype":  "publishClientSend",
			"topic":    topic,
			"allow":    true,
			"priority": 5,
		})
	}

	addPublishACLCmd := map[string]interface{}{
		"commands": publishACLCommands,
	}
	if err := d.executeCommand(addPublishACLCmd); err != nil {
		logger.WithDeviceID(deviceID).
			Error("Failed to add publish ACLs on southbound mqtt", err)
		return fmt.Errorf("failed to add publish ACLs: %w", err)
	}

	// Step 3: Add subscribe ACLs for the role (batch all subscribe ACLs in one command)
	subscribeTopics := []string{
		fmt.Sprintf("/devices/%s/config", deviceID),
		fmt.Sprintf("/devices/%s/control", deviceID),
		fmt.Sprintf("/devices/%s/balance", deviceID),
		fmt.Sprintf("/devices/%s/response/authorize", deviceID),
		fmt.Sprintf("/devices/%s/response/invoice", deviceID),
		fmt.Sprintf("/devices/%s/events/invoice", deviceID),
		// Allow wildcard subscription for topic discovery (e.g., MQTT Explorer)
		// This allows the device to see all topics under its own path only
		fmt.Sprintf("/devices/%s/#", deviceID),
	}

	logger.WithDeviceID(deviceID).
		InfoWithFields("Adding subscribe ACLs for role on southbound mqtt", map[string]interface{}{
			"role_name": roleName,
			"count":     len(subscribeTopics),
		})
	subscribeACLCommands := make([]map[string]interface{}, 0, len(subscribeTopics))
	for _, topic := range subscribeTopics {
		logger.WithDeviceID(deviceID).
			DebugWithFields("Adding subscribe ACL for topic on southbound mqtt", map[string]interface{}{
				"topic": topic,
			})
		subscribeACLCommands = append(subscribeACLCommands, map[string]interface{}{
			"command":  "addRoleACL",
			"rolename": roleName,
			"acltype":  "subscribePattern",
			"topic":    topic,
			"allow":    true,
			"priority": 5,
		})
	}

	addSubscribeACLCmd := map[string]interface{}{
		"commands": subscribeACLCommands,
	}
	if err := d.executeCommand(addSubscribeACLCmd); err != nil {
		logger.WithDeviceID(deviceID).
			Error("Failed to add subscribe ACLs on southbound mqtt", err)
		return fmt.Errorf("failed to add subscribe ACLs: %w", err)
	}

	// Step 4: Check if client exists, create if missing
	logger.WithDeviceID(deviceID).
		DebugWithFields("Checking if client exists on southbound mqtt", map[string]interface{}{
			"client_username": clientUsername,
		})
	clientExists, err := d.clientExists(clientUsername)
	if err != nil {
		logger.WithDeviceID(deviceID).
			Warnf("Failed to check if client exists on southbound mqtt, will attempt to create: %v", err)
		clientExists = false // Assume it doesn't exist and try to create
	}

	if !clientExists {
		logger.WithDeviceID(deviceID).
			InfoWithFields("Creating client on southbound mqtt", map[string]interface{}{
				"client_username": clientUsername,
			})
		createClientCmd := map[string]interface{}{
			"commands": []map[string]interface{}{
				{
					"command":  "createClient",
					"username": clientUsername,
					"password": clientPassword,
				},
			},
		}
		if err := d.executeCommand(createClientCmd); err != nil {
			return fmt.Errorf("failed to create client %s: %w", clientUsername, err)
		}
		logger.WithDeviceID(deviceID).
			InfoWithFields("Client created successfully on southbound mqtt", map[string]interface{}{
				"client_username": clientUsername,
			})
	} else {
		logger.WithDeviceID(deviceID).
			DebugWithFields("Client already exists, skipping creation on southbound mqtt", map[string]interface{}{
				"client_username": clientUsername,
			})
	}

	// Step 5: Assign role to client
	logger.WithDeviceID(deviceID).
		InfoWithFields("Assigning role to client on southbound mqtt", map[string]interface{}{
			"role_name":       roleName,
			"client_username": clientUsername,
		})
	addRoleCmd := map[string]interface{}{
		"commands": []map[string]interface{}{
			{
				"command":  "addClientRole",
				"username": clientUsername,
				"rolename": roleName,
				"priority": 5,
			},
		},
	}
	if err := d.executeCommand(addRoleCmd); err != nil {
		// "Internal error" from addClientRole usually means role or client doesn't exist
		errStr := err.Error()
		if strings.Contains(strings.ToLower(errStr), "internal error") {
			return fmt.Errorf("failed to assign role %s to client %s: %w (this usually means the role or client doesn't exist - verify steps 1 and 4 succeeded)", roleName, clientUsername, err)
		}
		return fmt.Errorf("failed to assign role %s to client %s: %w", roleName, clientUsername, err)
	}

	logger.WithDeviceID(deviceID).
		InfoWithFields("Successfully provisioned device on southbound mqtt", map[string]interface{}{
			"client_username": clientUsername,
		})

	return nil
}

// GetDeviceCredentials returns the credentials for a provisioned device
func (d *DynSecService) GetDeviceCredentials(deviceID string) (username, password string) {
	username = deviceID
	password = fmt.Sprintf("%s_password", deviceID)
	return username, password
}

// ProvisionDeviceService provisions the device service user with ACLs to subscribe to device topics
func (d *DynSecService) ProvisionDeviceService(username, password string) error {
	if username == "" {
		return fmt.Errorf("device service username is required")
	}

	roleName := "device_service_role"

	// Step 1: Check if role exists, create if missing
	logger.DebugWithFields("Checking if device service role exists on southbound mqtt", map[string]interface{}{
		"role_name": roleName,
	})
	roleExists, err := d.roleExists(roleName)
	if err != nil {
		logger.Warnf("Failed to check if role exists on southbound mqtt, will attempt to create: %v", err)
		roleExists = false
	}

	if !roleExists {
		logger.InfoWithFields("Creating device service role on southbound mqtt", map[string]interface{}{
			"role_name": roleName,
		})
		createRoleCmd := map[string]interface{}{
			"commands": []map[string]interface{}{
				{
					"command":  "createRole",
					"rolename": roleName,
				},
			},
		}
		if err := d.executeCommand(createRoleCmd); err != nil {
			return fmt.Errorf("failed to create device service role %s: %w", roleName, err)
		}
		logger.InfoWithFields("Device service role created successfully on southbound mqtt", map[string]interface{}{
			"role_name": roleName,
		})
	} else {
		logger.DebugWithFields("Device service role already exists, skipping creation on southbound mqtt", map[string]interface{}{
			"role_name": roleName,
		})
	}

	// Step 2: Add subscribe ACLs for device topics
	subscribeTopics := []string{
		"/devices/+/heartbeat",
		"/devices/+/usage",
		"/devices/+/request/authorize",
		"/devices/+/request/invoice",
	}

	logger.InfoWithFields("Adding subscribe ACLs for device service role on southbound mqtt", map[string]interface{}{
		"role_name": roleName,
		"count":     len(subscribeTopics),
	})
	subscribeACLCommands := make([]map[string]interface{}, 0, len(subscribeTopics))
	for _, topic := range subscribeTopics {
		logger.DebugWithFields("Adding subscribe ACL for topic on southbound mqtt", map[string]interface{}{
			"topic": topic,
		})
		subscribeACLCommands = append(subscribeACLCommands, map[string]interface{}{
			"command":  "addRoleACL",
			"rolename": roleName,
			"acltype":  "subscribePattern",
			"topic":    topic,
			"allow":    true,
			"priority": 5,
		})
	}

	addSubscribeACLCmd := map[string]interface{}{
		"commands": subscribeACLCommands,
	}
	if err := d.executeCommand(addSubscribeACLCmd); err != nil {
		logger.Error("Failed to add subscribe ACLs on southbound mqtt", err)
		// Continue even if some ACLs already exist
	}

	// Step 3: Add publish ACLs for device service responses
	publishTopics := []string{
		"/devices/+/response/authorize",
		"/devices/+/response/invoice",
		"/devices/+/config",
		"/devices/+/control",
		"/devices/+/balance",
	}

	logger.InfoWithFields("Adding publish ACLs for device service role on southbound mqtt", map[string]interface{}{
		"role_name": roleName,
		"count":     len(publishTopics),
	})
	publishACLCommands := make([]map[string]interface{}, 0, len(publishTopics))
	for _, topic := range publishTopics {
		logger.DebugWithFields("Adding publish ACL for topic on southbound mqtt", map[string]interface{}{
			"topic": topic,
		})
		publishACLCommands = append(publishACLCommands, map[string]interface{}{
			"command":  "addRoleACL",
			"rolename": roleName,
			"acltype":  "publishClientSend",
			"topic":    topic,
			"allow":    true,
			"priority": 5,
		})
	}

	addPublishACLCmd := map[string]interface{}{
		"commands": publishACLCommands,
	}
	if err := d.executeCommand(addPublishACLCmd); err != nil {
		logger.Error("Failed to add publish ACLs on southbound mqtt", err)
		// Continue even if some ACLs already exist
	}

	// Step 4: Check if device service client exists, create if missing
	logger.InfoWithFields("Checking if device service client exists on southbound mqtt", map[string]interface{}{
		"username": username,
	})
	clientExists, err := d.clientExists(username)
	if err != nil {
		logger.Warnf("Failed to check if client exists on southbound mqtt, will attempt to create: %v", err)
		clientExists = false
	}

	if !clientExists {
		logger.InfoWithFields("Creating device service client on southbound mqtt", map[string]interface{}{
			"username": username,
		})
		createClientCmd := map[string]interface{}{
			"commands": []map[string]interface{}{
				{
					"command":  "createClient",
					"username": username,
					"password": password,
				},
			},
		}
		if err := d.executeCommand(createClientCmd); err != nil {
			return fmt.Errorf("failed to create device service client %s: %w", username, err)
		}
		logger.InfoWithFields("Device service client created successfully on southbound mqtt", map[string]interface{}{
			"username": username,
		})
	} else {
		logger.InfoWithFields("Device service client already exists, updating password on southbound mqtt", map[string]interface{}{
			"username": username,
		})
		// Update password if client exists
		setPasswordCmd := map[string]interface{}{
			"commands": []map[string]interface{}{
				{
					"command":  "setClientPassword",
					"username": username,
					"password": password,
				},
			},
		}
		if err := d.executeCommand(setPasswordCmd); err != nil {
			logger.Warnf("Failed to update password for device service client on southbound mqtt: %v", err)
		}
	}

	// Step 5: Assign device_service_role to the device service client
	logger.InfoWithFields("Assigning device service role to client on southbound mqtt", map[string]interface{}{
		"role_name": roleName,
		"username":  username,
	})
	addRoleCmd := map[string]interface{}{
		"commands": []map[string]interface{}{
			{
				"command":  "addClientRole",
				"username": username,
				"rolename": roleName,
			},
		},
	}
	if err := d.executeCommand(addRoleCmd); err != nil {
		logger.Warnf("Failed to assign role to device service client on southbound mqtt: %v (role might already be assigned)", err)
		// Continue even if role is already assigned
	}

	logger.InfoWithFields("Successfully provisioned device service on southbound mqtt", map[string]interface{}{
		"username": username,
	})
	return nil
}

// Disconnect disconnects from the MQTT broker
func (d *DynSecService) Disconnect() {
	if d.client != nil {
		d.client.Disconnect(250)
		logger.Info("Disconnected from MQTT broker (dynamic security service) on southbound mqtt")
	}
}
