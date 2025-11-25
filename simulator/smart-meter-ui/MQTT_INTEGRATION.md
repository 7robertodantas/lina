# MQTT Integration Summary

## Files Created/Modified

### New Files
1. **`lib/mqtt.ts`** - MQTT client wrapper
   - `MQTTClientWrapper` class with connection management
   - Support for publish, subscribe, unsubscribe operations
   - Event handlers for connect, disconnect, error, and message events
   - Singleton pattern via `getMQTTClient()`

### Modified Files
1. **`hooks/use-mqtt.ts`** - Updated to use real MQTT client
   - Replaced mock connection with actual MQTT.js client
   - Auto-subscribes to device-specific topics on connect
   - Handles incoming messages and routes to appropriate callbacks
   - Cleanup on component unmount

2. **`.env.local`** - Added MQTT credentials
   - `NEXT_PUBLIC_MQTT_BROKER_URL`
   - `NEXT_PUBLIC_MQTT_USERNAME`
   - `NEXT_PUBLIC_MQTT_PASSWORD`

3. **`README.md`** - Updated documentation
   - MQTT integration details
   - Topic structure
   - Features list

## How It Works

1. **Connection Flow**:
   ```
   Component → useMQTT() → getMQTTClient() → mqtt.connect()
   ```

2. **Auto-Subscription**:
   When connected, automatically subscribes to:
   - `/devices/{deviceId}/config`
   - `/devices/{deviceId}/response/authorize`
   - `/devices/{deviceId}/balance`
   - `/devices/{deviceId}/response/invoice`
   - `/devices/{deviceId}/control`

3. **Message Routing**:
   Incoming messages are parsed and routed to registered callbacks based on topic patterns

4. **Publishing**:
   All publish methods (heartbeat, authorize, usage, invoice) use the real MQTT client

## Testing

To test the connection:

1. Ensure your MQTT broker is running on `wss://localhost:9001`
2. Start the dev server: `npm run dev`
3. The app will connect automatically when you start the meter
4. Check browser console for MQTT connection logs

## Connection Events

- `[MQTT] Connecting to...` - Connection initiated
- `[MQTT] Connected successfully` - Connection established
- `[MQTT] Subscribed to {topic}` - Topic subscription confirmed
- `[MQTT] Published to {topic}` - Message published
- `[MQTT] Message received on {topic}` - Incoming message
- `[MQTT] Connection error` - Connection failed
- `[MQTT] Reconnecting...` - Auto-reconnect in progress
