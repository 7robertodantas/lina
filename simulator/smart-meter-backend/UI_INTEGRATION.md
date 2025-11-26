# UI Integration Guide

This guide explains how to update the React smart-meter-ui to use the Go backend instead of direct MQTT connection.

## Overview

The Go backend now handles:
- ✅ MQTT connection and message handling
- ✅ State management (appliances, balance, consumption)
- ✅ Consumption simulation and power variance
- ✅ Event logging
- ✅ Authorization and invoice management

The React UI simplifies to:
- ✅ Display state received from backend
- ✅ Send user commands via WebSocket
- ✅ No MQTT client code needed
- ✅ No duplicate state in dev environment

## Architecture Change

### Before (Current)
```
React UI ──MQTT──► MQTT Broker (Edge Node)
   │
   └─ State Management
   └─ Consumption Simulation
   └─ MQTT Client
```

### After (With Backend)
```
React UI ──WebSocket──► Go Backend ──MQTT──► MQTT Broker (Edge Node)
   │                         │
   └─ Display Only           ├─ State Management
                              ├─ Consumption Simulation
                              └─ MQTT Client
```

## Step-by-Step Integration

### 1. Create New WebSocket Hook

Create `hooks/use-backend.ts`:

```typescript
"use client"

import { useState, useCallback, useEffect, useRef } from "react"
import type { DeviceState } from "@/lib/types"

interface WSCommand {
  action: string
  data?: any
}

interface WSMessage {
  type: string
  payload: any
}

export function useBackend() {
  const [state, setState] = useState<DeviceState | null>(null)
  const [connectionStatus, setConnectionStatus] = useState<"disconnected" | "connecting" | "connected" | "error">("disconnected")
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<NodeJS.Timeout>()

  const connect = useCallback(() => {
    const backendUrl = process.env.NEXT_PUBLIC_BACKEND_WS_URL || "ws://localhost:8080/ws"
    
    setConnectionStatus("connecting")
    const ws = new WebSocket(backendUrl)
    wsRef.current = ws

    ws.onopen = () => {
      console.log("[Backend] Connected")
      setConnectionStatus("connected")
    }

    ws.onmessage = (event) => {
      try {
        const message: WSMessage = JSON.parse(event.data)
        
        if (message.type === "state") {
          setState(message.payload as DeviceState)
        }
      } catch (error) {
        console.error("[Backend] Error parsing message:", error)
      }
    }

    ws.onerror = (error) => {
      console.error("[Backend] WebSocket error:", error)
      setConnectionStatus("error")
    }

    ws.onclose = () => {
      console.log("[Backend] Disconnected")
      setConnectionStatus("disconnected")
      
      // Auto-reconnect after 3 seconds
      reconnectTimeoutRef.current = setTimeout(() => {
        connect()
      }, 3000)
    }
  }, [])

  const disconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current)
    }
    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }
  }, [])

  const sendCommand = useCallback((action: string, data?: any) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      const command: WSCommand = { action, data }
      wsRef.current.send(JSON.stringify(command))
    }
  }, [])

  // Auto-connect on mount
  useEffect(() => {
    connect()
    return () => disconnect()
  }, [connect, disconnect])

  return {
    state,
    connectionStatus,
    sendCommand,
    connect,
    disconnect,
  }
}
```

### 2. Update Types

Add to `lib/types.ts`:

```typescript
export interface DeviceState {
  deviceId: string
  deviceStatus: "OFFLINE" | "STARTING" | "ONLINE" | "PAUSED" | "ERROR"
  appliances: Appliance[]
  balance: BalanceMessage | null
  config: DeviceConfig
  totalConsumption: number
  instantPower: number
  invoice: InvoiceResponse | null
  authorizations: Authorization[]
  logs: LogEntry[]
  mqttStatus: "disconnected" | "connecting" | "connected" | "error"
}

export interface LogEntry {
  id: string
  timestamp: string
  message: string
  type: "info" | "error" | "success"
}
```

### 3. Simplify Smart Meter Hook

Replace `hooks/use-smart-meter.ts` with:

```typescript
"use client"

import { useBackend } from "./use-backend"

export function useSmartMeter() {
  const { state, connectionStatus, sendCommand } = useBackend()

  const startMeter = () => sendCommand("start")
  const stopMeter = () => sendCommand("stop")
  const toggleAppliance = (applianceId: string) => 
    sendCommand("toggle_appliance", { applianceId })
  const requestTopUp = (amountMsat: number) => 
    sendCommand("request_topup", { amountMsat })
  const simulatePayment = () => sendCommand("simulate_payment")
  const clearInvoice = () => sendCommand("clear_invoice")

  return {
    deviceId: state?.deviceId || "",
    deviceStatus: state?.deviceStatus || "OFFLINE",
    appliances: state?.appliances || [],
    balance: state?.balance || null,
    totalConsumption: state?.totalConsumption || 0,
    instantPower: state?.instantPower || 0,
    invoice: state?.invoice || null,
    logs: state?.logs || [],
    mqttStatus: connectionStatus,
    startMeter,
    stopMeter,
    toggleAppliance,
    requestTopUp,
    simulatePayment,
    clearInvoice,
  }
}
```

### 4. Environment Variables

Update `.env.local` in smart-meter-ui:

```bash
# Backend WebSocket URL
NEXT_PUBLIC_BACKEND_WS_URL=ws://localhost:8080/ws

# These are now only needed by the Go backend, not the UI
# NEXT_PUBLIC_MQTT_BROKER_URL=...
# NEXT_PUBLIC_MQTT_USERNAME=...
# NEXT_PUBLIC_MQTT_PASSWORD=...
```

### 5. Remove Unused Dependencies (Optional)

You can now remove MQTT-related dependencies from the UI:

```bash
cd smart-meter-ui
npm uninstall mqtt
```

Remove or deprecate:
- `lib/mqtt.ts` (no longer needed)
- `hooks/use-mqtt.ts` (no longer needed)

## Benefits

### ✅ Simplified UI Code
- No MQTT client management
- No state duplication in React dev mode
- Single source of truth (backend)

### ✅ Better Performance
- Lighter UI bundle (no MQTT library)
- Server-side consumption simulation
- Efficient WebSocket updates

### ✅ Easier Testing
- Mock backend responses easily
- No need to run MQTT broker for UI testing
- Clear separation of concerns

### ✅ Production Ready
- Centralized logging
- Better error handling
- Multiple clients supported

## Running Both Services

### Terminal 1 - Backend
```bash
cd simulator/smart-meter-backend
export MQTT_BROKER_URL=tcp://localhost:1883
export DEVICE_ID=smart-meter-001
go run main.go
```

### Terminal 2 - UI
```bash
cd simulator/smart-meter-ui
npm run dev
```

## Testing the Integration

1. Start the backend: `go run main.go`
2. Start the UI: `npm run dev`
3. Open browser to `http://localhost:3000`
4. Open browser console to see WebSocket connection logs
5. Click "Start" - should see state updates in real-time
6. Toggle appliances - should see immediate feedback

## Troubleshooting

### WebSocket Connection Failed
- Ensure backend is running on port 8080
- Check `NEXT_PUBLIC_BACKEND_WS_URL` in `.env.local`
- Check browser console for CORS errors

### State Not Updating
- Check backend logs for errors
- Verify WebSocket connection in browser Network tab
- Check that commands are being sent (see Network/WS tab)

### MQTT Issues
- Backend handles MQTT, not UI
- Check backend logs for MQTT connection status
- Verify MQTT broker is running and accessible

## Migration Checklist

- [ ] Create `hooks/use-backend.ts`
- [ ] Update `lib/types.ts` with DeviceState and LogEntry
- [ ] Simplify `hooks/use-smart-meter.ts`
- [ ] Update `.env.local` with NEXT_PUBLIC_BACKEND_WS_URL
- [ ] Test WebSocket connection
- [ ] Verify all commands work (start, stop, toggle, etc.)
- [ ] Remove unused MQTT code (optional)
- [ ] Update documentation

## Future Considerations

- Add authentication/authorization
- Implement reconnection with exponential backoff
- Add heartbeat/ping-pong for connection health
- Implement request/response pattern for critical operations
- Add loading states during commands
