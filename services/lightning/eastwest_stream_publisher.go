package main

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/robertodantas/lina/internal"
	lightningmodel "github.com/robertodantas/lina/proto/gen/model/lightning"
)

type EastWestStreamPublisher struct {
	streamClient       *internal.StreamClient
	ephemeralRetention time.Duration
}

func NewEastWestStreamPublisher(streamClient *internal.StreamClient, ephemeralRetention time.Duration) *EastWestStreamPublisher {
	if ephemeralRetention <= 0 {
		ephemeralRetention = time.Minute
	}
	return &EastWestStreamPublisher{
		streamClient:       streamClient,
		ephemeralRetention: ephemeralRetention,
	}
}

// PublishInvoiceCreated publishes an invoice created event to the ephemeral stream (time-trimmed).
func (sp *EastWestStreamPublisher) PublishInvoiceCreated(ctx context.Context, invoice *lightningmodel.Invoice) error {
	event := &lightningmodel.LightningEvent{
		Type: lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_CREATED,
		Payload: &lightningmodel.LightningEvent_InvoiceCreated{
			InvoiceCreated: &lightningmodel.InvoiceCreatedEvent{
				Invoice: invoice,
			},
		},
	}
	return sp.publishToStream(ctx, internal.StreamLightningEphemeral, event, invoice.DeviceId)
}

// PublishInvoiceSettled publishes an invoice settled event to the durable stream (ledger processing).
func (sp *EastWestStreamPublisher) PublishInvoiceSettled(ctx context.Context, invoiceID, deviceID string, amountReceivedMsat int64, timestamp string) error {
	event := &lightningmodel.LightningEvent{
		Type: lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_SETTLED,
		Payload: &lightningmodel.LightningEvent_InvoiceSettled{
			InvoiceSettled: &lightningmodel.InvoiceSettledEvent{
				InvoiceId:          invoiceID,
				DeviceId:           deviceID,
				AmountReceivedMsat: amountReceivedMsat,
				NewBalanceMsat:     0,
				Timestamp:          timestamp,
			},
		},
	}
	return sp.publishToStream(ctx, internal.StreamLightning, event, deviceID)
}

// PublishInvoiceExpired publishes an invoice expired event to the ephemeral stream (time-trimmed).
func (sp *EastWestStreamPublisher) PublishInvoiceExpired(ctx context.Context, invoiceID, deviceID, timestamp string) error {
	event := &lightningmodel.LightningEvent{
		Type: lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_EXPIRED,
		Payload: &lightningmodel.LightningEvent_InvoiceExpired{
			InvoiceExpired: &lightningmodel.InvoiceExpiredEvent{
				InvoiceId: invoiceID,
				DeviceId:  deviceID,
				Timestamp: timestamp,
			},
		},
	}
	return sp.publishToStream(ctx, internal.StreamLightningEphemeral, event, deviceID)
}

func (sp *EastWestStreamPublisher) publishToStream(ctx context.Context, streamName string, event *lightningmodel.LightningEvent, deviceID string) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	payload, err := internal.MarshalStreamEvent(event)
	if err != nil {
		return fmt.Errorf("failed to marshal lightning event: %w", err)
	}

	eventTypeFull := event.GetType().String()
	eventType := eventTypeFull
	if len(eventTypeFull) > len("LIGHTNING_EVENT_TYPE_") && eventTypeFull[:len("LIGHTNING_EVENT_TYPE_")] == "LIGHTNING_EVENT_TYPE_" {
		eventType = eventTypeFull[len("LIGHTNING_EVENT_TYPE_"):]
	}

	values := map[string]interface{}{
		"event":      payload,
		"event_type": eventType,
		"timestamp":  time.Now().UnixMilli(),
	}

	args := &redis.XAddArgs{
		Stream: streamName,
		Values: values,
	}

	streamID, err := sp.streamClient.XAddWithSpan(ctx, streamName, args, eventType)
	if err != nil {
		return fmt.Errorf("failed to publish lightning event: %w", err)
	}

	if streamName == internal.StreamLightningEphemeral {
		if trimErr := sp.trimEphemeralStream(ctx); trimErr != nil {
			logger.WithStream(streamName, "produce").
				Warnf(ctx, "Ephemeral stream trim after XADD failed (non-fatal): %v", trimErr)
		}
	}

	logEntry := logger.WithStream(streamName, "produce")
	if deviceID != "" {
		logEntry = logEntry.WithDeviceID(deviceID)
	}
	logEntry.DebugWithFields(ctx, "Published event to Redis stream", map[string]interface{}{
		"stream_id":  streamID,
		"event_type": event.GetType().String(),
	})
	return nil
}

// trimEphemeralStream drops entries older than ephemeralRetention using XTRIM MINID.
// Best-effort: may remove entries before a slow XREAD subscriber sees them; acceptable for created/expired only.
func (sp *EastWestStreamPublisher) trimEphemeralStream(ctx context.Context) error {
	minMs := time.Now().Add(-sp.ephemeralRetention).UnixMilli()
	if minMs < 0 {
		minMs = 0
	}
	minID := fmt.Sprintf("%d-0", minMs)
	_, err := sp.streamClient.Client().XTrimMinID(ctx, internal.StreamLightningEphemeral, minID).Result()
	return err
}
