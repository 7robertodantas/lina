package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"

	"github.com/robertodantas/lina/internal"
)

var logger = internal.NewLogger("smart-meter-core")

func main() {
	ctx := context.Background()

	cfg := LoadConfig()

	// Get device ID and secret from environment variables (required)
	deviceID := os.Getenv("DEVICE_ID")
	if deviceID == "" {
		logger.Fatal(ctx, "DEVICE_ID environment variable is required", nil)
	}

	deviceSecret := os.Getenv("DEVICE_SECRET")
	if deviceSecret == "" {
		logger.Fatal(ctx, "DEVICE_SECRET environment variable is required", nil)
	}

	meter := NewSmartMeter(deviceID, deviceSecret, &cfg)

	// Create WebSocket handler
	wsHandler := NewWebSocketHandler(meter)

	// HTTP handlers
	http.HandleFunc("/ws", wsHandler.HandleWebSocket)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	// Serve static files from /public directory
	fs := http.FileServer(http.Dir("./public"))
	http.Handle("/", fs)

	logger.InfoWithFields(ctx, "Smart Meter Backend starting", map[string]interface{}{
		"port": cfg.HTTPPort,
	})
	logger.Info(ctx, "Serving UI from /public directory")
	if err := http.ListenAndServe(":"+cfg.HTTPPort, nil); err != nil {
		logger.Fatal(ctx, "Server failed", err)
	}
}
