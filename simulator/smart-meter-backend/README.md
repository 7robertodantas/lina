# Smart Meter Backend

A Go-based backend service that acts as middleware between the smart meter UI and the MQTT edge node. This service simplifies the UI by handling all business logic, state management, consumption simulation, and MQTT communication server-side.

## Features

- **WebSocket Protocol**: Real-time bidirectional communication with the UI
- **State Management**: Centralized server-side state for device, appliances, and balance
- **Consumption Simulation**: Realistic power consumption calculation with variance
- **MQTT Integration**: Full integration with the edge node for device messaging
- **Event Logging**: In-memory event log streamed to connected clients
- **Multi-client Support**: Multiple UI instances can connect simultaneously

## Architecture

```
┌─────────────┐         WebSocket          ┌──────────────────┐         MQTT          ┌─────────────┐
│             │◄──────────────────────────►│                  │◄────────────────────►│             │
│  React UI   │   (Commands & State)       │  Go Backend      │  (Device Messages)   │  MQTT Edge  │
│             │                             │                  │                       │    Node     │
└─────────────┘                             └──────────────────┘                       └─────────────┘
                                                    │
                                                    ├─ State Management
                                                    ├─ Consumption Simulation  
                                                    ├─ Balance Tracking
                                                    └─ Event Logging
```

## WebSocket Protocol

### Client → Server Commands

All commands are sent as JSON with this structure:
```json
{
  "action": "command_name",
  "data": { /* optional payload */ }
}
```

#### Available Commands:

1. **Start Meter**
   ```json
   { "action": "start" }
   ```

2. **Stop Meter**
   ```json
   { "action": "stop" }
   ```

3. **Toggle Appliance**
   ```json
   {
     "action": "toggle_appliance",
     "data": { "applianceId": "fridge" }
   }
   ```

4. **Request Top-up**
   ```json
   {
     "action": "request_topup",
     "data": { "amountMsat": 100000 }
   }
   ```

5. **Simulate Payment** (for testing)
   ```json
   { "action": "simulate_payment" }
   ```

6. **Clear Invoice**
   ```json
   { "action": "clear_invoice" }
   ```

### Server → Client Messages

All server messages follow this structure:
```json
{
  "type": "message_type",
  "payload": { /* message data */ }
}
```

#### Message Types:

1. **State Update** (sent on connection and after every state change)
   ```json
   {
     "type": "state",
     "payload": {
       "deviceId": "smart-meter-001",
       "deviceStatus": "ONLINE",
       "appliances": [...],
       "balance": {...},
       "config": {...},
       "totalConsumption": 1.234,
       "instantPower": 1500,
       "invoice": {...},
       "authorizations": [...],
       "logs": [...],
       "mqttStatus": "connected"
     }
   }
   ```

## State Management

The backend maintains the complete device state:

- **Device Status**: OFFLINE, STARTING, ONLINE, PAUSED, ERROR
- **Appliances**: Array of appliances with current power consumption
- **Balance**: Available, reserved, and total balance in millisatoshis
- **Configuration**: Device settings (intervals, pricing, etc.)
- **Consumption**: Total accumulated consumption and instant power
- **Authorizations**: Active payment authorizations
- **Logs**: Event log (max 50 entries)

## Consumption Simulation

The backend simulates realistic power consumption:

1. **Power Variance**: Each appliance varies between min/max watts with ±20% random variance
2. **Update Frequency**: Power readings updated every 1 second
3. **Accumulation**: Total consumption calculated based on reporting interval
4. **Cost Calculation**: Automatic balance deduction based on consumption and unit price
5. **Out of Funds**: Automatically stops all appliances when balance reaches 0

## MQTT Integration

The backend acts as an MQTT client and handles these topics:

### Published Topics:
- `/devices/{device_id}/heartbeat` - Device status updates
- `/devices/{device_id}/request/authorize` - Authorization requests
- `/devices/{device_id}/usage` - Consumption reports
- `/devices/{device_id}/request/invoice` - Invoice requests

### Subscribed Topics:
- `/devices/{device_id}/config` - Configuration updates
- `/devices/{device_id}/response/authorize` - Authorization responses
- `/devices/{device_id}/balance` - Balance updates
- `/devices/{device_id}/response/invoice` - Invoice responses
- `/devices/{device_id}/control` - Control commands

## Environment Variables

Create a `.env` file or set these environment variables:

```bash
# Server Configuration
PORT=8080

# Device Configuration
DEVICE_ID=smart-meter-001

# MQTT Configuration
MQTT_BROKER=mosquitto                    # MQTT broker hostname
MQTT_PORT=1883                           # Non-TLS port (usually disabled)
MQTT_TLS_PORT=8883                       # TLS port
MQTT_USE_TLS=true                        # Enable/disable TLS (default: true)
MQTT_TLS_SKIP_VERIFY=false               # Skip TLS certificate verification (not recommended for production)
MQTT_TLS_SERVER_NAME=mosquitto           # Server name for TLS verification
MQTT_TLS_CA_CERT=/certs/ca.crt          # Path to CA certificate
MQTT_USERNAME=smart-meter-001            # MQTT username
MQTT_PASSWORD=smart-meter-001_password   # MQTT password
```

## Running the Backend

### Local Development

```bash
# Install dependencies
go mod download

# Run the server
go run main.go

# Or with custom port
PORT=8080 go run main.go
```

### With TLS (Production-like)

The backend connects to MQTT with TLS by default (matching the device service configuration).

```bash
# Ensure certs are available
export MQTT_USE_TLS=true
export MQTT_TLS_CA_CERT=/path/to/certs/ca.crt
export MQTT_BROKER=mosquitto
export MQTT_TLS_PORT=8883
go run main.go
```

### Without TLS (Local Testing)

If you want to disable TLS for local testing:

```bash
export MQTT_USE_TLS=false
export MQTT_BROKER=localhost
export MQTT_PORT=1883
go run main.go
```

### With Environment File

```bash
# Load from .env
export $(cat .env | xargs)
go run main.go
```

### Build and Run

```bash
# Build binary
go build -o smart-meter-backend

# Run
./smart-meter-backend
```

## Testing WebSocket Connection

You can test the WebSocket connection using `wscat`:

```bash
# Install wscat
npm install -g wscat

# Connect
wscat -c ws://localhost:8080/ws

# Send commands
{"action": "start"}
{"action": "toggle_appliance", "data": {"applianceId": "fridge"}}
```

## Event Log

The backend maintains an in-memory event log that shows:
- System events (startup, shutdown)
- MQTT events (connection, messages)
- Appliance events (on/off)
- Balance events (authorization, payment, low balance)
- Errors and warnings

Logs are automatically broadcast to all connected clients and limited to 50 entries.

## Multi-Client Support

Multiple UI instances can connect simultaneously. All clients receive:
- Initial state on connection
- Real-time state updates
- Synchronized event logs

This prevents the React development environment from creating duplicate state issues.

## Health Check

The backend exposes a health check endpoint:

```bash
curl http://localhost:8080/health
# Response: {"status":"healthy"}
```

## Future Enhancements

- [ ] Persistent state (Redis/PostgreSQL)
- [ ] Authentication/Authorization
- [ ] Rate limiting
- [ ] Metrics and monitoring
- [ ] Docker support
- [ ] Configuration file support
- [ ] Historical consumption data
- [ ] Export logs to file
