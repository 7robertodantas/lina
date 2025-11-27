package main

import (
	"strings"
)

type invoiceMetadata struct {
	DeviceID string `json:"device_id"`
	Reason   string `json:"reason,omitempty"`
}

func encodeInvoiceMetadata(deviceID, reason string) string {
	if reason == "" {
		return deviceID
	}
	return deviceID + ":" + reason
}

func decodeInvoiceMetadata(memo string) invoiceMetadata {
	parts := strings.SplitN(memo, ":", 2)
	if len(parts) == 2 {
		return invoiceMetadata{
			DeviceID: parts[0],
			Reason:   parts[1],
		}
	}
	return invoiceMetadata{Reason: memo}
}
