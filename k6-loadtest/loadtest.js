import { Counter, Rate } from 'k6/metrics';
import http from 'k6/http';
import { randomString } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';
import { sleep } from 'k6';

// --- Metrics ---
const httpRequestSuccess = new Counter('http_request_success');
const httpRequestFailure = new Counter('http_request_failure');
const httpRequestRate = new Rate('http_request_rate');

// --- Configuration ---
export const options = {
  setupTimeout: '10m', // Allow up to 10 minutes for setup (device pre-registration)
  scenarios: {
    devices: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '1m', target: 5 },   // warmup
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

const API_BASE_URL = __ENV.API_BASE_URL || 'http://localhost:8080';
const API_DEVICES_BATCH_ENDPOINT = __ENV.API_DEVICES_BATCH_ENDPOINT || '/devices/batch';
const BRIDGE_BASE_URL = __ENV.BRIDGE_BASE_URL || 'http://localhost:3000';
const MQTT_BROKER = __ENV.MQTT_BROKER || 'ssl://localhost:8883';
const USAGE_REPORT_INTERVAL = parseInt(__ENV.USAGE_REPORT_INTERVAL || '1'); // seconds between reports
const UNIT_PRICE_MSAT = parseInt(__ENV.UNIT_PRICE_MSAT || '100');
const AUTHORIZE_REQUEST_MSAT = parseInt(__ENV.AUTHORIZE_REQUEST_MSAT || '10000');
const MAX_VUS = parseInt(__ENV.MAX_VUS || '5');

// --- Helpers ---
function generateDeviceID(vuID) {
  // Match the pattern used in setup: k6_device_{id} with 6-digit padding
  return `k6_device_${String(vuID).padStart(6, '0')}`;
}

function generateID() {
  return randomString(16, 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789');
}

function getISOTimestamp() {
  return new Date().toISOString();
}

// --- Setup ---
export function setup() {
  console.log(`Starting load test setup: pre-registering and connecting ${MAX_VUS} devices...`);

  // Step 1: Register all devices using batch endpoint
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
    heartbeat_interval: 60,
    authorize_request_msat: AUTHORIZE_REQUEST_MSAT,
    timestamp: getISOTimestamp(),
  });

  const batchRes = http.post(
    `${API_BASE_URL}${API_DEVICES_BATCH_ENDPOINT}`,
    batchPayload,
    { headers: { 'Content-Type': 'application/json' } }
  );

  let registered = 0;
  if (batchRes.status === 204) {
    console.log(`Batch already exists (204 No Content) - all ${MAX_VUS} devices are already registered`);
    registered = MAX_VUS;
  } else if (batchRes.status === 201) {
    const response = JSON.parse(batchRes.body);
    console.log(`Batch creation successful: ${response.devices_created} devices created (range: ${response.id_range})`);
    registered = response.devices_created;
  } else {
    console.error(`Failed to register device batch: ${batchRes.status} - ${batchRes.body}`);
    return {
      registered: 0,
      skipped: 0,
      failed: MAX_VUS,
      total: MAX_VUS,
      connected: 0,
      deviceIDs: [],
    };
  }

  // Step 2: Connect all devices to the bridge (initialize: invoice + authorization)
  console.log(`Connecting ${MAX_VUS} devices to bridge...`);
  const deviceIDs = [];
  let connected = 0;
  let failed = 0;

  // Connect devices sequentially (each connection waits for invoice + authorization)
  for (let id = 1; id <= MAX_VUS; id++) {
    const deviceID = `k6_device_${String(id).padStart(6, '0')}`;
    const deviceSecret = `${deviceID}_password`;
    deviceIDs.push(deviceID);

    const payload = JSON.stringify({
      broker: MQTT_BROKER,
      secret: deviceSecret,
    });

    const res = http.post(
      `${BRIDGE_BASE_URL}/devices/${deviceID}/connect`,
      payload,
      {
        headers: { 'Content-Type': 'application/json' },
        timeout: '120s', // Allow time for invoice + authorization
      }
    );

    if (res.status === 200) {
      connected++;
      if (connected % 10 === 0 || connected === MAX_VUS) {
        console.log(`Connected ${connected}/${MAX_VUS} devices...`);
      }
    } else {
      failed++;
      console.error(`Failed to connect ${deviceID}: ${res.status} - ${res.body}`);
    }
  }

  console.log(`Setup complete: ${registered} registered, ${connected} connected, ${failed} failed`);
  return {
    registered,
    skipped: 0,
    failed,
    total: MAX_VUS,
    connected,
    deviceIDs,
  };
}

// --- Main VU Function ---
export default function () {
  const vuID = __VU;
  const deviceID = generateDeviceID(vuID);

  // Device should already be connected from setup
  // Just send usage reports

  // Simulate device sending usage reports with delays
  // The bridge handles all the MQTT logic, authorization maintenance, etc.
  // We just need to send usage reports via HTTP to the bridge
  // This loop will run for the duration of the test scenario

  let reportCount = 0;

  // Keep sending reports until the test ends (k6 will stop calling this function when scenario ends)
  // Generate a random measurement between 0.1 and 1.0 kWh
  const measure = 0.1 + Math.random() * 0.9;
  const usagePayload = JSON.stringify({
    deviceId: deviceID,
    reportId: generateID(),
    strategy: 1,
    measure: measure,
    unit: 'kWh',
    timestamp: getISOTimestamp(),
  });

  // Send usage report via bridge
  const usageRes = http.post(
    `${BRIDGE_BASE_URL}/devices/${deviceID}/usage`,
    usagePayload,
    { headers: { 'Content-Type': 'application/json' } }
  );

  if (usageRes.status === 200) {
    httpRequestSuccess.add(1);
    httpRequestRate.add(1);
    reportCount++;
    if (reportCount % 10 === 0) {
      console.log(`[VU ${vuID}] Sent ${reportCount} usage reports`);
    }
  } else {
    httpRequestFailure.add(1);
    console.error(`[VU ${vuID}] Usage report failed: ${usageRes.status} - ${usageRes.body}`);
  }

  // Sleep for the configured interval with jitter before sending next report
  // Add random jitter (±20% of interval) to desynchronize devices and simulate realistic load
  // This prevents all devices from reporting at exactly the same time
  // const jitter = (Math.random() * 0.4 - 0.2) * USAGE_REPORT_INTERVAL; // ±20% jitter
  // const sleepDuration = Math.max(0.1, USAGE_REPORT_INTERVAL + jitter); // Ensure minimum 0.1s
  // sleep(sleepDuration);
}

// --- Teardown ---
export function teardown(data) {
  // console.log("Disconnecting all devices...");

  // const deviceIDs = data?.deviceIDs || [];
  // let disconnected = 0;
  // let failed = 0;

  // // Disconnect all devices
  // if (deviceIDs.length > 0) {
  //   // Disconnect sequentially
  //   for (const deviceID of deviceIDs) {
  //     const res = http.post(
  //       `${BRIDGE_BASE_URL}/devices/${deviceID}/disconnect`,
  //       '',
  //       { timeout: '10s' }
  //     );
  //     if (res.status === 200) {
  //       disconnected++;
  //     } else if (res.status !== 404) { // 404 is OK, device wasn't connected
  //       failed++;
  //     }
  //   }
  // } else {
  //   // Fallback: try to disconnect devices 1 to MAX_VUS
  //   console.log("No device IDs in data, attempting to disconnect all devices...");
  //   for (let id = 1; id <= MAX_VUS; id++) {
  //     const deviceID = `k6_device_${String(id).padStart(6, '0')}`;
  //     const res = http.post(
  //       `${BRIDGE_BASE_URL}/devices/${deviceID}/disconnect`,
  //       '',
  //       { timeout: '10s' }
  //     );
  //     if (res.status === 200) {
  //       disconnected++;
  //     } else if (res.status !== 404) { // 404 is OK, device wasn't connected
  //       failed++;
  //     }
  //   }
  // }

  // console.log(`Teardown complete: ${disconnected} disconnected, ${failed} failed`);
  console.log("Load test finished.");
}
