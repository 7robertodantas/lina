package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	lndStreamTracer = otel.Tracer("lnd.stream")
)

type LNDStreamInterface struct {
	lndClient *LNDClient
	handler   *LNDStreamHandler
}

func NewLNDStreamInterface(lndClient *LNDClient, handler *LNDStreamHandler) *LNDStreamInterface {
	return &LNDStreamInterface{
		lndClient: lndClient,
		handler:   handler,
	}
}

// Start begins listening for LND invoice updates and calling handler methods.
func (si *LNDStreamInterface) Start(ctx context.Context) error {
	stream, err := si.lndClient.SubscribeInvoices(ctx, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to subscribe to invoices: %w", err)
	}

	logger.Info(ctx, "LND event stream started, listening for invoice updates via cloud LND node")

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info(ctx, "LND event stream stopped via cloud LND node")
				return
			default:
				invoice, err := stream.Recv()
				if err != nil {
					logger.Error(ctx, "Error receiving invoice update via cloud LND node", err)
					time.Sleep(5 * time.Second)
					stream, err = si.lndClient.SubscribeInvoices(ctx, 0, 0)
					if err != nil {
						logger.Error(ctx, "Failed to reconnect invoice stream via cloud LND node", err)
					}
					continue
				}

				// Process invoice with tracing and route to appropriate handler
				si.processInvoiceWithTracing(ctx, invoice)
			}
		}
	}()

	return nil
}

// processInvoiceWithTracing wraps invoice processing with OpenTelemetry tracing
func (si *LNDStreamInterface) processInvoiceWithTracing(ctx context.Context, invoice *lnrpc.Invoice) {
	invoiceID := fmt.Sprintf("%x", invoice.RHash)
	amountMsat := invoice.ValueMsat
	if amountMsat == 0 {
		amountMsat = invoice.Value * 1000
	}
	stateName := invoice.State.String()
	deviceMeta := decodeInvoiceMetadata(invoice.Memo)

	// Create span for invoice processing
	ctx, span := lndStreamTracer.Start(ctx, "[lnd] invoice stream received",
		trace.WithAttributes(
			attribute.String("lnd.invoice.id", invoiceID),
			attribute.String("lnd.invoice.state", stateName),
			attribute.Int64("lnd.invoice.amount_msat", amountMsat),
			attribute.String("lnd.device_id", deviceMeta.DeviceID),
			attribute.String("lnd.operation", "INVOICE_UPDATE"),
		),
	)
	defer span.End()

	// Route to appropriate handler based on invoice state
	switch invoice.State {
	case lnrpc.Invoice_OPEN, lnrpc.Invoice_ACCEPTED:
		span.SetAttributes(attribute.String("lnd.event.type", "INVOICE_CREATED"))
		if err := si.handler.HandleInvoiceCreated(ctx, invoice); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to handle invoice created")
			logger.Error(ctx, "Failed to handle invoice created", err)
		} else {
			span.SetStatus(codes.Ok, "invoice created handled")
		}
	case lnrpc.Invoice_SETTLED:
		span.SetAttributes(attribute.String("lnd.event.type", "INVOICE_SETTLED"))
		if settled := invoice; settled != nil {
			span.SetAttributes(attribute.Int64("lnd.invoice.amount_received_msat", settled.AmtPaidSat*1000))
		}
		if err := si.handler.HandleInvoiceSettled(ctx, invoice); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to handle invoice settled")
			logger.Error(ctx, "Failed to handle invoice settled", err)
		} else {
			span.SetStatus(codes.Ok, "invoice settled handled")
		}
	case lnrpc.Invoice_CANCELED:
		span.SetAttributes(attribute.String("lnd.event.type", "INVOICE_EXPIRED"))
		if err := si.handler.HandleInvoiceExpired(ctx, invoice); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to handle invoice expired")
			logger.Error(ctx, "Failed to handle invoice expired", err)
		} else {
			span.SetStatus(codes.Ok, "invoice expired handled")
		}
	default:
		span.SetAttributes(attribute.String("lnd.event.type", "IGNORED"))
		span.SetStatus(codes.Ok, "unsupported state ignored")
		logger.DebugWithFields(ctx, "Ignoring invoice update with unsupported state via cloud LND node", map[string]interface{}{
			"invoice_id": invoiceID,
			"state":      stateName,
		})
	}
}
