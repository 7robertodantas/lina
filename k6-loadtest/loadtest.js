import { Client } from 'k6/x/mqtt';
import { Counter, Rate } from 'k6/metrics';
import http from 'k6/http';
import { randomString } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';




// --- Metrics ---
const mqttPublishSuccess = new Counter('mqtt_publish_success');
const mqttPublishFailure = new Counter('mqtt_publish_failure');
const mqttPublishRate = new Rate('mqtt_publish_rate');

// --- Configuration ---
export const options = {
  setupTimeout: '10m', // Allow up to 10 minutes for setup (device pre-registration)
  scenarios: {
    devices: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '1m', target: 10 },   // warmup
        // { duration: '1m', target: 5000 },
        // { duration: '1m', target: 10000 },
        // { duration: '1m', target: 20000 },
        // { duration: '1m', target: 40000 },
        // { duration: '1m', target: 60000 },
        // { duration: '1m', target: 80000 },
        // { duration: '1m', target: 100000 }, // peak
        // { duration: '5m', target: 100000 },// plateau at max
        { duration: '1m', target: 0 },      // ramp down
      ],
      gracefulRampDown: '2m',
    },
  },
};
// export const options = {
//   stages: [
//     { duration: '10s', target: 5 },   
//     { duration: '10s', target: 5 },
//     { duration: '10s', target: 0 },
//     // { duration: '30s', target: 100 },   // Ramp up to 100 devices
//     // { duration: '2m', target: 100 },    // Stay at 100 devices
//     // { duration: '30s', target: 500 },  // Ramp up to 500 devices
//     // { duration: '2m', target: 500 },    // Stay at 500 devices
//     // { duration: '30s', target: 1000 },   // Ramp up to 1000 devices
//     // { duration: '5m', target: 1000 },   // Stay at 1000 devices (stress test)
//     // { duration: '30s', target: 0 },    // Ramp down
//   ],
//   thresholds: {
//     'mqtt_publish_rate': ['rate>0.95'], 
//   },
// };

const API_BASE_URL = __ENV.API_BASE_URL || 'http://localhost:8080';
const API_DEVICES_ENDPOINT = __ENV.API_DEVICES_ENDPOINT || '/devices';
const API_DEVICES_BATCH_ENDPOINT = __ENV.API_DEVICES_BATCH_ENDPOINT || '/devices/batch';
const MQTT_BROKER = __ENV.MQTT_BROKER || 'localhost';
const MQTT_TLS_PORT = parseInt(__ENV.MQTT_TLS_PORT || '8883');
const HEARTBEAT_INTERVAL = parseInt(__ENV.HEARTBEAT_INTERVAL || '60'); 
const USAGE_REPORT_INTERVAL = parseInt(__ENV.USAGE_REPORT_INTERVAL || '1'); 
const UNIT_PRICE_MSAT = parseInt(__ENV.UNIT_PRICE_MSAT || '100');
const AUTHORIZE_REQUEST_MSAT = parseInt(__ENV.AUTHORIZE_REQUEST_MSAT || '10000');
const INVOICE_REQUEST_INTERVAL = parseInt(__ENV.INVOICE_REQUEST_INTERVAL || '5'); 
const INVOICE_AMOUNT_MSAT = parseInt(__ENV.INVOICE_AMOUNT_MSAT || '250000');
const MAX_VUS = parseInt(__ENV.MAX_VUS || '10'); 

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

// --- Setup ---
export function setup() {
  console.log(`Starting load test setup: pre-registering ${MAX_VUS} devices using batch endpoint...`);
  
  // Use batch endpoint to register all devices at once
  const batchPayload = JSON.stringify({
    device_id_pattern: 'k6_device_{id}',
    device_secret_pattern: 'k6_device_{id}_password',
    id_start: 1,
    id_end: MAX_VUS,
    id_padding: 6,
    measurement_unit: 'kWh',
    unit_price_msat: UNIT_PRICE_MSAT,
    reporting_strategy: 'interval',
    reporting_interval: USAGE_REPORT_INTERVAL,
    heartbeat_interval: HEARTBEAT_INTERVAL,
    authorize_request_msat: AUTHORIZE_REQUEST_MSAT,
    timestamp: getISOTimestamp(),
  });
  
  const batchRes = http.post(
    `${API_BASE_URL}${API_DEVICES_BATCH_ENDPOINT}`,
    batchPayload,
    { headers: { 'Content-Type': 'application/json' } }
  );
  
  if (batchRes.status === 204) {
    console.log(`Batch already exists (204 No Content) - all ${MAX_VUS} devices are already registered`);
    return {
      registered: 0,
      skipped: MAX_VUS,
      failed: 0,
      total: MAX_VUS,
    };
  } else if (batchRes.status === 201) {
    const response = JSON.parse(batchRes.body);
    console.log(`Batch creation successful: ${response.devices_created} devices created (range: ${response.id_range})`);
    return {
      registered: response.devices_created,
      skipped: 0,
      failed: 0,
      total: MAX_VUS,
    };
  } else {
    console.error(`Failed to register device batch: ${batchRes.status} - ${batchRes.body}`);
    return {
      registered: 0,
      skipped: 0,
      failed: MAX_VUS,
      total: MAX_VUS,
    };
  }
}

// --- Main VU Function (Event-Driven MQTT Pattern) ---
export default function () {
  const vuID = __VU;
  const deviceID = generateDeviceID(vuID);
  const deviceSecret = `${deviceID}_password`;

  // Device context stored in closure (accessible to all event handlers)
  const deviceContext = {
    id: deviceID,
    secret: deviceSecret,
    availableMsat: 0,
    reportingEnabled: false,
    subscriptionsReady: false,
    authorizationStatus: null,
    authorizationId: null,
    authorizationExpiresAt: null,
    authorizationGrantedMsat: null,
    authorizationRemainingMsat: null,
    authorizationIssuedAt: null,
    pendingAuthorization: false,
    lastInvoiceRequest: Date.now() - (INVOICE_REQUEST_INTERVAL * 1000),
    lastAuthorizationRequest: Date.now() - (INVOICE_REQUEST_INTERVAL * 1000), // Track last auth request separately
    lastUsageReport: Date.now() - (USAGE_REPORT_INTERVAL * 1000),
  };

  // Device should already be registered in setup()
  // Skip registration during load test to avoid testing registration endpoint
  
  // 1. Create MQTT Client
  const brokerURL = `ssl://${MQTT_BROKER}:${MQTT_TLS_PORT}`;
  
  const client = new Client({
    // Using deviceID for client_id ensures uniqueness per VU, which is sufficient if each VU/device is only ever connected once at a time.
    client_id: deviceID,
    username: deviceID,
    password: deviceSecret,
    clean: true,
    connectTimeout: 10,
    reconnectPeriod: 5,
  });

  // Topic definitions
  const balanceTopic = `/devices/${deviceID}/balance`;
  const controlTopic = `/devices/${deviceID}/control`;
  const authorizeResponseTopic = `/devices/${deviceID}/response/authorize`;

  // 3. Set up event handlers
  client.on('connect', () => {
    console.log(`[VU ${vuID}] Connected to MQTT broker`);

    // Subscribe to topics
    client.subscribe(balanceTopic);
    client.subscribe(controlTopic);
    client.subscribe(authorizeResponseTopic);
    
    deviceContext.subscriptionsReady = true;
    console.log(`[VU ${vuID}] Subscribed to topics: ${balanceTopic}, ${controlTopic}, ${authorizeResponseTopic}`);

    // Send initial authorization request
    sendAuthorizationRequest(vuID, client, deviceContext, 'STARTUP');

    // Set up heartbeat interval
    setInterval(() => {
      const payload = JSON.stringify({
        deviceId: deviceContext.id,
        status: 1,
        timestamp: getISOTimestamp(),
      });

      try {
        client.publish(`/devices/${deviceContext.id}/heartbeat`, payload, { qos: 1 });
        mqttPublishSuccess.add(1);
        mqttPublishRate.add(1);
      } catch (e) {
        mqttPublishFailure.add(1);
        console.error(`[VU ${vuID}] Heartbeat failed: ${e}`);
      }
    }, HEARTBEAT_INTERVAL * 1000);

    // Set up usage reporting interval with randomization to desynchronize VUs
    // Add random offset (0-100% of interval) so VUs don't all report at the same time
    const randomOffset = Math.random() * USAGE_REPORT_INTERVAL * 1000;
    let lastUsageReport = Date.now() - (USAGE_REPORT_INTERVAL * 1000) + randomOffset;
    
    // Use a shorter check interval but add jitter to prevent synchronization
    const checkInterval = 50 + Math.random() * 50; // 50-100ms random interval
    
    setInterval(() => {
      if (!deviceContext.reportingEnabled) {
        return;
      }

      const now = Date.now();
      const timeSinceLastReport = now - lastUsageReport;
      const reportIntervalMs = USAGE_REPORT_INTERVAL * 1000;
      
      // Check if we should report (either interval elapsed or immediate trigger)
      const shouldReport = (deviceContext.lastUsageReport === 0) || (timeSinceLastReport >= reportIntervalMs);
      
      if (shouldReport) {
        // Random measure between 0.1 and 1.0
        const measure = 0.1 + Math.random() * 0.9;
        const payload = JSON.stringify({
          deviceId: deviceContext.id,
          reportId: generateID(),
          strategy: 1,
          measure: measure,
          unit: 'kWh',
          timestamp: getISOTimestamp(),
        });

        // Publish (QoS 0 is fire-and-forget but still blocking in k6)
        // With randomization, VUs should be desynchronized and publish concurrently
        const publishStart = Date.now();
        try {
          client.publish(`/devices/${deviceContext.id}/usage`, payload, { qos: 0 });
          const publishDuration = Date.now() - publishStart;
          mqttPublishSuccess.add(1);
          mqttPublishRate.add(1);
          lastUsageReport = now;
          deviceContext.lastUsageReport = now; // Update context for immediate trigger logic
          console.log(`[VU ${vuID}] Usage report sent: ${measure} kWh (publish took ${publishDuration}ms)`);
        } catch (e) {
          mqttPublishFailure.add(1);
          mqttPublishRate.add(0);
          console.error(`[VU ${vuID}] Usage report failed: ${e}`);
        }
      }
    }, checkInterval); // Random interval 50-100ms to prevent synchronization
  });

  client.on('message', (topic, message) => {
    try {
      // Robust string conversion
      const msgStr = (typeof message === 'string')
        ? message
        : String.fromCharCode.apply(null, new Uint8Array(message));

      const payload = JSON.parse(msgStr);

      if (topic === balanceTopic) {
        handleBalanceMessage(vuID, client, deviceContext, payload);
      } else if (topic === controlTopic) {
        handleControlMessage(vuID, client, deviceContext, payload);
      } else if (topic === authorizeResponseTopic) {
        handleAuthorizationResponse(vuID, client, deviceContext, payload);
      } else {
        console.log(`[VU ${vuID}] Unknown topic: ${topic}`);
      }
    } catch (e) {
      console.error(`[VU ${vuID}] Message handler error: ${e} | Topic: ${topic}`);
    }
  });

  client.on('end', () => {
    console.log(`[VU ${vuID}] Disconnected from MQTT broker`);
  });

  client.on('error', (err) => {
    console.error(`[VU ${vuID}] MQTT error: ${err}`);
  });

  // 4. Connect to broker (this blocks until connection closes)
  try {
    client.connect(brokerURL);
  } catch (e) {
    console.error(`[VU ${vuID}] MQTT connect error: ${e}`);
  }

  // Note: Function returns here, but VU stays alive running the MQTT event loop
}

// --- Message Handlers ---
function sendAuthorizationRequest(vuID, client, deviceContext, reason) {
  if (deviceContext.pendingAuthorization) {
    console.log(`[VU ${vuID}] Auth request skipped - already pending`);
    return;
  }

  const authPayload = JSON.stringify({
    deviceId: deviceContext.id,
    requestId: generateID(),
    requestMsat: AUTHORIZE_REQUEST_MSAT,
    reason: reason,
    timestamp: getISOTimestamp(),
  });

  try {
    client.publish(`/devices/${deviceContext.id}/request/authorize`, authPayload, { qos: 1 });
    mqttPublishSuccess.add(1);
    mqttPublishRate.add(1);
    deviceContext.pendingAuthorization = true;
    deviceContext.lastAuthorizationRequest = Date.now();
    console.log(`[VU ${vuID}] Authorization request sent: ${reason}`);
  } catch (e) {
    mqttPublishFailure.add(1);
    console.error(`[VU ${vuID}] Auth request failed: ${e}`);
  }
}

function sendInvoiceRequest(vuID, client, deviceContext, reason) {
  const payload = JSON.stringify({
    deviceId: deviceContext.id,
    requestId: generateID(),
    amountMsat: INVOICE_AMOUNT_MSAT,
    reason: reason,
    timestamp: getISOTimestamp(),
  });

  try {
    client.publish(`/devices/${deviceContext.id}/request/invoice`, payload, { qos: 1 });
    mqttPublishSuccess.add(1);
    mqttPublishRate.add(1);
    deviceContext.lastInvoiceRequest = Date.now();
    console.log(`[VU ${vuID}] Invoice request sent: ${reason}`);
  } catch (e) {
    mqttPublishFailure.add(1);
    console.error(`[VU ${vuID}] Invoice request failed: ${e}`);
  }
}

function handleBalanceMessage(vuID, client, deviceContext, payload) {
  if (payload && payload.available_msat !== undefined) {
    const availableMsat = typeof payload.available_msat === 'string' 
      ? parseInt(payload.available_msat, 10) 
      : parseInt(payload.available_msat) || 0;
    
    deviceContext.availableMsat = availableMsat;
    console.log(`[VU ${vuID}] Balance updated: ${deviceContext.availableMsat}`);
  }
}

function handleAuthorizationResponse(vuID, client, deviceContext, payload) {
  if (!payload || !payload.status) {
    return;
  }

  const status = payload.status;
  console.log(`[VU ${vuID}] Authorization status received: ${status}`);
  
  if (status === 'AUTHORIZATION_STATUS_GRANTED' || status === 'AUTHORIZATION_STATUS_ACTIVE') {
    // Store authorization details
    deviceContext.authorizationStatus = status;
    deviceContext.authorizationId = payload.authorization_id || null;
    deviceContext.authorizationExpiresAt = payload.expires_at || null;
    deviceContext.authorizationGrantedMsat = payload.granted_msat || null;
    deviceContext.authorizationRemainingMsat = payload.remaining_msat || null;
    deviceContext.authorizationIssuedAt = payload.issued_at || null;
    deviceContext.pendingAuthorization = false;
    deviceContext.reportingEnabled = true;
    deviceContext.lastUsageReport = 0; // Reset to 0 to trigger immediate report
    
    const statusText = status === 'AUTHORIZATION_STATUS_GRANTED' ? 'GRANTED' : 'ACTIVE';
    console.log(`[VU ${vuID}] Authorization ${statusText} - reporting ENABLED`, {
      authorizationId: deviceContext.authorizationId,
      expiresAt: deviceContext.authorizationExpiresAt,
      grantedMsat: deviceContext.authorizationGrantedMsat,
      remainingMsat: deviceContext.authorizationRemainingMsat,
    });
  } else if (status === 'AUTHORIZATION_STATUS_REJECTED') {
    // Clear authorization details on rejection
    deviceContext.pendingAuthorization = false;
    deviceContext.authorizationStatus = 'REJECTED';
    deviceContext.authorizationId = null;
    deviceContext.authorizationExpiresAt = null;
    deviceContext.authorizationGrantedMsat = null;
    deviceContext.authorizationRemainingMsat = null;
    deviceContext.authorizationIssuedAt = null;
    console.log(`[VU ${vuID}] Authorization REJECTED: ${payload.reason || 'Unknown'}`);
    // Send invoice request to add funds when authorization is rejected
    sendInvoiceRequest(vuID, client, deviceContext, 'AUTHORIZATION_REJECTED_NEED_FUNDS');
  } else {
    console.log(`[VU ${vuID}] Unknown authorization status: ${status}`);
  }
}

// Check if current authorization is valid (exists, not rejected, not expired)
function isAuthorizationValid(deviceContext) {
  // No authorization set
  if (!deviceContext.authorizationStatus || 
      deviceContext.authorizationStatus === null) {
    return false;
  }
  
  // Authorization is rejected
  if (deviceContext.authorizationStatus === 'REJECTED') {
    return false;
  }
  
  // Check if expired
  if (deviceContext.authorizationExpiresAt) {
    const expiresAt = new Date(deviceContext.authorizationExpiresAt);
    const now = new Date();
    if (now >= expiresAt) {
      return false; // Expired
    }
  }
  
  // Authorization is valid (GRANTED or ACTIVE and not expired)
  return true;
}

function handleControlMessage(vuID, client, deviceContext, payload) {
  if (!payload || !payload.command) {
    return;
  }

  const command = payload.command;
  console.log(`[VU ${vuID}] Control command received: ${command}`);
  
  if (command === 'CONTROL_COMMAND_STOP' || command === 'CONTROL_COMMAND_PAUSE') {
    deviceContext.reportingEnabled = false;
    console.log(`[VU ${vuID}] STOP/PAUSE received - reporting DISABLED`);
  } else if (command === 'CONTROL_COMMAND_RESUME') {
    deviceContext.reportingEnabled = true;
    deviceContext.lastUsageReport = 0; // Reset to 0 to trigger immediate report
    console.log(`[VU ${vuID}] RESUME received - reporting ENABLED`);
    
    // Only send authorization request if current authorization is not valid
    if (!isAuthorizationValid(deviceContext)) {
      const reason = !deviceContext.authorizationStatus 
        ? 'RESUME_NO_AUTHORIZATION' 
        : deviceContext.authorizationStatus === 'REJECTED' 
          ? 'RESUME_AUTHORIZATION_REJECTED'
          : 'RESUME_AUTHORIZATION_EXPIRED';
      sendAuthorizationRequest(vuID, client, deviceContext, reason);
    } else {
      console.log(`[VU ${vuID}] RESUME - authorization already valid, skipping request`, {
        authorizationId: deviceContext.authorizationId,
        status: deviceContext.authorizationStatus,
        expiresAt: deviceContext.authorizationExpiresAt,
      });
    }
  } else if (command === 'CONTROL_COMMAND_AUTHORIZATION') {
    // Manually trigger auth request
    sendAuthorizationRequest(vuID, client, deviceContext, payload.reason || 'AUTHORIZATION_REQUIRED');
  } else {
    console.log(`[VU ${vuID}] Unknown control command: ${command}`);
  }
}

// --- Teardown ---
export function teardown() {
  console.log("Load test finished.");
}
