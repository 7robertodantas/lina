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
    { duration: '10s', target: 1 },   
    { duration: '30s', target: 1 },
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
const INVOICE_REQUEST_INTERVAL = parseInt(__ENV.INVOICE_REQUEST_INTERVAL || '5'); // Request invoice every 5 seconds
const INVOICE_AMOUNT_MSAT = parseInt(__ENV.INVOICE_AMOUNT_MSAT || '100000000'); // 0.1 BTC 

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
  lastAuthorizeRequest: 0,
  lastInvoiceRequest: 0,
  availableMsat: 0,           // Current available balance
  reportingEnabled: false,     // Whether usage reporting is enabled (set by RESUME/STOP)
  subscriptionsReady: false,   // Whether MQTT subscriptions are ready
  authorizationStatus: null,   // null, 'GRANTED', 'ACTIVE', 'REJECTED'
  initialAuthSent: false,       // Whether initial authorization request has been sent
  pendingAuthorization: false,  // Whether an authorization request is pending
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

// --- Message Handlers ---
function handleBalanceMessage(vuID, payload) {
  // Update balance from message
  // BalancePayload JSON format: { device_id, available_msat (string), reserved_msat, total_msat, timestamp }
  // Example: {"device_id":"smart-meter-001", "available_msat":"31701", "total_msat":"31701", "timestamp":"2025-12-11T13:18:55Z"}
  if (payload && payload.available_msat !== undefined) {
    // available_msat comes as a string in JSON, convert to number
    const availableMsat = typeof payload.available_msat === 'string' 
      ? parseInt(payload.available_msat, 10) 
      : parseInt(payload.available_msat) || 0;
    const previousBalance = deviceContext.availableMsat;
    deviceContext.availableMsat = availableMsat;
    console.log(`[VU ${vuID}] Balance updated: ${deviceContext.availableMsat} msat available`);
    
    // If balance is >= 0 and we don't have an active authorization, request authorization
    // But only if we haven't just sent an invoice request (wait for it to be paid first)
    const now = Date.now();
    const timeSinceLastInvoice = now - deviceContext.lastInvoiceRequest;
    const shouldRequestAuth = availableMsat >= 0 && 
        deviceContext.authorizationStatus !== 'GRANTED' && 
        deviceContext.authorizationStatus !== 'ACTIVE' &&
        !deviceContext.pendingAuthorization &&
        timeSinceLastInvoice >= INVOICE_REQUEST_INTERVAL * 1000; // Wait at least INVOICE_REQUEST_INTERVAL before trying auth after invoice
    
    if (shouldRequestAuth) {
      const authPayload = JSON.stringify({
        device_id: deviceContext.id,
        request_id: generateID(),
        request_msat: AUTHORIZE_REQUEST_MSAT,
        reason: 'FUNDS_AVAILABLE',
        timestamp: getISOTimestamp(),
      });
      
      try {
        mqttClient.publish(`/devices/${deviceContext.id}/request/authorize`, authPayload, { qos: 1 });
        mqttPublishSuccess.add(1);
        mqttPublishRate.add(1);
        deviceContext.lastAuthorizeRequest = now;
        deviceContext.pendingAuthorization = true;
        console.log(`[VU ${vuID}] Authorization request sent after balance update: ${AUTHORIZE_REQUEST_MSAT} msat (balance: ${availableMsat} msat)`);
      } catch (e) {
        mqttPublishFailure.add(1);
        mqttPublishRate.add(0);
        console.error(`[VU ${vuID}] Authorization request failed: ${e}`);
      }
    }
  }
}

function handleAuthorizationResponse(vuID, payload) {
  // AuthorizationResponsePayload JSON format: { device_id, request_id, status, authorization_id, granted_msat, remaining_msat, issued_at, expires_at, reason, available_msat }
  // status: "AUTHORIZATION_STATUS_GRANTED", "AUTHORIZATION_STATUS_ACTIVE", "AUTHORIZATION_STATUS_REJECTED"
  if (!payload || !payload.status) return;
  
  const status = payload.status;
  
  if (status === 'AUTHORIZATION_STATUS_GRANTED') {
    deviceContext.authorizationStatus = 'GRANTED';
    deviceContext.pendingAuthorization = false;
    deviceContext.reportingEnabled = true; // Enable usage reporting when authorization is granted
    deviceContext.lastUsageReport = 0; // Reset to 0 to allow immediate first report on next loop
    console.log(`[VU ${vuID}] Authorization GRANTED: ${payload.granted_msat || 0} msat (request_id: ${payload.request_id || 'N/A'}) - usage reporting enabled`);
  } else if (status === 'AUTHORIZATION_STATUS_ACTIVE') {
    deviceContext.authorizationStatus = 'ACTIVE';
    deviceContext.pendingAuthorization = false;
    deviceContext.reportingEnabled = true; // Enable usage reporting when authorization is active
    deviceContext.lastUsageReport = 0; // Reset to 0 to allow immediate first report on next loop
    console.log(`[VU ${vuID}] Authorization ACTIVE: ${payload.remaining_msat || 0} msat remaining (request_id: ${payload.request_id || 'N/A'}) - usage reporting enabled`);
  } else if (status === 'AUTHORIZATION_STATUS_REJECTED') {
    deviceContext.pendingAuthorization = false;
    deviceContext.authorizationStatus = 'REJECTED';
    const reason = payload.reason || 'INSUFFICIENT_FUNDS';
    console.log(`[VU ${vuID}] Authorization REJECTED: ${reason} (request_id: ${payload.request_id || 'N/A'})`);
    
    // If rejected due to insufficient funds, request invoice only if we haven't sent one recently
    if ((reason === 'INSUFFICIENT_FUNDS' || reason.includes('INSUFFICIENT')) &&
        (Date.now() - deviceContext.lastInvoiceRequest >= INVOICE_REQUEST_INTERVAL * 1000)) {
      const invoicePayload = JSON.stringify({
        device_id: deviceContext.id,
        request_id: generateID(),
        amount_msat: INVOICE_AMOUNT_MSAT,
        reason: 'USER_TOPUP',
        timestamp: getISOTimestamp(),
      });
      
      try {
        mqttClient.publish(`/devices/${deviceContext.id}/request/invoice`, invoicePayload, { qos: 1 });
        mqttPublishSuccess.add(1);
        mqttPublishRate.add(1);
        deviceContext.lastInvoiceRequest = Date.now();
        console.log(`[VU ${vuID}] Invoice request sent immediately after rejection: ${INVOICE_AMOUNT_MSAT} msat`);
      } catch (e) {
        mqttPublishFailure.add(1);
        mqttPublishRate.add(0);
        console.error(`[VU ${vuID}] Invoice request failed: ${e}`);
      }
    } else if (reason === 'INSUFFICIENT_FUNDS' || reason.includes('INSUFFICIENT')) {
      console.log(`[VU ${vuID}] Skipping invoice request - already sent recently (${Date.now() - deviceContext.lastInvoiceRequest}ms ago)`);
    }
  } else {
    console.log(`[VU ${vuID}] Unknown authorization status: ${status}`);
  }
}

function handleControlMessage(vuID, payload) {
  // ControlPayload JSON format: { command, reason, id, authorization_id }
  // command: "CONTROL_COMMAND_STOP", "CONTROL_COMMAND_PAUSE", "CONTROL_COMMAND_RESUME", etc.
  if (!payload || !payload.command) return;
  
  const command = payload.command;
  
  if (command === 'CONTROL_COMMAND_STOP') {
    deviceContext.reportingEnabled = false;
    console.log(`[VU ${vuID}] Control command STOP received: ${payload.reason || 'REMOTE_COMMAND'}`);
  } else if (command === 'CONTROL_COMMAND_PAUSE') {
    deviceContext.reportingEnabled = false;
    console.log(`[VU ${vuID}] Control command PAUSE received: ${payload.reason || 'REMOTE_COMMAND'}`);
  } else if (command === 'CONTROL_COMMAND_RESUME') {
    deviceContext.reportingEnabled = true;
    deviceContext.lastUsageReport = 0; // Reset to 0 to allow immediate first report on next loop
    console.log(`[VU ${vuID}] Control command RESUME received - enabling usage reporting`);
  } else if (command === 'CONTROL_COMMAND_AUTHORIZATION') {
    // Publish authorization request
    const authPayload = JSON.stringify({
      device_id: deviceContext.id,
      request_id: generateID(),
      request_msat: AUTHORIZE_REQUEST_MSAT,
      reason: payload.reason || 'AUTHORIZATION_REQUIRED',
      timestamp: getISOTimestamp(),
    });
    
    try {
      mqttClient.publish(`/devices/${deviceContext.id}/request/authorize`, authPayload, { qos: 1 });
      mqttPublishSuccess.add(1);
      mqttPublishRate.add(1);
      console.log(`[VU ${vuID}] Authorization request sent (command): ${AUTHORIZE_REQUEST_MSAT} msat`);
    } catch (e) {
      mqttPublishFailure.add(1);
      mqttPublishRate.add(0);
      console.error(`[VU ${vuID}] Authorization request failed: ${e}`);
    }
  } else {
    console.log(`[VU ${vuID}] Control command received: ${command} (reason: ${payload.reason || 'N/A'})`);
  }
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
      deviceContext.lastInvoiceRequest = now - (INVOICE_REQUEST_INTERVAL * 1000);
      deviceContext.lastUsageReport = now - (USAGE_REPORT_INTERVAL * 1000); // Initialize to allow immediate first report

      // D. Subscribe to balance, control, and authorization response topics
      const balanceTopic = `/devices/${deviceID}/balance`;
      const controlTopic = `/devices/${deviceID}/control`;
      const authorizeResponseTopic = `/devices/${deviceID}/response/authorize`;
      
      try {
        // Subscribe to balance topic
        mqttClient.subscribe(balanceTopic, { qos: 1 }, (err) => {
          if (err) {
            console.error(`[VU ${vuID}] Failed to subscribe to balance: ${err}`);
          } else {
            console.log(`[VU ${vuID}] Subscribed to ${balanceTopic}`);
          }
        });

        // Subscribe to control topic
        mqttClient.subscribe(controlTopic, { qos: 1 }, (err) => {
          if (err) {
            console.error(`[VU ${vuID}] Failed to subscribe to control: ${err}`);
          } else {
            console.log(`[VU ${vuID}] Subscribed to ${controlTopic}`);
          }
        });

        // Subscribe to authorization response topic
        mqttClient.subscribe(authorizeResponseTopic, { qos: 1 }, (err) => {
          if (err) {
            console.error(`[VU ${vuID}] Failed to subscribe to authorization response: ${err}`);
          } else {
            console.log(`[VU ${vuID}] Subscribed to ${authorizeResponseTopic}`);
          }
        });

        // Set up message handler - MQTT messages are in JSON format
        const messageHandler = (topic, message) => {
          try {
            let payload;
            
            // Convert message to string if needed
            let messageString;
            if (typeof message === 'string') {
              messageString = message;
            } else if (message instanceof ArrayBuffer) {
              messageString = String.fromCharCode.apply(null, new Uint8Array(message));
            } else if (message && message.buffer instanceof ArrayBuffer) {
              messageString = String.fromCharCode.apply(null, new Uint8Array(message.buffer, message.byteOffset || 0, message.byteLength || message.length));
            } else if (typeof message === 'object' && message !== null) {
              // Already an object (may be auto-parsed by k6/x/mqtt)
              payload = message;
            } else {
              // Try toString as fallback
              messageString = String(message);
            }
            
            // Parse JSON if we have a string
            if (messageString !== undefined && !payload) {
              payload = JSON.parse(messageString);
            }
            
            if (payload) {
              if (topic === balanceTopic) {
                handleBalanceMessage(vuID, payload);
              } else if (topic === controlTopic) {
                handleControlMessage(vuID, payload);
              } else if (topic === authorizeResponseTopic) {
                handleAuthorizationResponse(vuID, payload);
              }
            }
          } catch (e) {
            console.error(`[VU ${vuID}] Error parsing JSON message from ${topic}: ${e}`);
          }
        };

        // Try different message handler patterns based on k6/x/mqtt API
        if (mqttClient.onMessage) {
          mqttClient.onMessage(messageHandler);
        } else if (mqttClient.on) {
          // Fallback to event emitter pattern if available
          mqttClient.on('message', messageHandler);
        } else {
          console.warn(`[VU ${vuID}] MQTT client does not support message handlers - subscriptions may not work`);
        }

        // Mark subscriptions as ready (give a small sleep for subscriptions to complete)
        sleep(0.5);
        deviceContext.subscriptionsReady = true;

      } catch (e) {
        console.error(`[VU ${vuID}] Subscription error: ${e}`);
      }
      
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
  
  // Logic: Send initial authorization request after subscriptions are ready
  if (deviceContext.subscriptionsReady && !deviceContext.initialAuthSent) {
    const authPayload = JSON.stringify({
      device_id: deviceContext.id,
      request_id: generateID(),
      request_msat: AUTHORIZE_REQUEST_MSAT,
      reason: 'STARTUP',
      timestamp: getISOTimestamp(),
    });
    
    try {
      mqttClient.publish(`/devices/${deviceContext.id}/request/authorize`, authPayload, { qos: 1 });
      mqttPublishSuccess.add(1);
      mqttPublishRate.add(1);
      deviceContext.initialAuthSent = true;
      deviceContext.lastAuthorizeRequest = now;
      console.log(`[VU ${__VU}] Initial authorization request sent: ${AUTHORIZE_REQUEST_MSAT} msat`);
    } catch (e) {
      mqttPublishFailure.add(1);
      mqttPublishRate.add(0);
      console.error(`[VU ${__VU}] Initial authorization request failed: ${e}`);
    }
  }
  
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

  // Logic: Usage Report (only if reporting is enabled)
  if (deviceContext.reportingEnabled) {
    const timeSinceLastReport = now - deviceContext.lastUsageReport;
    const shouldReport = timeSinceLastReport >= USAGE_REPORT_INTERVAL * 1000;
    
    // Debug: always log the first check, then every 5 seconds
    const debugLog = (deviceContext.lastUsageReport === 0 || now % 5000 < 100);
    if (debugLog || shouldReport) {
      console.log(`[VU ${__VU}] Usage check: enabled=true, timeSince=${timeSinceLastReport}ms, interval=${USAGE_REPORT_INTERVAL * 1000}ms, shouldReport=${shouldReport}, lastReport=${deviceContext.lastUsageReport}, now=${now}`);
    }
    
    if (shouldReport) {
      const measure = randomIntBetween(0, 100) / 1000.0;
      const payload = JSON.stringify({
        device_id: deviceContext.id,
        report_id: generateID(),
        strategy: 1,
        measure: measure,
        unit: 'kWh',
        timestamp: getISOTimestamp(),
      });

      try {
          mqttClient.publish(`/devices/${deviceContext.id}/usage`, payload, { qos: 1 });
          mqttPublishSuccess.add(1);
          mqttPublishRate.add(1);
          deviceContext.lastUsageReport = now;
          console.log(`[VU ${__VU}] Usage report sent: ${measure} kWh`);
      } catch(e) {
          mqttPublishFailure.add(1);
          mqttPublishRate.add(0);
          console.error(`[VU ${__VU}] Usage report failed: ${e}`);
      }
    }
  }

  // Logic: Invoice Request (to add funds after authorization is rejected)
  // Request invoice only if authorization was rejected and enough time has passed since last request
  if (deviceContext.subscriptionsReady && 
      deviceContext.initialAuthSent &&
      deviceContext.authorizationStatus === 'REJECTED' && 
      now - deviceContext.lastInvoiceRequest >= INVOICE_REQUEST_INTERVAL * 1000) {
    const payload = JSON.stringify({
      device_id: deviceContext.id,
      request_id: generateID(),
      amount_msat: INVOICE_AMOUNT_MSAT,
      reason: 'USER_TOPUP',
      timestamp: getISOTimestamp(),
    });

    try {
        mqttClient.publish(`/devices/${deviceContext.id}/request/invoice`, payload, { qos: 1 });
        mqttPublishSuccess.add(1);
        mqttPublishRate.add(1);
        console.log(`[VU ${__VU}] Invoice request sent: ${INVOICE_AMOUNT_MSAT} msat (authorization was rejected)`);
        // Reset authorization status so we can retry after invoice is paid
        deviceContext.authorizationStatus = null;
    } catch(e) {
        mqttPublishFailure.add(1);
        mqttPublishRate.add(0);
        console.error(`[VU ${__VU}] Invoice request failed: ${e}`);
    }
    deviceContext.lastInvoiceRequest = now;
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