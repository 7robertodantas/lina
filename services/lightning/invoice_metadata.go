package main

import (
	"encoding/json"
	"log"
)

type invoiceMetadata struct {
	DeviceID string `json:"device_id"`
	Reason   string `json:"reason,omitempty"`
}

func encodeInvoiceMetadata(deviceID, reason string) string {
	meta := invoiceMetadata{
		DeviceID: deviceID,
		Reason:   reason,
	}

	data, err := json.Marshal(meta)
	if err != nil {
		log.Printf("failed to encode invoice metadata, returning reason only: %v", err)
		return reason
	}

	return string(data)
}

func decodeInvoiceMetadata(memo string) invoiceMetadata {
	var meta invoiceMetadata
	if err := json.Unmarshal([]byte(memo), &meta); err != nil {
		return invoiceMetadata{Reason: memo}
	}
	return meta
}
