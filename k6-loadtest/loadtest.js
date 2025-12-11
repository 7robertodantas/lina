import { Client } from 'k6/x/mqtt';
import { check } from 'k6';
import { Counter, Rate } from 'k6/metrics';
import http from 'k6/http';
import { randomString, randomIntBetween } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';

// Custom metrics for MQTT operations
const mqttPublishSuccess = new Counter('mqtt_publish_success');
const mqttPublishFailure = new Counter('mqtt_publish_failure');
const mqttPublishRate = new Rate('mqtt_publish_rate');

// Test configuration
export const options = {
  stages: [
    { duration: '10s', target: 5 },   // Ramp up to 5 devices
    { duration: '10s', target: 0 },   // Ramp down
    // { duration: '30s', target: 100 },   // Ramp up to 100 devices
    // { duration: '2m', target: 100 },    // Stay at 100 devices
    // { duration: '30s', target: 500 },  // Ramp up to 500 devices
    // { duration: '2m', target: 500 },    // Stay at 500 devices
    // { duration: '30s', target: 1000 },   // Ramp up to 1000 devices
    // { duration: '5m', target: 1000 },   // Stay at 1000 devices (stress test)
    // { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    // 'http_req_duration': ['p(95)<500'], // 95% of HTTP requests should be below 500ms
    'mqtt_publish_rate': ['rate>0.95'], // 95% of MQTT publishes should succeed
  },
  setupTimeout: '30s', // Increase setup timeout to 2 minutes
};

// Configuration from environment variables
const API_BASE_URL = __ENV.API_BASE_URL || 'http://192.168.0.111:8080';
// Note: If using Caddy reverse proxy, use /devices (Caddy rewrites to /api/v1/devices)
// If accessing device service directly, use /api/v1/devices
const API_DEVICES_ENDPOINT = __ENV.API_DEVICES_ENDPOINT || '/devices';
const MQTT_BROKER = __ENV.MQTT_BROKER || '192.168.0.111';
const MQTT_TLS_PORT = parseInt(__ENV.MQTT_TLS_PORT || '8883');
const USAGE_REPORT_INTERVAL = parseInt(__ENV.USAGE_REPORT_INTERVAL || '1'); // seconds
const AUTHORIZE_REQUEST_MSAT = parseInt(__ENV.AUTHORIZE_REQUEST_MSAT || '1000000000'); // 1 billion msat = 1 BTC
const HEARTBEAT_INTERVAL = parseInt(__ENV.HEARTBEAT_INTERVAL || '60'); // seconds
const ABORT_ON_SETUP_FAILURE = __ENV.ABORT_ON_SETUP_FAILURE !== 'false'; // Default: true

// Generate a unique device ID for this VU
function generateDeviceID(vuID) {
  return `smart-meter-${String(vuID).padStart(6, '0')}`;
}

// Generate a unique request/report ID
function generateID() {
  return randomString(16, 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789');
}

// Get current ISO timestamp
function getISOTimestamp() {
  return new Date().toISOString();
}

// Setup phase: Register device and connect to MQTT
export function setup() {
  const vuID = __VU;
  const deviceID = generateDeviceID(vuID);
  const deviceSecret = `${deviceID}_password`;

  console.log(`[VU ${vuID}] Starting setup for device: ${deviceID}`);

  // Step 1: Register device via POST /api/v1/devices
  const devicePayload = JSON.stringify({
    device_id: deviceID,
    device_secret: deviceSecret,
    measurement_unit: 'kWh',
    unit_price_msat: 1000000,
    reporting_strategy: 'interval',
    reporting_interval: 30,
    heartbeat_interval: HEARTBEAT_INTERVAL,
    authorize_request_msat: AUTHORIZE_REQUEST_MSAT,
    timestamp: getISOTimestamp(),
  });

  const registerRes = http.post(
    `${API_BASE_URL}${API_DEVICES_ENDPOINT}`,
    devicePayload,
    {
      headers: { 'Content-Type': 'application/json' },
      tags: { name: 'RegisterDevice' },
    }
  );

  const registerSuccess = check(registerRes, {
    'device registration successful': (r) => r.status === 200 || r.status === 201,
  });

  if (!registerSuccess) {
    console.error(`[VU ${vuID}] Failed to register device: ${registerRes.status} - ${registerRes.body}`);
    console.error(`[VU ${vuID}] Endpoint: ${API_BASE_URL}${API_DEVICES_ENDPOINT}`);
    if (ABORT_ON_SETUP_FAILURE) {
      console.error(`[VU ${vuID}] Aborting test due to setup failure`);
      throw new Error(`Setup failed: Device registration returned ${registerRes.status}`);
    }
    return null;
  }

  console.log(`[VU ${vuID}] Device registered successfully`);

  // Step 2: Connect to MQTT with TLS
  // Note: Use k6's --insecure-skip-tls-verify flag to skip TLS verification for self-signed certs
  console.log(`[VU ${vuID}] Starting MQTT connection setup`);
  let client;
  try {
    const brokerURL = `ssl://${MQTT_BROKER}:${MQTT_TLS_PORT}`;
    console.log(`[VU ${vuID}] Broker URL: ${brokerURL}`);
    
    // Build connection options
    const clientId = `${deviceID}_k6_${generateID()}`;
    console.log(`[VU ${vuID}] Generated clientId: ${clientId}`);
    
    const connectOptions = {
      clientId: clientId,
      client_id: clientId, // Try both property names
      username: deviceID,
      password: deviceSecret,
      clean: true,
      connectTimeout: 10,
      reconnectPeriod: 5,
    };
    
    console.log(`[VU ${vuID}] Creating MQTT Client instance...`);
    client = new Client(connectOptions);
    console.log(`[VU ${vuID}] MQTT Client instance created`);

    // Set up event handlers before connecting
    console.log(`[VU ${vuID}] Setting up MQTT event handlers...`);
    client.on('connect', () => {
      console.log(`[VU ${vuID}] MQTT connection established (in setup)`);
    });
    
    client.on('error', (err) => {
      console.error(`[VU ${vuID}] MQTT connection error (in setup): ${err}`);
    });
    
    console.log(`[VU ${vuID}] Event handlers set up`);

    // Connect to broker
    console.log(`[VU ${vuID}] Calling client.connect(${brokerURL})...`);
    try {
      client.connect(brokerURL);
      console.log(`[VU ${vuID}] client.connect() returned (should be non-blocking)`);
    } catch (error) {
      console.error(`[VU ${vuID}] Exception during client.connect(): ${error}`);
      throw error;
    }
    
    console.log(`[VU ${vuID}] MQTT connection initiated, proceeding to return from setup`);
  } catch (error) {
    console.error(`[VU ${vuID}] Failed to connect to MQTT: ${error}`);
    if (ABORT_ON_SETUP_FAILURE) {
      console.error(`[VU ${vuID}] Aborting test due to setup failure`);
      throw new Error(`Setup failed: MQTT connection error - ${error}`);
    }
    return null;
  }

  console.log(`[VU ${vuID}] Setup complete, returning data object`);
  return {
    deviceID,
    deviceSecret,
    mqttClient: client,
    mqttConnected: false, // Track connection status
    lastHeartbeat: 0,
    lastUsageReport: 0,
    lastAuthorizeRequest: 0,
  };
}

// Main test function
export default function (data) {
  if (!data || !data.mqttClient) {
    console.error(`[VU ${__VU}] Setup failed, skipping test`);
    return;
  }

  const { deviceID, mqttClient } = data;
  const vuID = __VU;
  const now = Date.now();
  
  // Set up connection event handlers on first iteration
  if (!data.mqttConnected && data.lastHeartbeat === 0) {
    console.log(`[VU ${vuID}] First iteration - setting up MQTT event handlers`);
    
    mqttClient.on('connect', () => {
      data.mqttConnected = true;
      console.log(`[VU ${vuID}] MQTT connection confirmed in default function`);
    });
    
    mqttClient.on('error', (err) => {
      console.error(`[VU ${vuID}] MQTT error in default function: ${err}`);
    });
  }
  
  // Wait for connection before publishing (give it a moment on first iteration)
  if (!data.mqttConnected) {
    // Connection might still be establishing - skip this iteration
    // On next iteration, connection should be ready
    return;
  }
  
  // Initialize timestamps on first iteration if needed
  if (data.lastHeartbeat === 0) {
    data.lastHeartbeat = now - (HEARTBEAT_INTERVAL * 1000); // Set to allow immediate first heartbeat
    console.log(`[VU ${vuID}] First iteration - initializing timestamps`);
  }

  // Publish heartbeat at configured interval
  const timeSinceLastHeartbeat = now - data.lastHeartbeat;
  if (timeSinceLastHeartbeat >= HEARTBEAT_INTERVAL * 1000) {
    console.log(`[VU ${vuID}] Publishing heartbeat (elapsed: ${timeSinceLastHeartbeat}ms)`);
    // Note: protojson with UseProtoNames serializes enums as numeric values or string names
    // Using numeric value 1 for DEVICE_STATUS_ONLINE
    const heartbeatPayload = JSON.stringify({
      device_id: deviceID,
      status: 1, // DEVICE_STATUS_ONLINE = 1
      timestamp: getISOTimestamp(),
    });

    const heartbeatTopic = `/devices/${deviceID}/heartbeat`;
    const heartbeatResult = mqttClient.publish(heartbeatTopic, heartbeatPayload, 1);
    const heartbeatSuccess = check(heartbeatResult, {
      'heartbeat published': (r) => r === true,
    }, { name: 'PublishHeartbeat' });
    
    // Track metrics
    if (heartbeatSuccess) {
      mqttPublishSuccess.add(1);
      mqttPublishRate.add(1);
    } else {
      mqttPublishFailure.add(1);
      mqttPublishRate.add(0);
    }
    
    data.lastHeartbeat = now;
  }

  // Request authorization periodically (every 2 heartbeat intervals)
  if (now - data.lastAuthorizeRequest >= HEARTBEAT_INTERVAL * 2 * 1000) {
    const authorizePayload = JSON.stringify({
      device_id: deviceID,
      request_id: generateID(),
      request_msat: AUTHORIZE_REQUEST_MSAT,
      reason: 'STRESS_TEST',
      timestamp: getISOTimestamp(),
    });

    const authorizeTopic = `/devices/${deviceID}/request/authorize`;
    const authorizeResult = mqttClient.publish(authorizeTopic, authorizePayload, 1);
    const authorizeSuccess = check(authorizeResult, {
      'authorization request published': (r) => r === true,
    }, { name: 'PublishAuthorizeRequest' });
    
    // Track metrics
    if (authorizeSuccess) {
      mqttPublishSuccess.add(1);
      mqttPublishRate.add(1);
    } else {
      mqttPublishFailure.add(1);
      mqttPublishRate.add(0);
    }
    
    data.lastAuthorizeRequest = now;
  }

  // Publish usage report at configured interval
  const timeSinceLastUsage = now - data.lastUsageReport;
  if (timeSinceLastUsage >= USAGE_REPORT_INTERVAL * 1000) {
    console.log(`[VU ${vuID}] Publishing usage report (elapsed: ${timeSinceLastUsage}ms)`);
    const usagePayload = JSON.stringify({
      device_id: deviceID,
      report_id: generateID(),
      strategy: 1, // REPORTING_STRATEGY_INTERVAL = 1
      measure: randomIntBetween(0, 100) / 1000.0, // Random usage between 0 and 0.1 kWh
      unit: 'kWh',
      timestamp: getISOTimestamp(),
    });

    const usageTopic = `/devices/${deviceID}/usage`;
    const usageResult = mqttClient.publish(usageTopic, usagePayload, 1);
    const usageSuccess = check(usageResult, {
      'usage report published': (r) => r === true,
    }, { name: 'PublishUsageReport' });
    
    // Track metrics
    if (usageSuccess) {
      mqttPublishSuccess.add(1);
      mqttPublishRate.add(1);
    } else {
      mqttPublishFailure.add(1);
      mqttPublishRate.add(0);
    }
    
    data.lastUsageReport = now;
  }
  
  // Add a small delay to prevent the function from completing too quickly
  // This ensures k6 sees activity and continues the test
  // The delay is minimal (1ms) but helps with k6's execution model
}

// Teardown: Disconnect MQTT client
export function teardown(data) {
  if (data && data.mqttClient) {
    console.log(`[VU ${__VU}] Disconnecting MQTT client`);
    try {
      data.mqttClient.disconnect();
    } catch (error) {
      console.error(`[VU ${__VU}] Error disconnecting MQTT client: ${error}`);
    }
  }
}

