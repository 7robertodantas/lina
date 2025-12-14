package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	mqttpb "github.com/robertodantas/lnpay/proto/gen/model/mqtt"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// CreateDeviceRequest represents the request body for creating a device
type CreateDeviceRequest struct {
	DeviceID             string `json:"device_id" binding:"required"`
	DeviceSecret         string `json:"device_secret" binding:"required"`
	MeasurementUnit      string `json:"measurement_unit" binding:"required"`
	UnitPriceMsat        int64  `json:"unit_price_msat" binding:"required"`
	ReportingStrategy    string `json:"reporting_strategy" binding:"required"`
	ReportingInterval    int    `json:"reporting_interval" binding:"required"`
	HeartbeatInterval    int    `json:"heartbeat_interval" binding:"required"`
	AuthorizeRequestMsat int    `json:"authorize_request_msat" binding:"required"`
	Timestamp            string `json:"timestamp" binding:"required"`
}

// CreateDevicesBatchRequest represents the request body for creating devices in batch
type CreateDevicesBatchRequest struct {
	DeviceIDPattern      string `json:"device_id_pattern" binding:"required"`     // e.g., "smart_meter_{id}"
	DeviceSecretPattern  string `json:"device_secret_pattern" binding:"required"` // e.g., "smart_meter_{id}_password"
	IDStart              *int   `json:"id_start" binding:"required"`              // inclusive start of ID range (pointer to allow 0)
	IDEnd                *int   `json:"id_end" binding:"required"`                // inclusive end of ID range (pointer to allow 0)
	IDPadding            int    `json:"id_padding" binding:"required,min=1"`      // number of digits to pad (e.g., 6 for "000001")
	MeasurementUnit      string `json:"measurement_unit" binding:"required"`
	UnitPriceMsat        int64  `json:"unit_price_msat" binding:"required"`
	ReportingStrategy    string `json:"reporting_strategy" binding:"required"`
	ReportingInterval    int    `json:"reporting_interval" binding:"required,min=1"`
	HeartbeatInterval    int    `json:"heartbeat_interval" binding:"required,min=1"`
	AuthorizeRequestMsat int    `json:"authorize_request_msat" binding:"required"`
	Timestamp            string `json:"timestamp" binding:"required"`
}

// NorthboundInterface handles REST API endpoints
type NorthboundInterface struct {
	router     *gin.Engine
	repo       *DeviceRepository
	dynSec     *MQTTDynSecService
	mqttClient *MQTTClient
	server     *http.Server
}

// NewNorthboundInterface creates a new northbound interface
func NewNorthboundInterface(repo *DeviceRepository, dynSec *MQTTDynSecService, mqttClient *MQTTClient) *NorthboundInterface {
	router := gin.Default()

	// Add OpenTelemetry middleware for automatic route-based span naming
	// This will create spans named like "GET /api/v1/devices" or "POST /api/v1/devices/:id"
	router.Use(otelgin.Middleware("device-service"))

	nb := &NorthboundInterface{
		router:     router,
		repo:       repo,
		dynSec:     dynSec,
		mqttClient: mqttClient,
	}

	// Register routes
	nb.registerRoutes()

	return nb
}

// registerRoutes registers all REST API routes
func (nb *NorthboundInterface) registerRoutes() {
	api := nb.router.Group("/api/v1")
	{
		api.POST("/devices", nb.createDevice)
		api.POST("/devices/batch", nb.createDevicesBatch)
		api.GET("/devices", nb.listDevices)
		api.GET("/devices/:id", nb.getDevice)
	}
}

// createDevice handles POST /devices
func (nb *NorthboundInterface) createDevice(c *gin.Context) {
	var req CreateDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate reporting_strategy
	validStrategies := map[string]bool{
		"interval": true,
		"delta":    true,
		"total":    true,
	}
	if !validStrategies[req.ReportingStrategy] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "reporting_strategy must be one of: interval, delta, total",
		})
		return
	}

	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339, req.Timestamp)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid timestamp format, expected RFC3339 (e.g., 2025-11-07T17:40:00Z)",
		})
		return
	}

	// Create device struct (note: we don't store device_secret)
	device := &Device{
		DeviceID:             req.DeviceID,
		MeasurementUnit:      req.MeasurementUnit,
		UnitPriceMsat:        req.UnitPriceMsat,
		ReportingStrategy:    req.ReportingStrategy,
		ReportingInterval:    req.ReportingInterval,
		HeartbeatInterval:    req.HeartbeatInterval,
		AuthorizeRequestMsat: req.AuthorizeRequestMsat,
		Timestamp:            timestamp,
	}

	// Check if device already exists
	_, err = nb.repo.GetDevice(c, device.DeviceID)
	deviceExists := err == nil

	ctx := c.Request.Context()
	if deviceExists {
		// Update existing device in database
		logger.WithDeviceID(device.DeviceID).
			Info(ctx, "Device already exists, updating via northbound REST")
		if err := nb.repo.UpdateDevice(ctx, device); err != nil {
			logger.WithDeviceID(device.DeviceID).
				Error(ctx, "Failed to update device in database via northbound REST", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to update device",
			})
			return
		}
		logger.WithDeviceID(device.DeviceID).
			Info(ctx, "Device updated in database via northbound REST")
	} else {
		// Create new device in database
		if err := nb.repo.CreateDevice(ctx, device); err != nil {
			logger.WithDeviceID(device.DeviceID).
				Error(ctx, "Failed to create device in database via northbound REST", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to create device",
			})
			return
		}
		logger.WithDeviceID(device.DeviceID).
			Info(ctx, "Device created in database via northbound REST")
	}

	// Trigger dynsec provisioning (using device_secret as password)
	logger.WithDeviceID(device.DeviceID).
		Info(ctx, "Provisioning device in dynsec via northbound REST")
	if err := nb.dynSec.ProvisionDevice(ctx, device.DeviceID, req.DeviceSecret); err != nil {
		logger.WithDeviceID(device.DeviceID).
			Warnf(ctx, "Failed to provision device in dynsec via northbound REST: %v", err)
		// Continue even if provisioning fails - device is already in database
	} else {
		logger.WithDeviceID(device.DeviceID).
			Info(ctx, "Device provisioned successfully in dynsec via northbound REST")
	}

	// Publish device configuration to /devices/{device_id}/config
	if err := nb.publishDeviceConfig(ctx, device); err != nil {
		logger.WithDeviceID(device.DeviceID).
			Warnf(ctx, "Failed to publish device config on southbound mqtt via northbound REST: %v", err)
		// Continue even if publishing fails - device is already in database and provisioned
	} else {
		logger.WithDeviceID(device.DeviceID).
			InfoWithFields(ctx, "Device config published on southbound mqtt via northbound REST", map[string]interface{}{
				"topic": fmt.Sprintf("/devices/%s/config", device.DeviceID),
			})
	}

	// Return 200 OK for updates, 201 Created for new devices
	if deviceExists {
		c.JSON(http.StatusOK, device)
	} else {
		c.JSON(http.StatusCreated, device)
	}
}

// listDevices handles GET /devices
func (nb *NorthboundInterface) listDevices(c *gin.Context) {
	ctx := c.Request.Context()
	devices, err := nb.repo.ListDevices(ctx)
	if err != nil {
		logger.Error(ctx, "Failed to list devices via northbound REST", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to list devices",
		})
		return
	}

	c.JSON(http.StatusOK, devices)
}

// getDevice handles GET /devices/:id
func (nb *NorthboundInterface) getDevice(c *gin.Context) {
	ctx := c.Request.Context()
	deviceID := c.Param("id")
	device, err := nb.repo.GetDevice(ctx, deviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "device not found",
		})
		return
	}

	c.JSON(http.StatusOK, device)
}

// Start starts the HTTP server
func (nb *NorthboundInterface) Start(ctx context.Context, addr string) error {
	// OpenTelemetry middleware is already added in NewNorthboundInterface
	// It will automatically name spans as "{HTTP_METHOD} {route_template}"
	// e.g., "GET /api/v1/devices", "POST /api/v1/devices", "GET /api/v1/devices/:id"
	nb.server = &http.Server{
		Addr:    addr,
		Handler: nb.router,
	}

	logger.Infof(ctx, "Starting northbound REST API server on %s", addr)
	return nb.server.ListenAndServe()
}

// publishDeviceConfig publishes the device configuration to MQTT
func (nb *NorthboundInterface) publishDeviceConfig(ctx context.Context, device *Device) error {
	// Map reporting_strategy string to enum
	var reportingStrategy mqttpb.ReportingStrategy
	switch device.ReportingStrategy {
	case "interval":
		reportingStrategy = mqttpb.ReportingStrategy_REPORTING_STRATEGY_INTERVAL
	case "delta":
		reportingStrategy = mqttpb.ReportingStrategy_REPORTING_STRATEGY_DELTA
	case "total":
		reportingStrategy = mqttpb.ReportingStrategy_REPORTING_STRATEGY_TOTAL
	default:
		return fmt.Errorf("invalid reporting strategy: %s", device.ReportingStrategy)
	}

	// Create config payload
	config := &mqttpb.ConfigPayload{
		DeviceId:             device.DeviceID,
		MeasurementUnit:      device.MeasurementUnit,
		UnitPriceMsat:        device.UnitPriceMsat,
		ReportingStrategy:    reportingStrategy,
		ReportingInterval:    int32(device.ReportingInterval),
		HeartbeatInterval:    int32(device.HeartbeatInterval),
		AuthorizeRequestMsat: int64(device.AuthorizeRequestMsat),
		Timestamp:            device.Timestamp.Format(time.RFC3339),
	}

	// Serialize to JSON
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config payload: %w", err)
	}

	// Publish to /devices/{device_id}/config (retained message)
	configTopic := fmt.Sprintf("/devices/%s/config", device.DeviceID)
	if err := nb.mqttClient.Publish(ctx, configTopic, 1, true, configJSON); err != nil {
		return fmt.Errorf("failed to publish config: %w", err)
	}

	return nil
}

// createDevicesBatch handles POST /devices/batch
func (nb *NorthboundInterface) createDevicesBatch(c *gin.Context) {
	var req CreateDevicesBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate reporting_strategy
	validStrategies := map[string]bool{
		"interval": true,
		"delta":    true,
		"total":    true,
	}
	if !validStrategies[req.ReportingStrategy] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "reporting_strategy must be one of: interval, delta, total",
		})
		return
	}

	// Validate ID range (dereference pointers)
	if req.IDStart == nil || req.IDEnd == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "id_start and id_end are required",
		})
		return
	}

	idStart := *req.IDStart
	idEnd := *req.IDEnd

	if idStart < 0 || idEnd < idStart {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "id_start must be >= 0 and id_end must be >= id_start",
		})
		return
	}

	// Limit batch size to prevent memory issues (100,001 to allow 0-100000 range)
	maxBatchSize := 100001
	totalDevices := idEnd - idStart + 1
	if totalDevices > maxBatchSize {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("batch size too large: %d devices (max: %d)", totalDevices, maxBatchSize),
		})
		return
	}

	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339, req.Timestamp)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid timestamp format, expected RFC3339 (e.g., 2025-11-07T17:40:00Z)",
		})
		return
	}

	ctx := c.Request.Context()

	// Check if batch already exists
	batchExists, err := nb.repo.BatchExists(ctx, idStart, idEnd, req.IDPadding, req.DeviceIDPattern)
	if err != nil {
		logger.Errorf(ctx, "Failed to check batch existence: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to check batch existence: %v", err),
		})
		return
	}

	if batchExists {
		logger.Infof(ctx, "Batch already exists for pattern %s, idStart=%d, idEnd=%d, returning 204", req.DeviceIDPattern, idStart, idEnd)
		c.Status(http.StatusNoContent)
		return
	}

	// Generate all devices
	devices := make([]*Device, 0, totalDevices)
	deviceSecrets := make(map[string]string, totalDevices)

	logger.Infof(ctx, "Generating %d devices for batch creation", totalDevices)
	for id := idStart; id <= idEnd; id++ {
		// Format ID with padding
		idStr := fmt.Sprintf("%0*d", req.IDPadding, id)

		// Generate device ID and secret from patterns (replace {id} placeholder)
		deviceID := strings.ReplaceAll(req.DeviceIDPattern, "{id}", idStr)
		deviceSecret := strings.ReplaceAll(req.DeviceSecretPattern, "{id}", idStr)

		device := &Device{
			DeviceID:             deviceID,
			MeasurementUnit:      req.MeasurementUnit,
			UnitPriceMsat:        req.UnitPriceMsat,
			ReportingStrategy:    req.ReportingStrategy,
			ReportingInterval:    req.ReportingInterval,
			HeartbeatInterval:    req.HeartbeatInterval,
			AuthorizeRequestMsat: req.AuthorizeRequestMsat,
			Timestamp:            timestamp,
		}
		devices = append(devices, device)
		deviceSecrets[deviceID] = deviceSecret
	}

	// Insert devices in database in batches (SQLite works better with smaller chunks)
	logger.Infof(ctx, "Inserting %d devices into database in batches", totalDevices)
	batchSize := 1000 // SQLite batch size
	for i := 0; i < len(devices); i += batchSize {
		end := i + batchSize
		if end > len(devices) {
			end = len(devices)
		}
		batch := devices[i:end]
		if err := nb.repo.CreateDevicesBatch(ctx, batch); err != nil {
			logger.Errorf(ctx, "Failed to create device batch %d-%d: %v", i, end-1, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("failed to create devices batch %d-%d: %v", i, end-1, err),
			})
			return
		}
		logger.Infof(ctx, "Created device batch %d-%d (%d devices)", i, end-1, len(batch))
	}

	// Use shared devices_any_role for all batch-provisioned devices
	// groupName and roleName are kept for backward compatibility but no longer used
	groupName := ""
	roleName := ""

	// Provision devices in dynsec in batches (uses shared devices_any_role internally)
	logger.Infof(ctx, "Provisioning %d devices in dynsec with shared devices_any_role", totalDevices)
	if err := nb.dynSec.ProvisionDevicesBatch(ctx, devices, deviceSecrets, groupName, roleName, req.DeviceIDPattern); err != nil {
		logger.Errorf(ctx, "Failed to provision devices in dynsec: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("failed to provision devices in dynsec: %v", err),
		})
		return
	}
	logger.Infof(ctx, "Successfully provisioned %d devices in dynsec", totalDevices)

	// Publish device configurations in batches (only if dynsec provisioning succeeded)
	logger.Infof(ctx, "Publishing configs for %d devices", totalDevices)
	for i := 0; i < len(devices); i += batchSize {
		end := i + batchSize
		if end > len(devices) {
			end = len(devices)
		}
		batch := devices[i:end]
		for _, device := range batch {
			if err := nb.publishDeviceConfig(ctx, device); err != nil {
				logger.Warnf(ctx, "Failed to publish config for device %s: %v", device.DeviceID, err)
				// Continue even if publishing fails - devices are already in database and dynsec
			}
		}
		logger.Infof(ctx, "Published configs for device batch %d-%d", i, end-1)
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":         "batch creation initiated",
		"devices_created": totalDevices,
		"id_range":        fmt.Sprintf("%d-%d", idStart, idEnd),
	})
}

// Stop gracefully stops the HTTP server
func (nb *NorthboundInterface) Stop(ctx context.Context) error {
	if nb.server != nil {
		logger.Info(ctx, "Stopping northbound REST API server")
		return nb.server.Shutdown(ctx)
	}
	return nil
}
