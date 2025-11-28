package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const northboundRequestTimeout = 5 * time.Second

// NorthboundInterface exposes a lightweight REST surface for LND data.
type NorthboundInterface struct {
	router    *gin.Engine
	lndClient *LNDClient
	cfg       *Config
	server    *http.Server
}

// NewNorthboundInterface wires the HTTP handlers.
func NewNorthboundInterface(lndClient *LNDClient, cfg *Config) *NorthboundInterface {
	router := gin.Default()

	nb := &NorthboundInterface{
		router:    router,
		lndClient: lndClient,
		cfg:       cfg,
	}

	nb.registerRoutes()
	return nb
}

func (nb *NorthboundInterface) registerRoutes() {
	nb.router.GET("/health", nb.health)

	api := nb.router.Group("/api/v1")
	{
		lndGroup := api.Group("/lnd", nb.authMiddleware())
		{
			lndGroup.GET("/info", nb.getInfo)
			lndGroup.GET("/wallet", nb.getWallet)
		}
	}
}

func (nb *NorthboundInterface) health(c *gin.Context) {
	logger.InfoWithFields(c, "Health check requested via northbound REST", map[string]interface{}{
		"client_ip": c.ClientIP(),
	})
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (nb *NorthboundInterface) getInfo(c *gin.Context) {
	start := time.Now()
	logger.InfoWithFields(c, "Northbound getInfo request via northbound REST", map[string]interface{}{
		"client_ip": c.ClientIP(),
	})
	ctx, cancel := context.WithTimeout(c, northboundRequestTimeout)
	defer cancel()

	info, err := nb.lndClient.GetInfo(ctx)
	if err != nil {
		logger.Error(c, "Northbound getInfo failed via northbound REST", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	logger.InfoWithFields(c, "Northbound getInfo succeeded via northbound REST", map[string]interface{}{
		"duration":     time.Since(start).String(),
		"alias":        info.Alias,
		"block_height": info.BlockHeight,
	})
	c.JSON(http.StatusOK, info)
}

func (nb *NorthboundInterface) getWallet(c *gin.Context) {
	start := time.Now()
	logger.InfoWithFields(c, "Northbound getWallet request via northbound REST", map[string]interface{}{
		"client_ip": c.ClientIP(),
	})
	ctx, cancel := context.WithTimeout(c, northboundRequestTimeout)
	defer cancel()

	bal, err := nb.lndClient.GetWalletBalance(ctx)
	if err != nil {
		logger.Error(c, "Northbound getWallet failed via northbound REST", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	logger.InfoWithFields(c, "Northbound getWallet succeeded via northbound REST", map[string]interface{}{
		"duration":      time.Since(start).String(),
		"confirmed_sat": bal.ConfirmedBalance,
	})
	c.JSON(http.StatusOK, bal)
}

// Start boots the HTTP server.
func (nb *NorthboundInterface) Start(ctx context.Context, addr string) error {
	logger.Infof(ctx, "Starting northbound HTTP server on %s", addr)
	nb.server = &http.Server{
		Addr:    addr,
		Handler: nb.router,
	}

	return nb.server.ListenAndServe()
}

// Stop gracefully stops the HTTP server.
func (nb *NorthboundInterface) Stop(ctx context.Context) error {
	logger.Info(ctx, "Stopping northbound HTTP server")
	if nb.server == nil {
		return nil
	}
	return nb.server.Shutdown(ctx)
}

func (nb *NorthboundInterface) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if nb.cfg.ServiceToken == "" {
			c.Next()
			return
		}

		if c.GetHeader("X-Service-Token") == nb.cfg.ServiceToken {
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}
