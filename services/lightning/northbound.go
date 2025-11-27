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
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (nb *NorthboundInterface) getInfo(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), northboundRequestTimeout)
	defer cancel()

	info, err := nb.lndClient.GetInfo(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, info)
}

func (nb *NorthboundInterface) getWallet(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), northboundRequestTimeout)
	defer cancel()

	bal, err := nb.lndClient.GetWalletBalance(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, bal)
}

// Start boots the HTTP server.
func (nb *NorthboundInterface) Start(addr string) error {
	nb.server = &http.Server{
		Addr:    addr,
		Handler: nb.router,
	}

	return nb.server.ListenAndServe()
}

// Stop gracefully stops the HTTP server.
func (nb *NorthboundInterface) Stop(ctx context.Context) error {
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
