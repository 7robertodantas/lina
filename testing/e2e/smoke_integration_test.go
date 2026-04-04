//go:build integration

package e2e

import (
	"fmt"
	"testing"
	"time"

	devicepkg "github.com/robertodantas/lina/testing/device"
)

// TestSmokeProvisionConnectUsage covers NB-001, SB-001, AUTH-001, USE-001 style flow:
// provision via gateway, fund via ledger credit, MQTT connect, one usage report, consumption visible.
func TestSmokeProvisionConnectUsage(t *testing.T) {
	env := LoadEnv(t)
	env.WaitGatewayHealthy(t, 2*time.Minute)

	deviceID := UniqueDeviceID("e2e-smoke")
	secret := deviceID + "_password"
	t.Logf("e2e: test device_id=%s", deviceID)

	env.ProvisionDevice(t, CreateDeviceRequest{
		DeviceID:             deviceID,
		DeviceSecret:         secret,
		MeasurementUnit:      "kWh",
		UnitPriceMsat:        100_000,
		ReportingStrategy:    "interval",
		ReportingInterval:    60,
		HeartbeatInterval:    300,
		AuthorizeRequestMsat: 50_000,
		Timestamp:            time.Now().UTC().Format(time.RFC3339),
	})
	env.CreditDevice(t, deviceID, 500_000, deviceID+"-credit-smoke")

	cb := &RecordingCallback{Logf: t.Logf}
	di := devicepkg.NewDeviceInterface(cb, env.DeviceMQTTConfig(), deviceID)
	di.SetHeartbeatEnabled(false)
	t.Logf("e2e: MQTT connect (broker %s:%d, CA %s)", env.MQTTHost, env.MQTTTLSPort, env.MQTTCACert)
	di.Connect(deviceID, secret)
	defer func() {
		t.Logf("e2e: MQTT disconnect")
		di.Disconnect()
	}()

	WaitUntil(t, 90*time.Second, 400*time.Millisecond, "MQTT + config + authorization", func() bool {
		return di.IsConnected() && di.GetDeviceConfig() != nil && di.HasActiveAuthorization()
	}, DiagMQTT(env, deviceID, di, nil))

	reportID := devicepkg.GenerateID()
	t.Logf("e2e: publishing usage report_id=%s measure_kwh=0.001", reportID)
	di.PublishUsageReport(reportID, 0.001)

	WaitUntil(t, 90*time.Second, 500*time.Millisecond, "consumption recorded", func() bool {
		for _, c := range env.ListConsumptions(t, deviceID, 20) {
			if c.ReportID == reportID {
				return c.DebitMsat >= 1
			}
		}
		return false
	}, DiagMQTT(env, deviceID, di, func() string {
		for _, c := range env.ListConsumptions(t, deviceID, 20) {
			if c.ReportID == reportID {
				return fmt.Sprintf("target report found debit_msat=%d", c.DebitMsat)
			}
		}
		return fmt.Sprintf("looking for report_id=%s", reportID)
	}))
}

// TestRegressionDuplicateReportIdempotent asserts USE-010: same report_id only debits once.
func TestRegressionDuplicateReportIdempotent(t *testing.T) {
	env := LoadEnv(t)
	env.WaitGatewayHealthy(t, 2*time.Minute)

	deviceID := UniqueDeviceID("e2e-idem")
	secret := deviceID + "_password"
	t.Logf("e2e: test device_id=%s", deviceID)

	env.ProvisionDevice(t, CreateDeviceRequest{
		DeviceID:             deviceID,
		DeviceSecret:         secret,
		MeasurementUnit:      "kWh",
		UnitPriceMsat:        1_000,
		ReportingStrategy:    "interval",
		ReportingInterval:    60,
		HeartbeatInterval:    300,
		AuthorizeRequestMsat: 10_000,
		Timestamp:            time.Now().UTC().Format(time.RFC3339),
	})
	env.CreditDevice(t, deviceID, 100_000, deviceID+"-credit-idem")

	cb := &RecordingCallback{Logf: t.Logf}
	di := devicepkg.NewDeviceInterface(cb, env.DeviceMQTTConfig(), deviceID)
	di.SetHeartbeatEnabled(false)
	t.Logf("e2e: MQTT connect")
	di.Connect(deviceID, secret)
	defer di.Disconnect()

	WaitUntil(t, 90*time.Second, 400*time.Millisecond, "authorization", func() bool {
		return di.IsConnected() && di.GetDeviceConfig() != nil && di.HasActiveAuthorization()
	}, DiagMQTT(env, deviceID, di, nil))

	rid := "fixed-report-id-e2e-idem"
	t.Logf("e2e: publish same report_id twice: %s", rid)
	di.PublishUsageReport(rid, 0.5)
	time.Sleep(800 * time.Millisecond)
	di.PublishUsageReport(rid, 0.5)

	WaitUntil(t, 90*time.Second, 500*time.Millisecond, "single consumption row", func() bool {
		n := 0
		for _, c := range env.ListConsumptions(t, deviceID, 50) {
			if c.ReportID == rid {
				n++
			}
		}
		return n == 1
	}, DiagMQTT(env, deviceID, di, func() string {
		n := 0
		for _, c := range env.ListConsumptions(t, deviceID, 50) {
			if c.ReportID == rid {
				n++
			}
		}
		return fmt.Sprintf("rows matching report_id=%q: %d (want 1)", rid, n)
	}))
}

// TestRegressionMinimumOneMsatDebit asserts USE-020 style minimum 1 msat debit.
func TestRegressionMinimumOneMsatDebit(t *testing.T) {
	env := LoadEnv(t)
	env.WaitGatewayHealthy(t, 2*time.Minute)

	deviceID := UniqueDeviceID("e2e-min1")
	secret := deviceID + "_password"
	t.Logf("e2e: test device_id=%s", deviceID)

	env.ProvisionDevice(t, CreateDeviceRequest{
		DeviceID:             deviceID,
		DeviceSecret:         secret,
		MeasurementUnit:      "kWh",
		UnitPriceMsat:        1_000,
		ReportingStrategy:    "interval",
		ReportingInterval:    60,
		HeartbeatInterval:    300,
		AuthorizeRequestMsat: 10_000,
		Timestamp:            time.Now().UTC().Format(time.RFC3339),
	})
	env.CreditDevice(t, deviceID, 50_000, deviceID+"-credit-min")

	cb := &RecordingCallback{Logf: t.Logf}
	di := devicepkg.NewDeviceInterface(cb, env.DeviceMQTTConfig(), deviceID)
	di.SetHeartbeatEnabled(false)
	di.Connect(deviceID, secret)
	defer di.Disconnect()

	WaitUntil(t, 90*time.Second, 400*time.Millisecond, "authorization", func() bool {
		return di.IsConnected() && di.GetDeviceConfig() != nil && di.HasActiveAuthorization()
	}, DiagMQTT(env, deviceID, di, nil))

	rid := devicepkg.GenerateID()
	t.Logf("e2e: tiny usage report_id=%s (expect debit_msat==1)", rid)
	di.PublishUsageReport(rid, 0.0000001)

	WaitUntil(t, 90*time.Second, 500*time.Millisecond, "debit_msat == 1", func() bool {
		for _, c := range env.ListConsumptions(t, deviceID, 20) {
			if c.ReportID == rid {
				return c.DebitMsat == 1
			}
		}
		return false
	}, DiagMQTT(env, deviceID, di, func() string {
		for _, c := range env.ListConsumptions(t, deviceID, 20) {
			if c.ReportID == rid {
				return fmt.Sprintf("found row debit_msat=%d (want 1)", c.DebitMsat)
			}
		}
		return "consumption row not found yet"
	}))
}

// TestRegressionConsumptionRequiresServiceToken checks NB-021 style rejection on mutating/read protected path.
func TestRegressionConsumptionRequiresServiceToken(t *testing.T) {
	env := LoadEnv(t)
	env.WaitGatewayHealthy(t, 2*time.Minute)

	t.Logf("e2e: GET consumptions without X-Service-Token (expect 401)")
	resp := env.GET(t, "/devices/does-not-matter/consumptions?limit=5", nil)
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 without X-Service-Token, got %d", resp.StatusCode)
	}
	t.Logf("e2e: got expected 401")
}
