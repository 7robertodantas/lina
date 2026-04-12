package main

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/robertodantas/lina/internal"
	lightningmodel "github.com/robertodantas/lina/proto/gen/model/lightning"
)

const LightningEventsStream = "event.lightning"

type EastWestStreamPublisher struct {
	streamClient *internal.StreamClient
}

func NewEastWestStreamPublisher(streamClient *internal.StreamClient) *EastWestStreamPublisher {
	return &EastWestStreamPublisher{
		streamClient: streamClient,
	}
}

// PublishInvoiceCreated publishes an invoice created event.
func (sp *EastWestStreamPublisher) PublishInvoiceCreated(ctx context.Context, invoice *lightningmodel.Invoice) error {
	event := &lightningmodel.LightningEvent{
		Type: lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_CREATED,
		Payload: &lightningmodel.LightningEvent_InvoiceCreated{
			InvoiceCreated: &lightningmodel.InvoiceCreatedEvent{
				Invoice: invoice,
			},
		},
	}
	return sp.publishEvent(ctx, event, invoice.DeviceId)
}

// PublishInvoiceSettled publishes an invoice settled event.
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
	return sp.publishEvent(ctx, event, deviceID)
}

// PublishInvoiceExpired publishes an invoice expired event.
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
	return sp.publishEvent(ctx, event, deviceID)
}

func (sp *EastWestStreamPublisher) publishEvent(ctx context.Context, event *lightningmodel.LightningEvent, deviceID string) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	opts := protojson.MarshalOptions{UseProtoNames: true}
	payload, err := opts.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal lightning event: %w", err)
	}

	values := map[string]interface{}{
		"event":     string(payload),
		"timestamp": time.Now().UnixMilli(),
	}

	args := &redis.XAddArgs{
		Stream: LightningEventsStream,
		Values: values,
	}

	// Extract event type for span naming
	eventTypeFull := event.GetType().String()
	eventType := eventTypeFull
	if len(eventTypeFull) > len("LIGHTNING_EVENT_TYPE_") && eventTypeFull[:len("LIGHTNING_EVENT_TYPE_")] == "LIGHTNING_EVENT_TYPE_" {
		eventType = eventTypeFull[len("LIGHTNING_EVENT_TYPE_"):]
	}

	streamID, err := sp.streamClient.XAddWithSpan(ctx, LightningEventsStream, args, eventType)
	if err != nil {
		return fmt.Errorf("failed to publish lightning event: %w", err)
	}

	logEntry := logger.WithStream(LightningEventsStream, "produce")
	if deviceID != "" {
		logEntry = logEntry.WithDeviceID(deviceID)
	}
	logEntry.DebugWithFields(ctx, "Published event to Redis stream", map[string]interface{}{
		"stream_id":  streamID,
		"event_type": event.GetType().String(),
	})
	return nil
}
