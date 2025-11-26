package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	deviceID := getEnv("DEVICE_ID", "smart-meter-001")

	// Create the smart meter
	meter := NewSmartMeter(deviceID)

	// Create WebSocket handler (uses meter internals)
	wsHandler := NewWebSocketHandler(meter, meter.southbound)

	// HTTP handlers
	http.HandleFunc("/ws", wsHandler.HandleWebSocket)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	port := getEnv("PORT", "8080")
	log.Printf("Smart Meter Backend starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
