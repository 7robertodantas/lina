package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	devicepkg "github.com/robertodantas/lina/testing/device"
)

const e2eWaitLogInterval = 3 * time.Second

// Env holds connectivity settings for integration tests against a running stack
// (e.g. deployment/docker-compose.evaluation.edge.yml + Caddy on 8080, Mosquitto TLS on 8883).
//
// Environment variables:
//   - LINA_E2E: must be "1" for tests to run (otherwise skipped).
//   - LINA_E2E_BASE_URL: gateway base URL (default http://127.0.0.1:8080).
//   - LINA_E2E_SERVICE_TOKEN: X-Service-Token for ledger credit and consumption API (default dev-token).
//   - LINA_E2E_MQTT_HOST: broker hostname from test process (default 127.0.0.1).
//   - LINA_E2E_MQTT_TLS_PORT: TLS port (default 8883).
//   - LINA_E2E_MQTT_CA: path to CA PEM (default ../../infrastructure/certs/ca.crt from module dir).
//   - LINA_E2E_MQTT_TLS_SKIP_VERIFY: if "1", skip TLS verify (dev only).
//   - LINA_E2E_MQTT_TLS_SERVER_NAME: TLS ServerName if cert does not match host (e.g. mosquitto).
//   - LINA_E2E_DEVICE_TRACE: if "1", RecordingCallback forwards all device OnLog lines (verbose).
type Env struct {
	BaseURL       string
	ServiceToken  string
	MQTTHost      string
	MQTTTLSPort   int
	MQTTCACert    string
	MQTTSkipTLSVerify bool
	MQTTServerName    string
}

func LoadEnv(t *testing.T) Env {
	t.Helper()
	if os.Getenv("LINA_E2E") != "1" {
		t.Skip("set LINA_E2E=1 and start the edge stack (see deployment/docker-compose.evaluation.edge.yml)")
	}
	base := os.Getenv("LINA_E2E_BASE_URL")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	base = strings.TrimRight(base, "/")
	token := os.Getenv("LINA_E2E_SERVICE_TOKEN")
	if token == "" {
		token = "dev-token"
	}
	host := os.Getenv("LINA_E2E_MQTT_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := 8883
	if s := os.Getenv("LINA_E2E_MQTT_TLS_PORT"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("LINA_E2E_MQTT_TLS_PORT: %v", err)
		}
		port = n
	}
	ca := os.Getenv("LINA_E2E_MQTT_CA")
	if ca == "" {
		ca = "../../infrastructure/certs/ca.crt"
	}
	skip := os.Getenv("LINA_E2E_MQTT_TLS_SKIP_VERIFY") == "1"
	sni := os.Getenv("LINA_E2E_MQTT_TLS_SERVER_NAME")
	env := Env{
		BaseURL:           base,
		ServiceToken:      token,
		MQTTHost:          host,
		MQTTTLSPort:       port,
		MQTTCACert:        ca,
		MQTTSkipTLSVerify: skip,
		MQTTServerName:    sni,
	}
	t.Logf("e2e env: base_url=%s mqtt=%s:%d ca=%s tls_skip_verify=%v tls_server_name=%q",
		env.BaseURL, env.MQTTHost, env.MQTTTLSPort, env.MQTTCACert, env.MQTTSkipTLSVerify, env.MQTTServerName)
	return env
}

func (e Env) WaitGatewayHealthy(t *testing.T, timeout time.Duration) {
	t.Helper()
	t.Logf("e2e: waiting for gateway GET %s/health (timeout %s)", e.BaseURL, timeout)
	start := time.Now()
	deadline := time.Now().Add(timeout)
	var lastErr error
	nextLog := start
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.BaseURL+"/health", nil)
		if err != nil {
			lastErr = err
			cancel()
			time.Sleep(300 * time.Millisecond)
			continue
		}
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			t.Logf("e2e: gateway healthy after %s", time.Since(start).Truncate(time.Millisecond))
			return
		}
		if err != nil {
			lastErr = err
		} else {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		}
		if time.Since(nextLog) >= e2eWaitLogInterval {
			t.Logf("e2e: gateway not ready (%v elapsed): %v", time.Since(start).Truncate(time.Millisecond), lastErr)
			nextLog = time.Now()
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("gateway not healthy at %s/health: %v", e.BaseURL, lastErr)
}

// DeviceMQTTConfig returns MQTT settings for testing/device.Config (aligned with services/device env semantics).
func (e Env) DeviceMQTTConfig() *devicepkg.Config {
	return &devicepkg.Config{
		MQTTBroker:             e.MQTTHost,
		MQTTUseTLS:             true,
		MQTTPort:               1883,
		MQTTTLSPort:            e.MQTTTLSPort,
		MQTTTLSProtocol:        "tls",
		MQTTClientID:           "",
		MQTTTLSSkipVerify:      e.MQTTSkipTLSVerify,
		MQTTTLSServerName:      e.MQTTServerName,
		MQTTTLSCACert:          e.MQTTCACert,
		MQTTTLSRequireEdgeCert: false,
		MQTTTLSEdgeCert:        "",
		MQTTTLSEdgeKey:         "",
	}
}

// CreateDeviceRequest mirrors services/device northbound JSON (POST /devices).
type CreateDeviceRequest struct {
	DeviceID             string `json:"device_id"`
	DeviceSecret         string `json:"device_secret"`
	MeasurementUnit      string `json:"measurement_unit"`
	UnitPriceMsat        int64  `json:"unit_price_msat"`
	ReportingStrategy    string `json:"reporting_strategy"`
	ReportingInterval    int    `json:"reporting_interval"`
	HeartbeatInterval    int    `json:"heartbeat_interval"`
	AuthorizeRequestMsat int    `json:"authorize_request_msat"`
	Timestamp            string `json:"timestamp"`
}

func (e Env) POSTJSON(t *testing.T, path string, body any, extraHeaders map[string]string) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (e Env) GET(t *testing.T, path string, extraHeaders map[string]string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.BaseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (e Env) ProvisionDevice(t *testing.T, req CreateDeviceRequest) {
	t.Helper()
	t.Logf("e2e: provisioning device_id=%s (POST /devices)", req.DeviceID)
	resp := e.POSTJSON(t, "/devices", req, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("provision device: %s: %s", resp.Status, string(b))
	}
	t.Logf("e2e: provision OK status=%d device_id=%s", resp.StatusCode, req.DeviceID)
}

type creditBody struct {
	AmountMsat     int64  `json:"amount_msat"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (e Env) CreditDevice(t *testing.T, deviceID string, amountMsat int64, idempotencyKey string) {
	t.Helper()
	t.Logf("e2e: crediting device_id=%s amount_msat=%d idempotency_key=%s", deviceID, amountMsat, idempotencyKey)
	resp := e.POSTJSON(t, "/devices/"+deviceID+"/credit", creditBody{
		AmountMsat:     amountMsat,
		Reason:         "e2e_test_credit",
		IdempotencyKey: idempotencyKey,
	}, map[string]string{"X-Service-Token": e.ServiceToken})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("credit: %s: %s", resp.Status, string(b))
	}
	t.Logf("e2e: credit OK device_id=%s", deviceID)
}

func (e Env) GetBalanceMsat(t *testing.T, deviceID string) int64 {
	t.Helper()
	resp := e.GET(t, "/devices/"+deviceID+"/balance", nil)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("balance: %s: %s", resp.Status, string(b))
	}
	var out struct {
		BalanceMsat int64 `json:"balance_msat"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out.BalanceMsat
}

type consumptionItem struct {
	ReportID  string `json:"report_id"`
	DebitMsat int64  `json:"debit_msat"`
}

func (e Env) ListConsumptions(t *testing.T, deviceID string, limit int) []consumptionItem {
	t.Helper()
	resp := e.GET(t, fmt.Sprintf("/devices/%s/consumptions?limit=%d", deviceID, limit), map[string]string{
		"X-Service-Token": e.ServiceToken,
	})
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("consumptions: %s: %s", resp.Status, string(b))
	}
	var out struct {
		Items []consumptionItem `json:"items"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out.Items
}

// WaitUntil polls cond until it returns true or timeout is reached.
// If diag is non-nil, its return value is logged every ~3s while waiting, on success, and on timeout.
func WaitUntil(t *testing.T, timeout, poll time.Duration, desc string, cond func() bool, diag func() string) {
	t.Helper()
	start := time.Now()
	deadline := time.Now().Add(timeout)
	nextLog := start
	for time.Now().Before(deadline) {
		if cond() {
			if diag != nil {
				t.Logf("e2e: done %q after %s | %s", desc, time.Since(start).Truncate(time.Millisecond), diag())
			} else {
				t.Logf("e2e: done %q after %s", desc, time.Since(start).Truncate(time.Millisecond))
			}
			return
		}
		if diag != nil && time.Since(nextLog) >= e2eWaitLogInterval {
			t.Logf("e2e: waiting %q (%s elapsed) | %s", desc, time.Since(start).Truncate(time.Millisecond), diag())
			nextLog = time.Now()
		}
		time.Sleep(poll)
	}
	if diag != nil {
		t.Logf("e2e: TIMEOUT %q after %s | last snapshot: %s", desc, time.Since(start).Truncate(time.Millisecond), diag())
	} else {
		t.Logf("e2e: TIMEOUT %q after %s", desc, time.Since(start).Truncate(time.Millisecond))
	}
	t.Fatalf("timeout waiting for: %s", desc)
}

// DiagMQTT returns a snapshot of client-visible MQTT/device state plus optional ledger balance and consumption count.
func DiagMQTT(e Env, deviceID string, di devicepkg.DeviceInterface, extra func() string) func() string {
	return func() string {
		var b strings.Builder
		ctx := di.GetDeviceContext()
		fmt.Fprintf(&b, "mqtt_connected=%v", di.IsConnected())
		fmt.Fprintf(&b, " device_status=%q ctx_mqtt=%q", di.GetDeviceStatus(), ctx.MQTTStatus)
		if c := di.GetDeviceConfig(); c != nil {
			fmt.Fprintf(&b, " config=[strategy=%s interval=%d unit_price_msat=%d auth_req_msat=%d]",
				c.ReportingStrategy, c.ReportingInterval, c.UnitPriceMsat, c.AuthorizeRequestMsat)
		} else {
			b.WriteString(" config=nil")
		}
		if a := di.GetAuthorization(); a != nil {
			fmt.Fprintf(&b, " auth=[status=%s remaining_msat=%d granted_msat=%d id=%s]",
				a.Status, a.RemainingMsat, a.GrantedMsat, a.AuthorizationID)
		} else {
			b.WriteString(" auth=nil")
		}
		if bal := di.GetBalance(); bal != nil {
			fmt.Fprintf(&b, " mqtt_balance_available_msat=%d", bal.AvailableMsat)
		} else {
			b.WriteString(" mqtt_balance=nil")
		}
		if s, err := e.snapshotBalanceMsat(deviceID); err != nil {
			fmt.Fprintf(&b, " ledger_balance=error:%v", err)
		} else {
			fmt.Fprintf(&b, " ledger_balance_msat=%d", s)
		}
		if n, sample, err := e.snapshotConsumptions(deviceID, 20); err != nil {
			fmt.Fprintf(&b, " consumptions=error:%v", err)
		} else {
			fmt.Fprintf(&b, " consumptions_count=%d sample_report_ids=%v", n, sample)
		}
		if extra != nil {
			if x := extra(); x != "" {
				fmt.Fprintf(&b, " | %s", x)
			}
		}
		return b.String()
	}
}

func (e Env) snapshotBalanceMsat(deviceID string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.BaseURL+"/devices/"+deviceID+"/balance", nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bytes.TrimSpace(body)))
	}
	var out struct {
		BalanceMsat int64 `json:"balance_msat"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, err
	}
	return out.BalanceMsat, nil
}

func (e Env) snapshotConsumptions(deviceID string, limit int) (n int, reportIDs []string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s/devices/%s/consumptions?limit=%d", e.BaseURL, deviceID, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Service-Token", e.ServiceToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bytes.TrimSpace(body)))
	}
	var out struct {
		Items []consumptionItem `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, nil, err
	}
	n = len(out.Items)
	for i, it := range out.Items {
		if i >= 5 {
			break
		}
		reportIDs = append(reportIDs, it.ReportID)
	}
	return n, reportIDs, nil
}

func UniqueDeviceID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
