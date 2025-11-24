package main

import (
	"encoding/json"
	"fmt"
	"log"
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
func NewDynSecService() (*DynSecService, error) {
	broker := getEnv("MQTT_BROKER", "mosquitto")
	useTLS := getEnvBool("MQTT_USE_TLS", true)

	var port int
	var protocol string
	if useTLS {
		port = getEnvInt("MQTT_TLS_PORT", 8883)
		protocol = getEnv("MQTT_TLS_PROTOCOL", "tls")
	} else {
		port = getEnvInt("MQTT_PORT", 1883)
		protocol = "tcp"
	}

	adminUser := getEnv("MQTT_DYNSEC_ADMIN_USER", "admin")
	adminPass := getEnv("MQTT_DYNSEC_ADMIN_PASSWORD", "admin")
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

	log.Printf("Connecting to MQTT broker for dynamic security: %s://%s:%d", protocol, broker, port)

	client, err := ConnectMQTT(opts)
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

	log.Printf("Connected to MQTT broker and subscribed to %s", responseTopic)

	return service, nil
}

// handleResponse handles responses from the dynamic security plugin
func (d *DynSecService) handleResponse(client mqtt.Client, msg mqtt.Message) {
	var response map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &response); err != nil {
		log.Printf("Failed to parse response: %v", err)
		return
	}
	select {
	case d.responseCh <- response:
	default:
		log.Printf("Response channel full, dropping response")
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
				log.Printf("Draining old responses before command %d", commandID)
			}
		default:
			// No more old responses
			if drained > 0 {
				log.Printf("Drained %d old response(s) before command %d", drained, commandID)
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
	log.Printf("Publishing command %d to %s: %s", commandID, controlTopic, string(commandJSON))

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
		log.Printf("Received response for command %d: %s", commandID, string(responseJSON))

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
							log.Printf("Command %d, response %d: %s (skipping, already exists)", commandID, i, errStr)
						} else {
							errorJSON, _ := json.MarshalIndent(respMap, "", "  ")
							log.Printf("Command %d, response %d failed with error: %s", commandID, i, string(errorJSON))
							errors = append(errors, fmt.Sprintf("response %d: %v", i, errMsg))
						}
					}
				}
			}
			if len(errors) > 0 {
				return fmt.Errorf("command failed with %d error(s): %v", len(errors), errors)
			}
			if alreadyExistsCount > 0 {
				log.Printf("Command %d completed with %d 'already exists' warning(s) (%d total responses)", commandID, alreadyExistsCount, len(resp))
			} else {
				log.Printf("Command %d executed successfully (%d responses)", commandID, len(resp))
			}
		} else {
			log.Printf("Command %d executed successfully (no response array)", commandID)
		}
		return nil
	case <-timeout:
		return fmt.Errorf("timeout waiting for response to command %d", commandID)
	}
}

// ProvisionDevice provisions a new device with dynamic security
// It creates a role, client, and sets up ACL rules for the device
func (d *DynSecService) ProvisionDevice(deviceID string) error {
	log.Printf("Provisioning device: %s", deviceID)

	roleName := fmt.Sprintf("device_%s_role", deviceID)
	clientUsername := deviceID
	// Generate a simple password (in production, use a secure random password)
	clientPassword := fmt.Sprintf("%s_password", deviceID)

	// Step 1: Create role for the device
	log.Printf("Creating role: %s", roleName)
	createRoleCmd := map[string]interface{}{
		"commands": []map[string]interface{}{
			{
				"command":  "createRole",
				"rolename": roleName,
			},
		},
	}
	if err := d.executeCommand(createRoleCmd); err != nil {
		// Role might already exist, log but continue
		log.Printf("Note: Role creation returned error (may already exist): %v", err)
	}

	// Step 2: Add publish ACLs for the role (batch all publish ACLs in one command)
	publishTopics := []string{
		fmt.Sprintf("/devices/%s/heartbeat", deviceID),
		fmt.Sprintf("/devices/%s/usage", deviceID),
		fmt.Sprintf("/devices/%s/request/authorize", deviceID),
		fmt.Sprintf("/devices/%s/request/invoice", deviceID),
	}

	log.Printf("Adding %d publish ACLs for role: %s", len(publishTopics), roleName)
	publishACLCommands := make([]map[string]interface{}, 0, len(publishTopics))
	for _, topic := range publishTopics {
		log.Printf("  - Adding publish ACL for topic: %s", topic)
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
		log.Printf("ERROR: Failed to add publish ACLs: %v", err)
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
	}

	log.Printf("Adding %d subscribe ACLs for role: %s", len(subscribeTopics), roleName)
	subscribeACLCommands := make([]map[string]interface{}, 0, len(subscribeTopics))
	for _, topic := range subscribeTopics {
		log.Printf("  - Adding subscribe ACL for topic: %s", topic)
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
		log.Printf("ERROR: Failed to add subscribe ACLs: %v", err)
		return fmt.Errorf("failed to add subscribe ACLs: %w", err)
	}

	// Step 4: Create client with deviceID as username
	log.Printf("Creating client: %s", clientUsername)
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
		// Client might already exist, log but continue
		log.Printf("Note: Client creation returned error (may already exist): %v", err)
	}

	// Step 5: Assign role to client
	log.Printf("Assigning role %s to client %s", roleName, clientUsername)
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
		return fmt.Errorf("failed to assign role to client: %w", err)
	}

	log.Printf("Successfully provisioned device: %s", deviceID)
	log.Printf("Client credentials - Username: %s, Password: %s", clientUsername, clientPassword)

	return nil
}

// GetDeviceCredentials returns the credentials for a provisioned device
func (d *DynSecService) GetDeviceCredentials(deviceID string) (username, password string) {
	username = deviceID
	password = fmt.Sprintf("%s_password", deviceID)
	return username, password
}

// Disconnect disconnects from the MQTT broker
func (d *DynSecService) Disconnect() {
	if d.client != nil {
		d.client.Disconnect(250)
		log.Println("Disconnected from MQTT broker (dynamic security service)")
	}
}
