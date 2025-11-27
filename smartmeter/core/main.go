package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func main() {
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

	log.Printf("Smart Meter Backend starting on port %s", cfg.HTTPPort)
	log.Fatal(http.ListenAndServe(":"+cfg.HTTPPort, nil))
}
