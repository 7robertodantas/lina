//go:build integration

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	devicepkg "github.com/robertodantas/lina/testing/device"
)

// TestLifecycleExhaustAuthorizationThenStopInsufficientFunds drives STOP-001/STOP-002 style behavior:
// fund enough for one hold, consume the full authorization, automatic re-authorization fails (insufficient funds), STOP.
func TestLifecycleExhaustAuthorizationThenStopInsufficientFunds(t *testing.T) {
	env := LoadEnv(t)
	env.WaitGatewayHealthy(t, 2*time.Minute)

	deviceID := UniqueDeviceID("e2e-exhaust")
	secret := deviceID + "_password"
	const authMsat = 1000
	const unitPrice = 1000
	t.Logf("e2e: test device_id=%s credit=1500 auth_req=%d unit_price=%d", deviceID, authMsat, unitPrice)

	env.ProvisionDevice(t, CreateDeviceRequest{
		DeviceID:             deviceID,
		DeviceSecret:         secret,
		MeasurementUnit:      "kWh",
		UnitPriceMsat:        unitPrice,
		ReportingStrategy:    "interval",
		ReportingInterval:    60,
		HeartbeatInterval:    300,
		AuthorizeRequestMsat: authMsat,
		Timestamp:            time.Now().UTC().Format(time.RFC3339),
	})
	// After first 1000 msat hold, only 500 msat remains — not enough for a second 1000 msat authorization.
	env.CreditDevice(t, deviceID, 1500, deviceID+"-credit-exhaust")

	cb := &RecordingCallback{Logf: t.Logf}
	di := devicepkg.NewDeviceInterface(cb, env.DeviceMQTTConfig(), deviceID)
	di.SetHeartbeatEnabled(false)
	di.Connect(deviceID, secret)
	defer di.Disconnect()

	WaitUntil(t, 90*time.Second, 400*time.Millisecond, "first authorization", func() bool {
		return di.IsConnected() && di.GetDeviceConfig() != nil && di.HasActiveAuthorization()
	}, DiagMQTT(env, deviceID, di, nil))

	t.Logf("e2e: publishing usage to exhaust authorization (measure=1.0 kWh * price %d => 1000 msat)", unitPrice)
	di.PublishUsageReport(devicepkg.GenerateID(), 1.0)

	WaitUntil(t, 2*time.Minute, 500*time.Millisecond, "STOP after insufficient funds for next auth", func() bool {
		reason := cb.LastStopReason()
		return strings.Contains(reason, "INSUFFICIENT_FUNDS")
	}, DiagMQTT(env, deviceID, di, func() string {
		return fmt.Sprintf("stops=%v auth_controls=%v last_stop=%q",
			cb.StopsSnapshot(), cb.AuthControlSnapshot(), cb.LastStopReason())
	}))

	stops := cb.StopsSnapshot()
	if len(stops) < 1 {
		t.Fatalf("expected at least one STOP, got %v", stops)
	}
	t.Logf("e2e: final STOP reasons: %v", stops)
}

// TestLifecycleSequentialUsageReports exercises USE-002: several distinct reports each recorded once with stable funding.
func TestLifecycleSequentialUsageReports(t *testing.T) {
	env := LoadEnv(t)
	env.WaitGatewayHealthy(t, 2*time.Minute)

	deviceID := UniqueDeviceID("e2e-multiuse")
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
		AuthorizeRequestMsat: 500_000,
		Timestamp:            time.Now().UTC().Format(time.RFC3339),
	})
	env.CreditDevice(t, deviceID, 2_000_000, deviceID+"-credit-multi")

	cb := &RecordingCallback{Logf: t.Logf}
	di := devicepkg.NewDeviceInterface(cb, env.DeviceMQTTConfig(), deviceID)
	di.SetHeartbeatEnabled(false)
	di.Connect(deviceID, secret)
	defer di.Disconnect()

	WaitUntil(t, 90*time.Second, 400*time.Millisecond, "authorization", func() bool {
		return di.IsConnected() && di.GetDeviceConfig() != nil && di.HasActiveAuthorization()
	}, DiagMQTT(env, deviceID, di, nil))

	const n = 6
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = devicepkg.GenerateID()
		t.Logf("e2e: usage %d/%d report_id=%s", i+1, n, ids[i])
		di.PublishUsageReport(ids[i], 0.001)
		time.Sleep(100 * time.Millisecond)
	}

	WaitUntil(t, 2*time.Minute, 500*time.Millisecond, "all reports present", func() bool {
		got := map[string]bool{}
		for _, c := range env.ListConsumptions(t, deviceID, 50) {
			got[c.ReportID] = true
		}
		for _, id := range ids {
			if !got[id] {
				return false
			}
		}
		return true
	}, DiagMQTT(env, deviceID, di, func() string {
		got := map[string]bool{}
		for _, c := range env.ListConsumptions(t, deviceID, 50) {
			got[c.ReportID] = true
		}
		missing := 0
		for _, id := range ids {
			if !got[id] {
				missing++
			}
		}
		return fmt.Sprintf("expected %d report ids, still missing %d", n, missing)
	}))
}

// TestLifecycleAuthorizationControlAfterExhaust verifies AUTHORIZATION control (REPLENISH) is observed after consuming a hold.
func TestLifecycleAuthorizationControlAfterExhaust(t *testing.T) {
	env := LoadEnv(t)
	env.WaitGatewayHealthy(t, 2*time.Minute)

	deviceID := UniqueDeviceID("e2e-authctl")
	secret := deviceID + "_password"
	t.Logf("e2e: test device_id=%s", deviceID)

	env.ProvisionDevice(t, CreateDeviceRequest{
		DeviceID:             deviceID,
		DeviceSecret:         secret,
		MeasurementUnit:      "kWh",
		UnitPriceMsat:        500,
		ReportingStrategy:    "interval",
		ReportingInterval:    60,
		HeartbeatInterval:    300,
		AuthorizeRequestMsat: 500,
		Timestamp:            time.Now().UTC().Format(time.RFC3339),
	})
	env.CreditDevice(t, deviceID, 2000, deviceID+"-credit-authctl")

	cb := &RecordingCallback{Logf: t.Logf}
	di := devicepkg.NewDeviceInterface(cb, env.DeviceMQTTConfig(), deviceID)
	di.SetHeartbeatEnabled(false)
	di.Connect(deviceID, secret)
	defer di.Disconnect()

	WaitUntil(t, 90*time.Second, 400*time.Millisecond, "authorization", func() bool {
		return di.IsConnected() && di.GetDeviceConfig() != nil && di.HasActiveAuthorization()
	}, DiagMQTT(env, deviceID, di, nil))

	t.Logf("e2e: usage report to complete 500 msat authorization (measure=1.0 * price 500)")
	di.PublishUsageReport(devicepkg.GenerateID(), 1.0)

	WaitUntil(t, 2*time.Minute, 400*time.Millisecond, "AUTHORIZATION control after completion", func() bool {
		for _, r := range cb.AuthControlSnapshot() {
			if strings.Contains(strings.ToUpper(r), "REPLENISH") {
				return true
			}
		}
		return false
	}, DiagMQTT(env, deviceID, di, func() string {
		return fmt.Sprintf("auth_control_reasons=%v stops=%v", cb.AuthControlSnapshot(), cb.StopsSnapshot())
	}))
}
