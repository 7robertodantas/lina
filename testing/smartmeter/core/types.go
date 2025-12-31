package main

import (
	mqttmodel "github.com/robertodantas/lina/services/proto/gen/model/mqtt"
	devicepkg "github.com/robertodantas/lina/testing/device"
	"google.golang.org/protobuf/encoding/protojson"
)

// Proto type aliases (re-exported from device package for convenience)
type DeviceConfig = devicepkg.DeviceConfig
type BalanceMessage = devicepkg.BalanceMessage
type AuthorizeResponse = devicepkg.AuthorizeResponse
type InvoiceResponse = devicepkg.InvoiceResponse
type HeartbeatMessage = devicepkg.HeartbeatMessage
type AuthorizeRequest = devicepkg.AuthorizeRequest
type UsageReport = devicepkg.UsageReport
type InvoiceRequest = devicepkg.InvoiceRequest
type Authorization = devicepkg.Authorization

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
// Extends devicepkg.DeviceState with smartmeter-specific fields
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
