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
    const authPayload = JSON.stringify({
      deviceId: deviceContext.id,
      requestId: generateID(),
      requestMsat: AUTHORIZE_REQUEST_MSAT,
      reason: 'STARTUP',
      timestamp: getISOTimestamp(),
    });

    try {
      client.publish(`/devices/${deviceContext.id}/request/authorize`, authPayload, { qos: 1 });
      mqttPublishSuccess.add(1);
      mqttPublishRate.add(1);
      deviceContext.pendingAuthorization = true;
      deviceContext.lastAuthorizationRequest = Date.now(); // Update last auth request time
      console.log(`[VU ${vuID}] Initial authorization request sent`);
    } catch (e) {
      mqttPublishFailure.add(1);
      console.error(`[VU ${vuID}] Initial auth request failed: ${e}`);
    }

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

    // Set up usage reporting interval
    let lastUsageReport = Date.now() - (USAGE_REPORT_INTERVAL * 1000);
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

        try {
          client.publish(`/devices/${deviceContext.id}/usage`, payload, { qos: 0 });
          mqttPublishSuccess.add(1);
          mqttPublishRate.add(1);
          lastUsageReport = now;
          deviceContext.lastUsageReport = now; // Update context for immediate trigger logic
          console.log(`[VU ${vuID}] Usage report sent: ${measure} kWh`);
        } catch (e) {
          mqttPublishFailure.add(1);
          mqttPublishRate.add(0);
          console.error(`[VU ${vuID}] Usage report failed: ${e}`);
        }
      }
    }, 100); // Check every 100ms for more responsive reporting

    // Set up invoice retry interval (for REJECTED auth recovery)
    setInterval(() => {
      if (deviceContext.authorizationStatus !== 'REJECTED') {
        return;
      }

      const now = Date.now();
      if (now - deviceContext.lastInvoiceRequest >= INVOICE_REQUEST_INTERVAL * 1000) {
        const payload = JSON.stringify({
          deviceId: deviceContext.id,
          requestId: generateID(),
          amountMsat: INVOICE_AMOUNT_MSAT,
          reason: 'USER_TOPUP',
          timestamp: getISOTimestamp(),
        });

        try {
          client.publish(`/devices/${deviceContext.id}/request/invoice`, payload, { qos: 0 });
          mqttPublishSuccess.add(1);
          mqttPublishRate.add(1);
          deviceContext.lastInvoiceRequest = now;
          deviceContext.authorizationStatus = null; // Clear status to allow balance update to trigger new auth
          console.log(`[VU ${vuID}] Invoice request sent (REJECTED recovery)`);
        } catch (e) {
          mqttPublishFailure.add(1);
          console.error(`[VU ${vuID}] Invoice request failed: ${e}`);
        }
      }
    }, 1000); // Check every second
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
        handleAuthorizationResponse(vuID, deviceContext, payload);
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
function handleBalanceMessage(vuID, client, deviceContext, payload) {
  if (payload && payload.available_msat !== undefined) {
    const availableMsat = typeof payload.available_msat === 'string' 
      ? parseInt(payload.available_msat, 10) 
      : parseInt(payload.available_msat) || 0;
    
    // Capture previous balance before updating
    const previousBalance = deviceContext.availableMsat || 0;
    deviceContext.availableMsat = availableMsat;
    console.log(`[VU ${vuID}] Balance updated: ${deviceContext.availableMsat}`);
    
    // Auto-request auth if we have funds but no auth
    const now = Date.now();
    const timeSinceLastAuth = now - deviceContext.lastAuthorizationRequest;
    
    // Check individual conditions for debugging
    const hasFunds = availableMsat > 0;
    const notGranted = deviceContext.authorizationStatus !== 'GRANTED';
    const notActive = deviceContext.authorizationStatus !== 'ACTIVE';
    const notPending = !deviceContext.pendingAuthorization;
    const enoughTimePassed = timeSinceLastAuth >= INVOICE_REQUEST_INTERVAL * 1000;
    // Allow immediate request if we just got funds (previous balance was 0) or if enough time has passed
    const justGotFunds = previousBalance === 0 && availableMsat > 0;
    const canRequestNow = justGotFunds || enoughTimePassed;
    
    const shouldRequestAuth = hasFunds && 
        notGranted && 
        notActive &&
        notPending &&
        canRequestNow;
    
    // Debug logging to see why auth request might not be triggered
    if (!shouldRequestAuth) {
      console.log(`[VU ${vuID}] Auth request NOT triggered - hasFunds: ${hasFunds}, notGranted: ${notGranted}, notActive: ${notActive}, notPending: ${notPending}, canRequestNow: ${canRequestNow} (justGotFunds: ${justGotFunds}, enoughTimePassed: ${enoughTimePassed} (${timeSinceLastAuth}ms >= ${INVOICE_REQUEST_INTERVAL * 1000}ms)), authStatus: ${deviceContext.authorizationStatus}`);
    }
    
    if (shouldRequestAuth) {
      const authPayload = JSON.stringify({
        deviceId: deviceContext.id,
        requestId: generateID(),
        requestMsat: AUTHORIZE_REQUEST_MSAT,
        reason: 'FUNDS_AVAILABLE',
        timestamp: getISOTimestamp(),
      });
      
      try {
        client.publish(`/devices/${deviceContext.id}/request/authorize`, authPayload, { qos: 0 });
        mqttPublishSuccess.add(1);
        mqttPublishRate.add(1);
        deviceContext.pendingAuthorization = true;
        deviceContext.lastAuthorizationRequest = now; // Update last auth request time
        console.log(`[VU ${vuID}] Auth request sent (balance update)`);
      } catch (e) {
        console.error(`[VU ${vuID}] Auth request failed: ${e}`);
      }
    }
  }
}

function handleAuthorizationResponse(vuID, deviceContext, payload) {
  if (!payload || !payload.status) {
    return;
  }

  const status = payload.status;
  console.log(`[VU ${vuID}] Authorization status received: ${status}`);
  
  if (status === 'AUTHORIZATION_STATUS_GRANTED') {
    deviceContext.authorizationStatus = 'GRANTED';
    deviceContext.pendingAuthorization = false;
    deviceContext.reportingEnabled = true;
    deviceContext.lastUsageReport = 0; // Reset to 0 to trigger immediate report
    console.log(`[VU ${vuID}] Authorization GRANTED - reporting ENABLED`);
  } else if (status === 'AUTHORIZATION_STATUS_ACTIVE') {
    deviceContext.authorizationStatus = 'ACTIVE';
    deviceContext.pendingAuthorization = false;
    deviceContext.reportingEnabled = true;
    deviceContext.lastUsageReport = 0; // Reset to 0 to trigger immediate report
    console.log(`[VU ${vuID}] Authorization ACTIVE - reporting ENABLED`);
  } else if (status === 'AUTHORIZATION_STATUS_REJECTED') {
    deviceContext.pendingAuthorization = false;
    deviceContext.authorizationStatus = 'REJECTED';
    console.log(`[VU ${vuID}] Authorization REJECTED: ${payload.reason || 'Unknown'}`);
  } else {
    console.log(`[VU ${vuID}] Unknown authorization status: ${status}`);
  }
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
  } else if (command === 'CONTROL_COMMAND_AUTHORIZATION') {
    // Manually trigger auth request
    const authPayload = JSON.stringify({
      deviceId: deviceContext.id,
      requestId: generateID(),
      requestMsat: AUTHORIZE_REQUEST_MSAT,
      reason: payload.reason || 'AUTHORIZATION_REQUIRED',
      timestamp: getISOTimestamp(),
    });
    try {
      client.publish(`/devices/${deviceContext.id}/request/authorize`, authPayload, { qos: 0 });
      mqttPublishSuccess.add(1);
      mqttPublishRate.add(1);
      deviceContext.pendingAuthorization = true;
      deviceContext.lastAuthorizationRequest = Date.now(); // Update last auth request time
      console.log(`[VU ${vuID}] Auth request sent (command)`);
    } catch (e) { 
      console.error(`[VU ${vuID}] Auth request failed (command): ${e}`);
    }
  } else {
    console.log(`[VU ${vuID}] Unknown control command: ${command}`);
  }
}

// --- Teardown ---
export function teardown() {
  console.log("Load test finished.");
}
