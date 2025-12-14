package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	lightningmodel "github.com/robertodantas/lnpay/proto/gen/model/lightning"
)

type LNDStreamHandler struct {
	eastwestStreamPublisher *EastWestStreamPublisher
}

func NewLNDStreamHandler(eastwestStreamPublisher *EastWestStreamPublisher) *LNDStreamHandler {
	return &LNDStreamHandler{
		eastwestStreamPublisher: eastwestStreamPublisher,
	}
}

// HandleInvoiceCreated handles an invoice created event.
func (h *LNDStreamHandler) HandleInvoiceCreated(ctx context.Context, invoice *lnrpc.Invoice) error {
	deviceMeta := decodeInvoiceMetadata(invoice.Memo)
	invoiceID := fmt.Sprintf("%x", invoice.RHash)
	amountMsat := invoice.ValueMsat
	if amountMsat == 0 {
		amountMsat = invoice.Value * 1000
	}
	expiresAt := time.Unix(invoice.CreationDate+invoice.Expiry, 0).UTC().Format(time.RFC3339)

	lnInvoice := &lightningmodel.Invoice{
		InvoiceId:  invoiceID,
		DeviceId:   deviceMeta.DeviceID,
		Bolt11:     invoice.PaymentRequest,
		AmountMsat: amountMsat,
		Status:     mapInvoiceStatus(invoice.State),
		ExpiresAt:  expiresAt,
	}

	if err := h.eastwestStreamPublisher.PublishInvoiceCreated(ctx, lnInvoice); err != nil {
		return fmt.Errorf("failed to publish invoice created event: %w", err)
	}

	return nil
}

// HandleInvoiceSettled handles an invoice settled event.
func (h *LNDStreamHandler) HandleInvoiceSettled(ctx context.Context, invoice *lnrpc.Invoice) error {
	deviceMeta := decodeInvoiceMetadata(invoice.Memo)
	invoiceID := fmt.Sprintf("%x", invoice.RHash)
	amountReceivedMsat := invoice.AmtPaidSat * 1000
	timestamp := time.Unix(invoice.SettleDate, 0).UTC().Format(time.RFC3339)

	logger.WithDeviceID(deviceMeta.DeviceID).
		InfoWithFields(ctx, "Invoice settled via cloud LND node", map[string]interface{}{
			"invoice_id":           invoiceID,
			"amount_received_msat": amountReceivedMsat,
		})

	if err := h.eastwestStreamPublisher.PublishInvoiceSettled(ctx, invoiceID, deviceMeta.DeviceID, amountReceivedMsat, timestamp); err != nil {
		return fmt.Errorf("failed to publish invoice settled event: %w", err)
	}

	return nil
}

// HandleInvoiceExpired handles an invoice expired event.
func (h *LNDStreamHandler) HandleInvoiceExpired(ctx context.Context, invoice *lnrpc.Invoice) error {
	deviceMeta := decodeInvoiceMetadata(invoice.Memo)
	invoiceID := fmt.Sprintf("%x", invoice.RHash)
	timestamp := time.Unix(invoice.CreationDate+invoice.Expiry, 0).UTC().Format(time.RFC3339)

	logger.WithDeviceID(deviceMeta.DeviceID).
		InfoWithFields(ctx, "Invoice expired/canceled via cloud LND node", map[string]interface{}{
			"invoice_id": invoiceID,
		})

	if err := h.eastwestStreamPublisher.PublishInvoiceExpired(ctx, invoiceID, deviceMeta.DeviceID, timestamp); err != nil {
		return fmt.Errorf("failed to publish invoice expired event: %w", err)
	}

	return nil
}

func mapInvoiceStatus(state lnrpc.Invoice_InvoiceState) lightningmodel.InvoiceStatus {
	switch state {
	case lnrpc.Invoice_SETTLED:
		return lightningmodel.InvoiceStatus_INVOICE_STATUS_SETTLED
	case lnrpc.Invoice_CANCELED:
		return lightningmodel.InvoiceStatus_INVOICE_STATUS_EXPIRED
	default:
		return lightningmodel.InvoiceStatus_INVOICE_STATUS_CREATED
	}
}
