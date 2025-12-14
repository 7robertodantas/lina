package main

import (
	mqttmodel "github.com/robertodantas/lnpay/services/proto/gen/model/mqtt"
	"google.golang.org/protobuf/encoding/protojson"
)

// Proto type aliases (single source of truth)
// DeviceConfig represents the device configuration payload received via MQTT
type DeviceConfig = mqttmodel.ConfigPayload
type BalanceMessage = mqttmodel.BalancePayload
type AuthorizeResponse = mqttmodel.AuthorizationResponsePayload
type InvoiceResponse = mqttmodel.InvoiceResponsePayload
type HeartbeatMessage = mqttmodel.HeartbeatPayload
type AuthorizeRequest = mqttmodel.AuthorizationRequestPayload
type UsageReport = mqttmodel.UsagePayload
type InvoiceRequest = mqttmodel.InvoiceRequestPayload

// Domain types used across the simulator
type Appliance struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Icon         string `json:"icon"`
	MinWatts     int    `json:"minWatts"`
	MaxWatts     int    `json:"maxWatts"`
	IsOn         bool   `json:"isOn"`
	CurrentWatts int    `json:"currentWatts"`
}

// SmartMeterState contains SmartMeter-specific state
type SmartMeterState struct {
	Appliances       []Appliance `json:"appliances"`
	TotalConsumption float64     `json:"totalConsumption"`
	InstantPower     int         `json:"instantPower"`
	Logs             []LogEntry  `json:"logs"`
}

// DeviceState combines DeviceContext and SmartMeterState for UI/API
type DeviceState struct {
	DeviceID             string           `json:"deviceId"`
	DeviceStatus         string           `json:"deviceStatus"`
	Appliances           []Appliance      `json:"appliances"`
	Balance              *BalanceMessage  `json:"balance"`
	Config               *DeviceConfig    `json:"config"`
	TotalConsumption     float64          `json:"totalConsumption"`
	InstantPower         int              `json:"instantPower"`
	Invoice              *InvoiceResponse `json:"invoice"`
	CurrentAuthorization *Authorization   `json:"currentAuthorization"`
	Logs                 []LogEntry       `json:"logs"`
	MQTTStatus           string           `json:"mqttStatus"`
}

type Authorization struct {
	AuthorizationID string `json:"authorization_id"`
	RequestID       string `json:"request_id"`
	GrantedMsat     int64  `json:"granted_msat"`
	RemainingMsat   int64  `json:"remaining_msat"`
	IssuedAt        string `json:"issued_at"`
	ExpiresAt       string `json:"expires_at"`
	Status          string `json:"status"`
	Reason          string `json:"reason"`
}

type LogEntry struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	Type      string `json:"type"`
}

var defaultAppliances = []Appliance{
	{ID: "fridge", Name: "Refrigerator", Icon: "fridge", MinWatts: 100, MaxWatts: 150, IsOn: false, CurrentWatts: 0},
	{ID: "microwave", Name: "Microwave", Icon: "microwave", MinWatts: 800, MaxWatts: 1200, IsOn: false, CurrentWatts: 0},
	{ID: "heater", Name: "Space Heater", Icon: "heater", MinWatts: 1000, MaxWatts: 1500, IsOn: false, CurrentWatts: 0},
	{ID: "oven", Name: "Electric Oven", Icon: "oven", MinWatts: 2000, MaxWatts: 2500, IsOn: false, CurrentWatts: 0},
	{ID: "computer", Name: "Computer", Icon: "computer", MinWatts: 150, MaxWatts: 300, IsOn: false, CurrentWatts: 0},
	{ID: "washer", Name: "Washing Machine", Icon: "washer", MinWatts: 300, MaxWatts: 500, IsOn: false, CurrentWatts: 0},
}

// Proto marshal/unmarshal options (shared)
var (
	protoMarshalOpts   = protojson.MarshalOptions{UseProtoNames: true}
	protoUnmarshalOpts = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// InvoiceResponseJSON is a JSON-friendly representation of InvoiceResponse with string status
type InvoiceResponseJSON struct {
	DeviceID   string `json:"device_id"`
	RequestID  string `json:"request_id"`
	Status     string `json:"status"`
	InvoiceID  string `json:"invoice_id"`
	Bolt11     string `json:"bolt11"`
	AmountMsat int64  `json:"amount_msat"`
	ExpiresAt  string `json:"expires_at"`
}

// ConvertInvoiceStatusToString converts protobuf InvoiceStatus enum to string
func ConvertInvoiceStatusToString(status mqttmodel.InvoiceStatus) string {
	switch status {
	case mqttmodel.InvoiceStatus_INVOICE_STATUS_CREATED:
		return "CREATED"
	case mqttmodel.InvoiceStatus_INVOICE_STATUS_SETTLED:
		return "PAID" // Frontend expects "PAID" not "SETTLED"
	case mqttmodel.InvoiceStatus_INVOICE_STATUS_EXPIRED:
		return "EXPIRED"
	case mqttmodel.InvoiceStatus_INVOICE_STATUS_FAILED:
		return "ERROR" // Frontend expects "ERROR" not "FAILED"
	default:
		return "CREATED"
	}
}

// ConvertInvoiceResponseToJSON converts InvoiceResponse to JSON-friendly format
func ConvertInvoiceResponseToJSON(invoice *InvoiceResponse) *InvoiceResponseJSON {
	if invoice == nil {
		return nil
	}
	return &InvoiceResponseJSON{
		DeviceID:   invoice.DeviceId,
		RequestID:  invoice.RequestId,
		Status:     ConvertInvoiceStatusToString(invoice.Status),
		InvoiceID:  invoice.InvoiceId,
		Bolt11:     invoice.Bolt11,
		AmountMsat: invoice.AmountMsat,
		ExpiresAt:  invoice.ExpiresAt,
	}
}
