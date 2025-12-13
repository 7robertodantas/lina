package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gin-gonic/gin"
)

var mqttBroker = getEnv("MQTT_BROKER", "ssl://localhost:8883")

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Store active connections
type DeviceSession struct {
	Client    mqtt.Client
	DeviceCtx *DeviceContext
}

var (
	sessions = make(map[string]*DeviceSession)
	sessMux  sync.RWMutex
)

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Use a single wildcard route and dispatch based on the action
	r.POST("/devices/:deviceId/*action", handleDeviceRoute)
	r.POST("/devices/batch/connect", handleBatchConnect)

	listenAddr := getEnv("LISTEN_ADDR", ":8080")
	fmt.Printf("HTTP Device service running on %s (broker: %s)\n", listenAddr, mqttBroker)
	log.Fatal(r.Run(listenAddr))
}

// handleDeviceRoute dispatches to the appropriate handler based on the action
func handleDeviceRoute(c *gin.Context) {
	action := c.Param("action")
	// Remove leading slash if present
	if len(action) > 0 && action[0] == '/' {
		action = action[1:]
	}

	switch action {
	case "connect":
		handleConnect(c)
	case "disconnect":
		handleDisconnect(c)
	default:
		handleDevicePublish(c)
	}
}

func handleConnect(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(400, gin.H{"error": "deviceId is required"})
		return
	}

	var req struct {
		Secret string `json:"secret" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Check if device already has an active session
	sessMux.RLock()
	existingSession, exists := sessions[deviceID]
	sessMux.RUnlock()

	if exists && existingSession.Client.IsConnected() {
		// Device is already connected, skip reconnection
		log.Printf("[%s] Device already connected, skipping reconnection", deviceID)
		c.Status(200)
		return
	}

	// If session exists but client is not connected, clean it up
	if exists {
		log.Printf("[%s] Existing session found but not connected, cleaning up", deviceID)
		sessMux.Lock()
		if existingSession.Client.IsConnected() {
			existingSession.Client.Disconnect(250)
		}
		delete(sessions, deviceID)
		sessMux.Unlock()
		// Small delay to ensure the old connection is fully closed
		time.Sleep(100 * time.Millisecond)
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(mqttBroker)
	opts.SetClientID(deviceID)
	opts.SetUsername(deviceID)
	opts.SetPassword(req.Secret)
	opts.SetCleanSession(true) // Ensure clean session to avoid conflicts

	// Configure TLS with certificate verification disabled
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	opts.SetTLSConfig(tlsConfig)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		c.JSON(500, gin.H{"error": token.Error().Error()})
		return
	}

	// Create device context (broker URL is set in NewDeviceContext from mqttBroker)
	deviceCtx := NewDeviceContext(deviceID, req.Secret, client)

	// Subscribe to topics (this sets up message handlers)
	if err := deviceCtx.SubscribeToTopics(); err != nil {
		client.Disconnect(250)
		c.JSON(500, gin.H{"error": fmt.Sprintf("failed to subscribe: %v", err)})
		return
	}

	// Initialize device (request invoice, wait, request authorization, wait)
	if err := deviceCtx.Initialize(); err != nil {
		client.Disconnect(250)
		c.JSON(500, gin.H{"error": fmt.Sprintf("initialization failed: %v", err)})
		return
	}

	// Store session
	sessMux.Lock()
	sessions[deviceID] = &DeviceSession{
		Client:    client,
		DeviceCtx: deviceCtx,
	}
	sessMux.Unlock()

	// Start background goroutine to maintain authorization
	go maintainAuthorization(deviceCtx)

	c.Status(200)
}

func handleBatchConnect(c *gin.Context) {
	var req struct {
		Devices []struct {
			DeviceID string `json:"deviceId" binding:"required"`
			Secret   string `json:"secret" binding:"required"`
		} `json:"devices" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if len(req.Devices) == 0 {
		c.JSON(400, gin.H{"error": "at least one device is required"})
		return
	}

	type deviceResult struct {
		DeviceID string `json:"deviceId"`
		Success  bool   `json:"success"`
		Error    string `json:"error,omitempty"`
	}

	results := make([]deviceResult, len(req.Devices))
	var wg sync.WaitGroup

	// Connect all devices in parallel
	for i, device := range req.Devices {
		wg.Add(1)
		go func(idx int, devID, secret string) {
			defer wg.Done()

			// Check if device already has an active session
			sessMux.RLock()
			existingSession, exists := sessions[devID]
			sessMux.RUnlock()

			if exists && existingSession.Client.IsConnected() {
				// Device is already connected, skip reconnection
				log.Printf("[%s] Device already connected, skipping reconnection", devID)
				results[idx] = deviceResult{
					DeviceID: devID,
					Success:  true,
				}
				return
			}

			// If session exists but client is not connected, clean it up
			if exists {
				log.Printf("[%s] Existing session found but not connected, cleaning up", devID)
				sessMux.Lock()
				// Disconnect if somehow still connected (shouldn't happen, but be safe)
				if existingSession.Client.IsConnected() {
					existingSession.Client.Disconnect(250)
				}
				delete(sessions, devID)
				sessMux.Unlock()
				// Small delay to ensure the old connection is fully closed
				time.Sleep(100 * time.Millisecond)
			}

			opts := mqtt.NewClientOptions()
			opts.AddBroker(mqttBroker)
			opts.SetClientID(devID)
			opts.SetUsername(devID)
			opts.SetPassword(secret)
			opts.SetCleanSession(true) // Ensure clean session to avoid conflicts

			// Configure TLS with certificate verification disabled
			tlsConfig := &tls.Config{
				InsecureSkipVerify: true,
			}
			opts.SetTLSConfig(tlsConfig)

			client := mqtt.NewClient(opts)
			if token := client.Connect(); token.Wait() && token.Error() != nil {
				results[idx] = deviceResult{
					DeviceID: devID,
					Success:  false,
					Error:    token.Error().Error(),
				}
				return
			}

			// Create device context (broker URL is set in NewDeviceContext from mqttBroker)
			deviceCtx := NewDeviceContext(devID, secret, client)

			// Subscribe to topics
			if err := deviceCtx.SubscribeToTopics(); err != nil {
				client.Disconnect(250)
				results[idx] = deviceResult{
					DeviceID: devID,
					Success:  false,
					Error:    fmt.Sprintf("failed to subscribe: %v", err),
				}
				return
			}

			// Initialize device (request invoice, wait, request authorization, wait)
			if err := deviceCtx.Initialize(); err != nil {
				client.Disconnect(250)
				results[idx] = deviceResult{
					DeviceID: devID,
					Success:  false,
					Error:    fmt.Sprintf("initialization failed: %v", err),
				}
				return
			}

			// Store session
			sessMux.Lock()
			sessions[devID] = &DeviceSession{
				Client:    client,
				DeviceCtx: deviceCtx,
			}
			sessMux.Unlock()

			// Start background goroutine to maintain authorization
			go maintainAuthorization(deviceCtx)

			results[idx] = deviceResult{
				DeviceID: devID,
				Success:  true,
			}
		}(i, device.DeviceID, device.Secret)
	}

	// Wait for all connections to complete
	wg.Wait()

	// Count successes and failures
	successCount := 0
	for _, result := range results {
		if result.Success {
			successCount++
		}
	}

	c.JSON(200, gin.H{
		"connected": successCount,
		"failed":    len(req.Devices) - successCount,
		"total":     len(req.Devices),
		"results":   results,
	})
}

func handleDisconnect(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(400, gin.H{"error": "deviceId is required"})
		return
	}

	sessMux.Lock()
	session, exists := sessions[deviceID]
	if exists {
		delete(sessions, deviceID)
	}
	sessMux.Unlock()

	if !exists {
		c.JSON(404, gin.H{"error": "Device not connected"})
		return
	}

	// Disconnect MQTT client
	session.Client.Disconnect(250)
	log.Printf("[%s] Device disconnected", deviceID)

	c.Status(200)
}

// maintainAuthorization periodically ensures authorization is active
func maintainAuthorization(ctx *DeviceContext) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx.EnsureAuthorizationActive()
		}
	}
}

func handleDevicePublish(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		c.JSON(400, gin.H{"error": "deviceId is required"})
		return
	}

	// Use the request path directly as the MQTT topic
	topic := c.Request.URL.Path

	// Read request body as payload
	payload, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	sessMux.RLock()
	session, exists := sessions[deviceID]
	sessMux.RUnlock()

	if !exists {
		c.JSON(404, gin.H{"error": "Device not connected"})
		return
	}

	// Check if reporting is enabled (for usage reports)
	if session.DeviceCtx != nil {
		// For usage reports, check if reporting is enabled
		if strings.Contains(topic, "/usage") {
			session.DeviceCtx.mu.RLock()
			reportingEnabled := session.DeviceCtx.ReportingEnabled
			session.DeviceCtx.mu.RUnlock()

			if !reportingEnabled {
				c.JSON(423, gin.H{"error": "reporting disabled (STOP/PAUSE command received)"})
				return
			}
		}
		// Ensure authorization is active before publishing usage reports
		session.DeviceCtx.EnsureAuthorizationActive()
	}

	// Check if client is connected before publishing
	if !session.Client.IsConnected() {
		log.Printf("[%s] Client not connected, attempting to reconnect before publish...", deviceID)
		if err := session.DeviceCtx.reconnectClient(); err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to reconnect: %v", err)})
			return
		}
		// Update session with reconnected client
		sessMux.Lock()
		session, exists = sessions[deviceID]
		if exists {
			// Update the client reference in the session
			session.Client = session.DeviceCtx.Client
		}
		sessMux.Unlock()
		if !exists {
			c.JSON(404, gin.H{"error": "Device session lost after reconnect"})
			return
		}
	}

	token := session.Client.Publish(topic, 1, false, string(payload))
	token.Wait()

	if token.Error() != nil {
		err := token.Error()
		errStr := err.Error()
		// Check if error is "not Connected"
		if strings.Contains(errStr, "not Connected") || strings.Contains(errStr, "not connected") {
			log.Printf("[%s] Got 'not Connected' error on publish, attempting to reconnect...", deviceID)
			if reconnectErr := session.DeviceCtx.reconnectClient(); reconnectErr != nil {
				c.JSON(500, gin.H{"error": fmt.Sprintf("failed to reconnect after publish error: %v", reconnectErr)})
				return
			}
			// Update session with reconnected client and retry publish
			sessMux.Lock()
			session, exists = sessions[deviceID]
			if exists {
				// Update the client reference in the session
				session.Client = session.DeviceCtx.Client
			}
			sessMux.Unlock()
			if !exists {
				c.JSON(404, gin.H{"error": "Device session lost after reconnect"})
				return
			}
			retryToken := session.Client.Publish(topic, 1, false, string(payload))
			retryToken.Wait()
			if retryToken.Error() != nil {
				c.JSON(500, gin.H{"error": fmt.Sprintf("failed to publish after reconnect: %v", retryToken.Error())})
				return
			}
			c.Status(200)
			return
		}
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.Status(200)
}
