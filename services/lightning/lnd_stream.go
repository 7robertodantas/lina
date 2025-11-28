package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	lightningmodel "github.com/robertodantas/lnpay/proto/gen/model/lightning"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	lndStreamTracer = otel.Tracer("lnd.stream")
)

// LightningEventWithContext wraps an event with its trace context
type LightningEventWithContext struct {
	Event   *lightningmodel.LightningEvent
	Context context.Context // Store full context to maintain parent-child span relationship
}

type LNDEventStream struct {
	lndClient   *LNDClient
	subscribers []chan *LightningEventWithContext
	mu          sync.RWMutex
}

func NewLNDEventStream(lndClient *LNDClient) *LNDEventStream {
	return &LNDEventStream{
		lndClient:   lndClient,
		subscribers: make([]chan *LightningEventWithContext, 0),
	}
}

// Subscribe adds a new subscriber to receive events.
func (es *LNDEventStream) Subscribe() <-chan *LightningEventWithContext {
	es.mu.Lock()
	defer es.mu.Unlock()

	ch := make(chan *LightningEventWithContext, 100)
	es.subscribers = append(es.subscribers, ch)
	return ch
}

// Unsubscribe removes a subscriber.
func (es *LNDEventStream) Unsubscribe(ch <-chan *LightningEventWithContext) {
	es.mu.Lock()
	defer es.mu.Unlock()

	for i, sub := range es.subscribers {
		if sub == ch {
			close(sub)
			es.subscribers = append(es.subscribers[:i], es.subscribers[i+1:]...)
			break
		}
	}
}

// Publish sends an event to all subscribers.
func (es *LNDEventStream) Publish(ctx context.Context, event *lightningmodel.LightningEvent) {
	ctx, span := lndStreamTracer.Start(ctx, "[stream] event.lightning publish",
		trace.WithAttributes(
			attribute.String("lnd.event.type", event.GetType().String()),
		),
	)
	defer span.End()

	es.mu.RLock()
	defer es.mu.RUnlock()

	eventWithCtx := &LightningEventWithContext{
		Event:   event,
		Context: ctx, // Store full context with active span
	}

	for _, ch := range es.subscribers {
		select {
		case ch <- eventWithCtx:
		default:
			logger.Warn(ctx, "Subscriber channel full, dropping lightning event via cloud LND node")
		}
	}

	span.SetStatus(codes.Ok, "event published")
}

// Start begins listening for LND invoice updates and publishing events.
func (es *LNDEventStream) Start(ctx context.Context) error {
	stream, err := es.lndClient.SubscribeInvoices(ctx, 0, 0)
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
					stream, err = es.lndClient.SubscribeInvoices(ctx, 0, 0)
					if err != nil {
						logger.Error(ctx, "Failed to reconnect invoice stream via cloud LND node", err)
					}
					continue
				}

				// Use wrapper with tracing - it will publish the event
				es.buildEventFromInvoiceWithTracing(ctx, invoice)
			}
		}
	}()

	return nil
}

// buildEventFromInvoiceWithTracing wraps buildEventFromInvoice with OpenTelemetry tracing
func (es *LNDEventStream) buildEventFromInvoiceWithTracing(ctx context.Context, invoice *lnrpc.Invoice) {
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

	// Call the actual business logic
	event := es.buildEventFromInvoice(ctx, invoice)

	// Add event type to span based on result
	if event != nil {
		var eventType string
		switch event.GetType() {
		case lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_CREATED:
			eventType = "INVOICE_CREATED"
		case lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_SETTLED:
			eventType = "INVOICE_SETTLED"
			if settled := event.GetInvoiceSettled(); settled != nil {
				span.SetAttributes(attribute.Int64("lnd.invoice.amount_received_msat", settled.AmountReceivedMsat))
			}
		case lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_EXPIRED:
			eventType = "INVOICE_EXPIRED"
		}
		if eventType != "" {
			span.SetAttributes(attribute.String("lnd.event.type", eventType))
		}
		span.SetStatus(codes.Ok, "event created")

		// Publish with the current context so it becomes a child span
		es.Publish(ctx, event)
	} else {
		span.SetAttributes(attribute.String("lnd.event.type", "IGNORED"))
		span.SetStatus(codes.Ok, "unsupported state ignored")
	}
}

func (es *LNDEventStream) buildEventFromInvoice(ctx context.Context, invoice *lnrpc.Invoice) *lightningmodel.LightningEvent {
	deviceMeta := decodeInvoiceMetadata(invoice.Memo)
	invoiceID := fmt.Sprintf("%x", invoice.RHash)
	amountMsat := invoice.ValueMsat
	if amountMsat == 0 {
		amountMsat = invoice.Value * 1000
	}

	expiresAt := time.Unix(invoice.CreationDate+invoice.Expiry, 0).UTC().Format(time.RFC3339)
	stateName := invoice.State.String()
	logger.WithDeviceID(deviceMeta.DeviceID).
		InfoWithFields(ctx, "Processing invoice update via cloud LND node", map[string]interface{}{
			"invoice_id":  invoiceID,
			"state":       stateName,
			"amount_msat": amountMsat,
		})

	switch invoice.State {
	case lnrpc.Invoice_OPEN, lnrpc.Invoice_ACCEPTED:
		lnInvoice := &lightningmodel.Invoice{
			InvoiceId:  invoiceID,
			DeviceId:   deviceMeta.DeviceID,
			Bolt11:     invoice.PaymentRequest,
			AmountMsat: amountMsat,
			Status:     mapInvoiceStatus(invoice.State),
			ExpiresAt:  expiresAt,
		}
		return &lightningmodel.LightningEvent{
			Type: lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_CREATED,
			Payload: &lightningmodel.LightningEvent_InvoiceCreated{
				InvoiceCreated: &lightningmodel.InvoiceCreatedEvent{
					Invoice: lnInvoice,
				},
			},
		}
	case lnrpc.Invoice_SETTLED:
		timestamp := time.Unix(invoice.SettleDate, 0).UTC().Format(time.RFC3339)
		logger.WithDeviceID(deviceMeta.DeviceID).
			InfoWithFields(ctx, "Invoice settled via cloud LND node", map[string]interface{}{
				"invoice_id":           invoiceID,
				"amount_received_msat": invoice.AmtPaidSat * 1000,
			})
		return &lightningmodel.LightningEvent{
			Type: lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_SETTLED,
			Payload: &lightningmodel.LightningEvent_InvoiceSettled{
				InvoiceSettled: &lightningmodel.InvoiceSettledEvent{
					InvoiceId:          invoiceID,
					DeviceId:           deviceMeta.DeviceID,
					AmountReceivedMsat: invoice.AmtPaidSat * 1000,
					NewBalanceMsat:     0,
					Timestamp:          timestamp,
				},
			},
		}
	case lnrpc.Invoice_CANCELED:
		timestamp := time.Unix(invoice.CreationDate+invoice.Expiry, 0).UTC().Format(time.RFC3339)
		logger.WithDeviceID(deviceMeta.DeviceID).
			InfoWithFields(ctx, "Invoice expired/canceled via cloud LND node", map[string]interface{}{
				"invoice_id": invoiceID,
			})
		return &lightningmodel.LightningEvent{
			Type: lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_EXPIRED,
			Payload: &lightningmodel.LightningEvent_InvoiceExpired{
				InvoiceExpired: &lightningmodel.InvoiceExpiredEvent{
					InvoiceId: invoiceID,
					DeviceId:  deviceMeta.DeviceID,
					Timestamp: timestamp,
				},
			},
		}
	default:
		logger.DebugWithFields(ctx, "Ignoring invoice update with unsupported state via cloud LND node", map[string]interface{}{
			"invoice_id": invoiceID,
			"state":      stateName,
		})
		return nil
	}
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
