import { Client } from 'k6/x/mqtt';
import { check, sleep } from 'k6';
import { Counter, Rate } from 'k6/metrics';
import http from 'k6/http';
import { randomString, randomIntBetween } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';

// --- Metrics ---
const mqttPublishSuccess = new Counter('mqtt_publish_success');
const mqttPublishFailure = new Counter('mqtt_publish_failure');
const mqttPublishRate = new Rate('mqtt_publish_rate');

// --- Configuration ---
export const options = {
  stages: [
    { duration: '10s', target: 5 },   
    { duration: '30s', target: 5 },
    { duration: '10s', target: 0 },
    // { duration: '30s', target: 100 },   // Ramp up to 100 devices
    // { duration: '2m', target: 100 },    // Stay at 100 devices
    // { duration: '30s', target: 500 },  // Ramp up to 500 devices
    // { duration: '2m', target: 500 },    // Stay at 500 devices
    // { duration: '30s', target: 1000 },   // Ramp up to 1000 devices
    // { duration: '5m', target: 1000 },   // Stay at 1000 devices (stress test)
    // { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    'mqtt_publish_rate': ['rate>0.95'], 
  },
};

const API_BASE_URL = __ENV.API_BASE_URL || 'http://192.168.0.111:8080';
const API_DEVICES_ENDPOINT = __ENV.API_DEVICES_ENDPOINT || '/devices';
const MQTT_BROKER = __ENV.MQTT_BROKER || '192.168.0.111';
const MQTT_TLS_PORT = parseInt(__ENV.MQTT_TLS_PORT || '8883');
const HEARTBEAT_INTERVAL = parseInt(__ENV.HEARTBEAT_INTERVAL || '60'); 
const USAGE_REPORT_INTERVAL = parseInt(__ENV.USAGE_REPORT_INTERVAL || '1'); 
const AUTHORIZE_REQUEST_MSAT = parseInt(__ENV.AUTHORIZE_REQUEST_MSAT || '1000000000'); 

// --- VU-Global State ---
// These variables persist inside a specific VU as long as the VU is running.
// They are NOT shared between VUs.
let mqttClient = null;
let deviceContext = {
  id: null,
  secret: null,
  connected: false,
  lastHeartbeat: 0,
  lastUsageReport: 0,
  lastAuthorizeRequest: 0
};

// --- Helpers ---
function generateDeviceID(vuID) {
  return `smart-meter-${String(vuID).padStart(6, '0')}`;
}

function generateID() {
  return randomString(16, 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789');
}

function getISOTimestamp() {
  return new Date().toISOString();
}

// --- Setup (Optional) ---
export function setup() {
  // Use setup ONLY for global health checks or getting authentication tokens.
  // DO NOT connect to MQTT here.
  console.log("Starting load test setup...");
}

// --- Main VU Loop ---
export default function () {
  // 1. INITIALIZATION: Run once per VU
  if (!mqttClient) {
    const vuID = __VU;
    const deviceID = generateDeviceID(vuID);
    const deviceSecret = `${deviceID}_password`;
    
    console.log(`[VU ${vuID}] Initializing Device: ${deviceID}`);

    // A. Register Device via HTTP
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
      { headers: { 'Content-Type': 'application/json' } }
    );

    if (registerRes.status !== 200 && registerRes.status !== 201) {
      console.error(`[VU ${vuID}] Registration failed: ${registerRes.status}`);
      sleep(1); 
      return; // Retry next loop
    }

    // B. Create MQTT Client
    const brokerURL = `ssl://${MQTT_BROKER}:${MQTT_TLS_PORT}`;
    const clientId = `${deviceID}_k6_${generateID()}`;
    
    mqttClient = new Client({
      clientId: clientId,
      username: deviceID,
      password: deviceSecret,
      clean: true,
      connectTimeout: 10,
      reconnectPeriod: 5,
    });

    // C. Connect
    try {
      mqttClient.connect(brokerURL);
      console.log(`[VU ${vuID}] Connected to MQTT`);
      
      // Update local state
      deviceContext.id = deviceID;
      deviceContext.secret = deviceSecret;
      deviceContext.connected = true;

      // Initialize timings so we trigger immediate first actions if desired
      const now = Date.now();
      deviceContext.lastHeartbeat = now - (HEARTBEAT_INTERVAL * 1000); 
    } catch (e) {
      console.error(`[VU ${vuID}] MQTT Connect error: ${e}`);
      mqttClient = null; // Reset to try again next loop
      sleep(1);
      return;
    }
  }

  // 2. RUNTIME LOGIC
  // If we are here, we have an active mqttClient for this VU
  
  const now = Date.now();
  
  // Logic: Heartbeat
  if (now - deviceContext.lastHeartbeat >= HEARTBEAT_INTERVAL * 1000) {
    const payload = JSON.stringify({
      device_id: deviceContext.id,
      status: 1, 
      timestamp: getISOTimestamp(),
    });
    
    try {
        mqttClient.publish(`/devices/${deviceContext.id}/heartbeat`, payload, { qos: 1 });
        mqttPublishSuccess.add(1);
        mqttPublishRate.add(1);
    } catch(e) {
        mqttPublishFailure.add(1);
        mqttPublishRate.add(0);
        console.error(`[VU ${__VU}] Publish failed: ${e}`);
    }
    deviceContext.lastHeartbeat = now;
  }

  // Logic: Usage Report
  if (now - deviceContext.lastUsageReport >= USAGE_REPORT_INTERVAL * 1000) {
    const payload = JSON.stringify({
      device_id: deviceContext.id,
      report_id: generateID(),
      strategy: 1,
      measure: randomIntBetween(0, 100) / 1000.0,
      unit: 'kWh',
      timestamp: getISOTimestamp(),
    });

    try {
        mqttClient.publish(`/devices/${deviceContext.id}/usage`, payload, { qos: 1 });
        mqttPublishSuccess.add(1);
        mqttPublishRate.add(1);
    } catch(e) {
        mqttPublishFailure.add(1);
        mqttPublishRate.add(0);
    }
    deviceContext.lastUsageReport = now;
  }

  // IMPORTANT: k6 execution model is a loop.
  // Add a small sleep to prevent tight looping CPU spikes if no work was done.
  // k6/x/mqtt is async, but the loop needs to yield.
  sleep(0.1); 
}

// --- Teardown ---
// Does not have access to VU-Global state (mqttClient), 
// so we cannot disconnect here. 
// k6/x/mqtt automatically closes connections when the VU stops.
export function teardown() {
  console.log("Load test finished.");
}