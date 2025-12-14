package main

// DeviceCallback defines callbacks that device implementations provide
// to handle MQTT message events
type DeviceCallback interface {
	// OnConfigUpdated is called when device configuration is updated
	OnConfigUpdated(config *DeviceConfig)

	// OnBalanceUpdated is called when balance is updated
	OnBalanceUpdated(balance *BalanceMessage)

	// OnAuthorizationGranted is called when authorization is granted
	OnAuthorizationGranted(response *AuthorizeResponse)

	// OnAuthorizationActive is called when an existing authorization is found
	OnAuthorizationActive(response *AuthorizeResponse)

	// OnAuthorizationRejected is called when authorization is rejected
	OnAuthorizationRejected(response *AuthorizeResponse)

	// OnInvoiceCreated is called when an invoice is created
	OnInvoiceCreated(response *InvoiceResponse)

	// OnInvoiceSettled is called when an invoice is settled (from response or event)
	OnInvoiceSettled(invoiceID string, amountMsat int64)

	// OnInvoiceExpired is called when an invoice expires
	OnInvoiceExpired(invoiceID string)

	// OnInvoiceFailed is called when an invoice fails
	OnInvoiceFailed(invoiceID string)

	// OnControlStop is called when STOP command is received
	OnControlStop(reason string)

	// OnControlPause is called when PAUSE command is received
	OnControlPause(reason string)

	// OnControlResume is called when RESUME command is received
	OnControlResume()

	// OnControlReboot is called when REBOOT command is received
	OnControlReboot()

	// OnConnected is called when the device has successfully connected to MQTT
	OnConnected()

	// OnMQTTStatus is called when MQTT connection status changes
	OnMQTTStatus(status string)

	// OnDeviceStatus is called when device status changes
	OnDeviceStatus(status string)

	// OnLog is called when a log message should be recorded
	OnLog(message, logType string)
}
