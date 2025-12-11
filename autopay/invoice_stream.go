package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
)

// InvoiceStreamHandler handles invoice stream events
type InvoiceStreamHandler struct {
	receiverLND *LNDClient
	payerLND    *LNDClient
}

func NewInvoiceStreamHandler(receiverLND, payerLND *LNDClient) *InvoiceStreamHandler {
	return &InvoiceStreamHandler{
		receiverLND: receiverLND,
		payerLND:    payerLND,
	}
}

// Start begins listening for invoice updates and auto-paying them
func (h *InvoiceStreamHandler) Start(ctx context.Context) error {
	stream, err := h.receiverLND.SubscribeInvoices(ctx, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to subscribe to invoices: %w", err)
	}

	logger.Info(ctx, "Invoice stream started, listening for invoice creation events")

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info(ctx, "Invoice stream stopped")
				return
			default:
				invoice, err := stream.Recv()
				if err != nil {
					logger.Error(ctx, "Error receiving invoice update", err)
					time.Sleep(5 * time.Second)
					// Try to reconnect
					stream, err = h.receiverLND.SubscribeInvoices(ctx, 0, 0)
					if err != nil {
						logger.Error(ctx, "Failed to reconnect invoice stream", err)
					}
					continue
				}

				// Process invoice update
				h.handleInvoiceUpdate(ctx, invoice)
			}
		}
	}()

	return nil
}

// handleInvoiceUpdate processes invoice updates and pays them when created
func (h *InvoiceStreamHandler) handleInvoiceUpdate(ctx context.Context, invoice *lnrpc.Invoice) {
	invoiceID := fmt.Sprintf("%x", invoice.RHash)
	amountMsat := invoice.ValueMsat
	if amountMsat == 0 {
		amountMsat = invoice.Value * 1000
	}
	stateName := invoice.State.String()

	logger.InfoWithFields(ctx, "Received invoice update", map[string]interface{}{
		"invoice_id":  invoiceID,
		"state":       stateName,
		"amount_msat": amountMsat,
	})

	// Only pay invoices that are OPEN or ACCEPTED (newly created)
	if invoice.State != lnrpc.Invoice_OPEN && invoice.State != lnrpc.Invoice_ACCEPTED {
		logger.DebugWithFields(ctx, "Skipping invoice (not OPEN/ACCEPTED)", map[string]interface{}{
			"invoice_id": invoiceID,
			"state":      stateName,
		})
		return
	}

	if invoice.PaymentRequest == "" {
		logger.WarnWithFields(ctx, "Invoice missing payment request", map[string]interface{}{
			"invoice_id": invoiceID,
		})
		return
	}

	// Pay the invoice using the payer LND node
	paymentCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	logger.InfoWithFields(paymentCtx, "Auto-paying invoice", map[string]interface{}{
		"invoice_id":  invoiceID,
		"amount_msat": amountMsat,
	})

	response, err := h.payerLND.PayInvoice(paymentCtx, invoice.PaymentRequest)
	if err != nil {
		logger.ErrorWithFields(paymentCtx, "Failed to pay invoice", err, map[string]interface{}{
			"invoice_id": invoiceID,
		})
		return
	}

	// Check payment status
	if response.PaymentError != "" {
		logger.ErrorWithFields(paymentCtx, "Payment failed", fmt.Errorf("payment error: %s", response.PaymentError), map[string]interface{}{
			"invoice_id":    invoiceID,
			"payment_error": response.PaymentError,
		})
		return
	}

	logger.InfoWithFields(paymentCtx, "Invoice paid successfully", map[string]interface{}{
		"invoice_id":       invoiceID,
		"payment_hash":     fmt.Sprintf("%x", response.PaymentHash),
		"payment_preimage": fmt.Sprintf("%x", response.PaymentPreimage),
	})
}
