package e2e

import (
	"os"
	"sync"

	devicepkg "github.com/robertodantas/lina/testing/device"
)

// RecordingCallback implements device.DeviceCallback and records control-related
// events for assertions. Safe for concurrent use.
// Optional Logf (e.g. t.Logf) receives high-signal MQTT/device events for debugging stuck tests.
type RecordingCallback struct {
	mu sync.Mutex

	Logf func(format string, args ...any)

	Stops              []string
	Pauses             []string
	Resumes            []string
	AuthControlReasons []string
	InvoicesSettled    []struct {
		ID     string
		Amount int64
	}
}

func (r *RecordingCallback) OnConfigUpdated(config *devicepkg.DeviceConfig) {
	if config != nil {
		r.logf("e2e device: config updated strategy=%s interval=%d unit_price_msat=%d auth_req_msat=%d",
			config.ReportingStrategy, config.ReportingInterval, config.UnitPriceMsat, config.AuthorizeRequestMsat)
	} else {
		r.logf("e2e device: config updated (nil)")
	}
}

func (r *RecordingCallback) OnBalanceUpdated(balance *devicepkg.BalanceMessage) {
	if balance != nil {
		r.logf("e2e device: balance updated available_msat=%d", balance.AvailableMsat)
	} else {
		r.logf("e2e device: balance updated (nil)")
	}
}

func (r *RecordingCallback) OnAuthorizationGranted(response *devicepkg.AuthorizeResponse) {
	if response != nil {
		r.logf("e2e device: authorization GRANTED request_id=%s granted_msat=%d remaining_msat=%d",
			response.RequestId, response.GrantedMsat, response.RemainingMsat)
	}
}

func (r *RecordingCallback) OnAuthorizationActive(response *devicepkg.AuthorizeResponse) {
	if response != nil {
		r.logf("e2e device: authorization ACTIVE request_id=%s remaining_msat=%d",
			response.RequestId, response.RemainingMsat)
	}
}

func (r *RecordingCallback) OnAuthorizationRejected(response *devicepkg.AuthorizeResponse) {
	if response != nil {
		r.logf("e2e device: authorization REJECTED request_id=%s reason=%q available_msat=%d",
			response.RequestId, response.Reason, response.AvailableMsat)
	}
}

func (r *RecordingCallback) OnInvoiceCreated(response *devicepkg.InvoiceResponse) {
	if response != nil {
		r.logf("e2e device: invoice CREATED id=%s request_id=%s", response.InvoiceId, response.RequestId)
	}
}

func (r *RecordingCallback) OnInvoiceSettled(invoiceID string, amountMsat int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.InvoicesSettled = append(r.InvoicesSettled, struct {
		ID     string
		Amount int64
	}{invoiceID, amountMsat})
	r.logf("e2e device: invoice settled id=%s amount_msat=%d", invoiceID, amountMsat)
}

func (r *RecordingCallback) OnInvoiceExpired(invoiceID string) {}

func (r *RecordingCallback) OnInvoiceFailed(invoiceID string) {}

func (r *RecordingCallback) OnControlStop(reason string) {
	r.logf("e2e device: control STOP reason=%q", reason)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Stops = append(r.Stops, reason)
}

func (r *RecordingCallback) OnControlPause(reason string) {
	r.logf("e2e device: control PAUSE reason=%q", reason)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Pauses = append(r.Pauses, reason)
}

func (r *RecordingCallback) OnControlResume() {
	r.logf("e2e device: control RESUME")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Resumes = append(r.Resumes, "RESUME")
}

func (r *RecordingCallback) OnControlReboot() {}

func (r *RecordingCallback) OnConnected() {
	r.logf("e2e device: OnConnected (subscriptions + startup authorize sent)")
}

func (r *RecordingCallback) OnMQTTStatus(status string) {
	r.logf("e2e device: mqtt status=%q", status)
}

func (r *RecordingCallback) OnDeviceStatus(status string) {
	r.logf("e2e device: device status=%q", status)
}

func (r *RecordingCallback) OnLog(message, logType string) {
	switch logType {
	case "error", "warning", "warn":
		r.logf("e2e device: [%s] %s", logType, message)
	default:
		if os.Getenv("LINA_E2E_DEVICE_TRACE") == "1" {
			r.logf("e2e device: [%s] %s", logType, message)
		}
	}
}

// OnControlAuthorization is invoked by DeviceInterface for AUTHORIZATION control (optional interface).
func (r *RecordingCallback) OnControlAuthorization(reason string) {
	r.logf("e2e device: control AUTHORIZATION reason=%q (will request new auth)", reason)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.AuthControlReasons = append(r.AuthControlReasons, reason)
}

func (r *RecordingCallback) logf(format string, args ...any) {
	if r == nil || r.Logf == nil {
		return
	}
	r.Logf(format, args...)
}

func (r *RecordingCallback) LastStopReason() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.Stops) == 0 {
		return ""
	}
	return r.Stops[len(r.Stops)-1]
}

func (r *RecordingCallback) StopsSnapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.Stops))
	copy(out, r.Stops)
	return out
}

func (r *RecordingCallback) AuthControlSnapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.AuthControlReasons))
	copy(out, r.AuthControlReasons)
	return out
}
