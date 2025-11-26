# Testing Guide - Backend Integration

## Quick Start

### 1. Start the Go Backend

In terminal 1:
```bash
cd simulator/smart-meter-backend
go run main.go
```

Expected output:
```
Smart Meter Backend starting on port 8080
```

### 2. Start the React UI

In terminal 2:
```bash
cd simulator/smart-meter-ui
npm run dev
```

Expected output:
```
▲ Next.js 14.x.x
- Local:        http://localhost:3000
```

### 3. Open Browser

Navigate to `http://localhost:3000`

## What to Test

### ✅ WebSocket Connection
1. Open browser DevTools (F12)
2. Go to Console tab
3. Look for: `[Backend] Connected`
4. Check Network tab → WS → you should see a WebSocket connection to `ws://localhost:8080/ws`

### ✅ Initial State
- Device should show as "OFFLINE"
- All appliances should be OFF
- No balance displayed yet

### ✅ Start Meter
1. Click "Start" button
2. Console should show state updates
3. Device status should change: OFFLINE → STARTING → ONLINE
4. MQTT status should show "connected"
5. Event log should show startup messages

### ✅ Appliance Control
1. Toggle refrigerator ON
2. Should see power consumption (100-150W)
3. Console shows state update with appliance ON
4. Try other appliances

### ✅ Power Simulation
- Watch the power readings update every second
- Values should vary slightly (realistic simulation)
- Total instant power = sum of all ON appliances

### ✅ Balance & Top-up
1. If you have MQTT broker running with backend services:
   - Authorization should be granted
   - Balance should appear
2. Click "Request Top-up"
3. Invoice QR code should appear
4. Click "Simulate Payment"
5. Balance should increase

### ✅ Out of Funds
If balance reaches 0:
- All appliances should automatically turn OFF
- Error message in event log
- Cannot turn appliances back ON until balance is added

### ✅ Event Log
- Every action should generate log entries
- Logs appear in real-time
- Different types: info (blue), success (green), error (red)

### ✅ Stop Meter
1. Click "Stop"
2. Device status → OFFLINE
3. All appliances turn OFF
4. MQTT disconnects

## Troubleshooting

### WebSocket Connection Failed
**Symptom:** Console shows connection errors, no state updates

**Solutions:**
- Ensure backend is running: `go run main.go`
- Check port 8080 is not in use: `lsof -i :8080`
- Verify `.env.local` has: `NEXT_PUBLIC_BACKEND_WS_URL=ws://localhost:8080/ws`

### State Not Updating
**Symptom:** UI doesn't respond to commands

**Solutions:**
- Check browser console for errors
- Verify WebSocket is "connected" in DevTools → Network → WS
- Restart both backend and UI

### Backend Crashes
**Symptom:** Backend exits with error

**Solutions:**
- Check backend logs for specific error
- Verify Go dependencies: `go mod download`
- Check MQTT broker is accessible (if configured)

### MQTT Not Working
**Symptom:** No balance, no authorization

**Solutions:**
- Backend can run without MQTT for UI testing
- Set mock MQTT_BROKER_URL if needed
- Backend will simulate locally if MQTT is unavailable

## Expected Console Output

### Successful Connection:
```
[Backend] Connected
```

### State Updates:
```json
{
  "type": "state",
  "payload": {
    "deviceId": "smart-meter-001",
    "deviceStatus": "ONLINE",
    "appliances": [...],
    "instantPower": 1500,
    ...
  }
}
```

### Commands Sent:
```json
{"action": "start"}
{"action": "toggle_appliance", "data": {"applianceId": "fridge"}}
```

## Performance Check

### Normal Behavior:
- WebSocket messages: ~1-2 per second during operation
- UI updates: Smooth, no lag
- Memory usage: Stable
- CPU: Low usage

### Issues to Report:
- Excessive WebSocket reconnections
- UI freezing or lagging
- Memory leaks (increasing over time)
- Missing state updates

## Testing Multiple Browser Tabs

1. Open `http://localhost:3000` in 2+ tabs
2. All tabs should connect to the same backend
3. All tabs show the SAME state (synchronized)
4. Actions in one tab update all other tabs instantly
5. This proves the backend is the single source of truth!

## Advanced Testing

### With MQTT Broker

If you have the full MQTT setup:

1. Start MQTT broker
2. Start backend with MQTT env vars:
   ```bash
   export MQTT_BROKER_URL=tcp://localhost:1883
   export MQTT_USERNAME=smart-meter-001
   export MQTT_PASSWORD=smart-meter-001_password
   go run main.go
   ```
3. Backend should connect to MQTT
4. Full authorization/balance flow should work

### WebSocket Inspector

Install browser extension like "WebSocket King" or use wscat:

```bash
npm install -g wscat
wscat -c ws://localhost:8080/ws

# Send test commands
{"action": "start"}
{"action": "toggle_appliance", "data": {"applianceId": "fridge"}}
{"action": "stop"}
```

## Success Criteria

✅ WebSocket connects successfully  
✅ Initial state received  
✅ Start/Stop commands work  
✅ Appliances toggle on/off  
✅ Power consumption simulates realistically  
✅ Event log updates in real-time  
✅ Multiple tabs stay synchronized  
✅ No console errors  
✅ UI is responsive  

## Next Steps After Testing

If all tests pass:
1. ✅ Backend integration is complete!
2. Consider removing old MQTT client code from UI
3. Deploy backend to production environment
4. Configure production MQTT broker
5. Add authentication if needed

## Reporting Issues

If you encounter problems, provide:
- Browser console output
- Backend terminal output
- Steps to reproduce
- Expected vs actual behavior
