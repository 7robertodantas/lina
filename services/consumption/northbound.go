package main

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// NorthboundInterface handles REST API endpoints
type NorthboundInterface struct {
	router *gin.Engine
	repo   *ConsumptionRepository
	cfg    Config
	server *http.Server
}

// NewNorthboundInterface creates a new northbound interface
func NewNorthboundInterface(repo *ConsumptionRepository, cfg Config) *NorthboundInterface {
	router := gin.Default()

	router.Use(otelgin.Middleware("consumption-service"))

	nb := &NorthboundInterface{
		router: router,
		repo:   repo,
		cfg:    cfg,
	}

	// Register routes
	nb.registerRoutes()

	return nb
}

// registerRoutes registers all REST API routes
func (nb *NorthboundInterface) registerRoutes() {
	// Health check
	nb.router.GET("/health", nb.health)

	// API v1 routes
	api := nb.router.Group("/api/v1")
	{
		// Device-specific routes
		devices := api.Group("/devices/:id", nb.authMiddleware())
		{
			devices.GET("/consumptions", nb.listDeviceConsumptions)
			devices.GET("/accumulations", nb.listDeviceAccumulations)
		}
	}
}

// health handles GET /health
func (nb *NorthboundInterface) health(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
}

// ConsumptionResponse represents a consumption record in the API response
type ConsumptionResponse struct {
	ReportID         string  `json:"report_id"`
	DeviceID         string  `json:"device_id"`
	DebitMsat        int64   `json:"debit_msat"`
	Measure          float64 `json:"measure"`
	PricePerUnitMsat int64   `json:"price_per_unit_msat"`
	Unit             string  `json:"unit"`
	Timestamp        string  `json:"timestamp"`
	CreatedAt        int64   `json:"created_at"`
	Published        bool    `json:"published"`
	Traceparent      string  `json:"traceparent,omitempty"`
}

// AccumulationResponse represents an accumulation ledger entry in the API response
type AccumulationResponse struct {
	ID                     int64   `json:"id"`
	DeviceID               string  `json:"device_id"`
	ReportID               string  `json:"report_id"`
	Type                   string  `json:"type"`
	AmountMsat             float64 `json:"amount_msat"`
	AccumulatedBalanceMsat float64 `json:"accumulated_balance_msat"`
	CreatedAt              int64   `json:"created_at"`
}

// listDeviceConsumptions handles GET /api/v1/devices/:id/consumptions
func (nb *NorthboundInterface) listDeviceConsumptions(c *gin.Context) {
	deviceID := c.Param("id")
	if deviceID == "" {
		c.JSON(400, gin.H{"error": "missing device_id"})
		return
	}

	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	resp, err := nb.repo.ListDeviceConsumptions(c, deviceID, limit)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"items": resp})
}

// listDeviceAccumulations handles GET /api/v1/devices/:id/accumulations
func (nb *NorthboundInterface) listDeviceAccumulations(c *gin.Context) {
	deviceID := c.Param("id")
	if deviceID == "" {
		c.JSON(400, gin.H{"error": "missing device_id"})
		return
	}

	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	resp, err := nb.repo.ListDeviceAccumulations(c, deviceID, limit)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"items": resp})
}

// Start starts the HTTP server
func (nb *NorthboundInterface) Start(ctx context.Context, addr string) error {
	nb.server = &http.Server{
		Addr:    addr,
		Handler: nb.router,
	}

	logger.Infof(nil, "Starting northbound REST API server on %s", addr)
	return nb.server.ListenAndServe()
}

// Stop gracefully stops the HTTP server
func (nb *NorthboundInterface) Stop(ctx context.Context) error {
	if nb.server != nil {
		logger.Info(ctx, "Stopping northbound REST API server")
		return nb.server.Shutdown(ctx)
	}
	return nil
}

// authMiddleware provides authentication middleware for protected endpoints
func (nb *NorthboundInterface) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		got := c.GetHeader("X-Service-Token")
		if nb.cfg.ServiceToken == "" || got == nb.cfg.ServiceToken {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}
