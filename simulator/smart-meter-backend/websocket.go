package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// WebSocket message types and upgrader (scoped to websocket)
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type WSCommand struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data,omitempty"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// WebSocketHandler manages WebSocket connections for the UI
type WebSocketHandler struct {
	meter      *SmartMeter
	southbound *SouthboundInterface
	clients    map[*websocket.Conn]*sync.Mutex
	clientsMu  sync.RWMutex
	broadcast  chan interface{}
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(meter *SmartMeter, southbound *SouthboundInterface) *WebSocketHandler {
	handler := &WebSocketHandler{
		meter:      meter,
		southbound: southbound,
		clients:    make(map[*websocket.Conn]*sync.Mutex),
		broadcast:  make(chan interface{}, 100),
	}

	// Set the meter's state change callback to broadcast to all WS clients
	meter.SetStateChangeCallback(func(state DeviceState) {
		handler.BroadcastState()
	})

	// Start broadcast loop
	go handler.broadcastLoop()

	return handler
}

// Callbacks removed - WebSocketHandler calls meter methods directly

// HandleWebSocket handles WebSocket connections
func (h *WebSocketHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Register client
	h.clientsMu.Lock()
	h.clients[conn] = &sync.Mutex{}
	h.clientsMu.Unlock()

	log.Printf("WebSocket client connected. Total clients: %d", len(h.clients))

	// Send initial state
	state := h.meter.GetState()
	log.Printf("Sending initial state with %d appliances, status: %s", len(state.Appliances), state.DeviceStatus)
	h.sendToClient(conn, WSMessage{
		Type:    "state",
		Payload: h.meter.GetStateJSON(),
	})

	// Cleanup on disconnect
	defer func() {
		h.clientsMu.Lock()
		delete(h.clients, conn)
		h.clientsMu.Unlock()
		log.Printf("WebSocket client disconnected. Total clients: %d", len(h.clients))
	}()

	// Read messages from client
	for {
		var cmd WSCommand
		err := conn.ReadJSON(&cmd)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		h.handleCommand(cmd)
	}
}

// handleCommand processes commands from WebSocket clients
func (h *WebSocketHandler) handleCommand(cmd WSCommand) {
	log.Printf("Received command: %s", cmd.Action)

	switch cmd.Action {
	case "start":
		h.meter.Start()

	case "stop":
		h.meter.Shutdown()

	case "toggle_appliance":
		var data struct {
			ApplianceID string `json:"applianceId"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err == nil {
			h.meter.ToggleAppliance(data.ApplianceID)
		} else {
			log.Printf("Error unmarshaling toggle_appliance data: %v", err)
		}

	case "request_topup":
		var data struct {
			AmountMsat int64 `json:"amountMsat"`
		}
		if err := json.Unmarshal(cmd.Data, &data); err == nil {
			h.meter.RequestTopUp(data.AmountMsat)
		}

	case "simulate_payment":
		h.meter.AddLog("Payment simulation - waiting for backend to confirm", "info")
		// In real implementation, payment would be detected by backend
		// and balance update would come via MQTT balance message

	case "clear_invoice":
		h.meter.ClearInvoice()

	default:
		log.Printf("Unknown command: %s", cmd.Action)
	}
}

// broadcastLoop sends state updates to all connected clients
func (h *WebSocketHandler) broadcastLoop() {
	for msg := range h.broadcast {
		// Take a snapshot of current clients to avoid locking upgrades on write errors
		h.clientsMu.RLock()
		type clientEntry struct {
			conn *websocket.Conn
			mu   *sync.Mutex
		}
		clients := make([]clientEntry, 0, len(h.clients))
		for client, mu := range h.clients {
			clients = append(clients, clientEntry{conn: client, mu: mu})
		}
		h.clientsMu.RUnlock()

		for _, client := range clients {
			client.mu.Lock()
			err := client.conn.WriteJSON(msg)
			client.mu.Unlock()

			if err != nil {
				log.Printf("WebSocket write error: %v", err)
				client.conn.Close()

				h.clientsMu.Lock()
				delete(h.clients, client.conn)
				h.clientsMu.Unlock()
			}
		}
	}
}

// sendToClient sends a message to a specific client
func (h *WebSocketHandler) sendToClient(conn *websocket.Conn, msg WSMessage) {
	h.clientsMu.RLock()
	writeMu, ok := h.clients[conn]
	h.clientsMu.RUnlock()
	if !ok {
		return
	}

	writeMu.Lock()
	err := conn.WriteJSON(msg)
	writeMu.Unlock()

	if err != nil {
		log.Printf("Error sending to client: %v", err)
	}
}

// BroadcastState broadcasts the current state to all clients
func (h *WebSocketHandler) BroadcastState() {
	h.broadcast <- WSMessage{
		Type:    "state",
		Payload: h.meter.GetStateJSON(),
	}
}
