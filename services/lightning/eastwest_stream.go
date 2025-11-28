package main

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/robertodantas/lnpay/internal"
	lightningmodel "github.com/robertodantas/lnpay/proto/gen/model/lightning"
)

const LightningEventsStream = "event.lightning"

type StreamPublisher struct {
	streamClient *internal.StreamClient
	lndStream    *LNDEventStream
}

func NewStreamPublisher(streamClient *internal.StreamClient, lndStream *LNDEventStream) *StreamPublisher {
	return &StreamPublisher{
		streamClient: streamClient,
		lndStream:    lndStream,
	}
}

// Start begins consuming LND events and publishing them to Redis.
func (sp *StreamPublisher) Start(ctx context.Context) error {
	eventChan := sp.lndStream.Subscribe()

	logger.WithStream(LightningEventsStream, "produce").
		Info(ctx, "Stream publisher started, forwarding events to Redis")

	go func() {
		defer sp.lndStream.Unsubscribe(eventChan)

		for {
			select {
			case <-ctx.Done():
				logger.WithStream(LightningEventsStream, "produce").
					Info(ctx, "Stream publisher stopped")
				return
			case eventWithCtx := <-eventChan:
				// Use the context from the event to maintain parent-child span relationship
				if err := sp.publishEvent(eventWithCtx.Context, eventWithCtx.Event); err != nil {
					logger.WithStream(LightningEventsStream, "produce").
						Error(eventWithCtx.Context, "Failed to publish lightning event", err)
				}
			}
		}
	}()

	return nil
}

func (sp *StreamPublisher) publishEvent(ctx context.Context, event *lightningmodel.LightningEvent) error {
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

	// Extract device_id from event if available
	deviceID := extractDeviceIDFromLightningEvent(event)
	logEntry := logger.WithStream(LightningEventsStream, "produce")
	if deviceID != "" {
		logEntry = logEntry.WithDeviceID(deviceID)
	}
	logEntry.InfoWithFields(ctx, "Published event to Redis stream", map[string]interface{}{
		"stream_id":  streamID,
		"event_type": event.GetType().String(),
	})
	return nil
}

// extractDeviceIDFromLightningEvent extracts device_id from various lightning event types
func extractDeviceIDFromLightningEvent(event *lightningmodel.LightningEvent) string {
	switch payload := event.GetPayload().(type) {
	case *lightningmodel.LightningEvent_InvoiceCreated:
		if payload.InvoiceCreated != nil && payload.InvoiceCreated.Invoice != nil {
			return payload.InvoiceCreated.Invoice.DeviceId
		}
	case *lightningmodel.LightningEvent_InvoiceSettled:
		if payload.InvoiceSettled != nil {
			return payload.InvoiceSettled.DeviceId
		}
	case *lightningmodel.LightningEvent_InvoiceExpired:
		if payload.InvoiceExpired != nil {
			return payload.InvoiceExpired.DeviceId
		}
	}
	return ""
}

// PublishInvoiceCreated publishes an invoice created event immediately after creation.
func (sp *StreamPublisher) PublishInvoiceCreated(ctx context.Context, invoice *lightningmodel.Invoice) error {
	event := &lightningmodel.LightningEvent{
		Type: lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_CREATED,
		Payload: &lightningmodel.LightningEvent_InvoiceCreated{
			InvoiceCreated: &lightningmodel.InvoiceCreatedEvent{
				Invoice: invoice,
			},
		},
	}
	return sp.publishEvent(ctx, event)
}
