package main

import (
	"context"
	"fmt"
	"log"
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

	log.Println("Stream publisher started, forwarding events to Redis...")

	go func() {
		defer sp.lndStream.Unsubscribe(eventChan)

		for {
			select {
			case <-ctx.Done():
				log.Println("Stream publisher stopped")
				return
			case event := <-eventChan:
				if err := sp.publishEvent(ctx, event); err != nil {
					log.Printf("failed to publish lightning event: %v", err)
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

	result := sp.streamClient.Client().XAdd(ctx, args)
	if err := result.Err(); err != nil {
		return fmt.Errorf("failed to publish lightning event: %w", err)
	}

	log.Printf("Published event to Redis stream %s (ID: %s)", LightningEventsStream, result.Val())
	return nil
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
