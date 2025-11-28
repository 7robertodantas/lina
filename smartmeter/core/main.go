package main

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/robertodantas/lnpay/internal"
)

var logger = internal.NewLogger("smart-meter-core")

func main() {
	ctx := context.Background()

	cfg := LoadConfig()

	// Create device instance
	deviceID := "smart-meter-001"
	deviceSecret := "smart-meter-001_password"
	meter := NewSmartMeter(deviceID, deviceSecret, cfg)

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
