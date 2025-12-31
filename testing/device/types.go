package device

import (
	mqttmodel "github.com/robertodantas/lina/services/proto/gen/model/mqtt"
	"google.golang.org/protobuf/encoding/protojson"
)

// Type aliases for protobuf types (single source of truth)
type DeviceConfig = mqttmodel.ConfigPayload
type BalanceMessage = mqttmodel.BalancePayload
type AuthorizeResponse = mqttmodel.AuthorizationResponsePayload
type InvoiceResponse = mqttmodel.InvoiceResponsePayload
type HeartbeatMessage = mqttmodel.HeartbeatPayload
type AuthorizeRequest = mqttmodel.AuthorizationRequestPayload
type UsageReport = mqttmodel.UsagePayload
type InvoiceRequest = mqttmodel.InvoiceRequestPayload

// Authorization represents an authorization granted to a device
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

// DeviceState represents the device's current state (device-related fields only)
// Device implementations can extend this with their own fields
type DeviceState struct {
	DeviceID             string           `json:"deviceId"`
	DeviceStatus         string           `json:"deviceStatus"`
	Balance              *BalanceMessage  `json:"balance"`
	Config               *DeviceConfig    `json:"config"`
	Invoice              *InvoiceResponse `json:"invoice"`
	CurrentAuthorization *Authorization   `json:"currentAuthorization"`
	MQTTStatus           string           `json:"mqttStatus"`
}

// Config holds MQTT connection configuration
type Config struct {
	HTTPPort          string
	MQTTBroker        string
	MQTTUseTLS        bool
	MQTTPort          int
	MQTTTLSPort       int
	MQTTTLSCACert     string
	MQTTTLSSkipVerify bool
	MQTTTLSServerName string
}

// Proto marshal/unmarshal options (shared)
var (
	ProtoMarshalOpts   = protojson.MarshalOptions{UseProtoNames: true}
	ProtoUnmarshalOpts = protojson.UnmarshalOptions{DiscardUnknown: true}
)
