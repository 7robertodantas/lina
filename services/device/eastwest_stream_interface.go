package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/robertodantas/lina/internal"
	ledgermodel "github.com/robertodantas/lina/proto/gen/model/ledger"
	lightningmodel "github.com/robertodantas/lina/proto/gen/model/lightning"
)

// Consumer group for event.lightning + event.lightning.ephemeral (same pattern as ledger-consumers / consumption-consumers; not env-tunable).
const deviceStreamGroup = "device-consumers"

// EastWestStreamInterface wraps the internal StreamClient with device-specific methods for east-west stream communication
type EastWestStreamInterface struct {
	*internal.StreamClient
	streamConsumerName string
	consumeParallelism int
	streamReadCount    int
}

// NewEastWestStreamInterface creates a new Redis stream client using the internal package
func NewEastWestStreamInterface(ctx context.Context, cfg Config) (*EastWestStreamInterface, error) {
	libClient, err := internal.NewStreamClientFromEnv(ctx)
	if err != nil {
		return nil, err
	}

	cname := cfg.StreamConsumerName
	if cname == "" {
		cname = defaultDeviceStreamConsumerName()
	}

	return &EastWestStreamInterface{
		StreamClient:       libClient,
		streamConsumerName: cname,
		consumeParallelism: cfg.ConsumeParallelism,
		streamReadCount:    cfg.StreamReadCount,
	}, nil
}

func defaultDeviceStreamConsumerName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("device-%s-%d", host, os.Getpid())
}

func busyGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}

// StartLedgerBalanceSubscriber listens for ledger balance events and forwards updates via MQTT
func (ewsi *EastWestStreamInterface) StartLedgerBalanceSubscriber(ctx context.Context, publisher *SouthboundPublisher) {
	// Create handler for processing ledger events
	handler := NewEastWestStreamHandler(publisher)
	go ewsi.consumeLedgerBalanceEvents(ctx, handler)
}

func (ewsi *EastWestStreamInterface) consumeLedgerBalanceEvents(ctx context.Context, handler *EastWestStreamHandler) {
	streamName := internal.StreamLedger
	lastID := "$"

	logger.WithStream(streamName, "consume").
		Info(ctx, "Starting ledger balance subscriber")

	for {
		select {
		case <-ctx.Done():
			logger.WithStream(streamName, "consume").
				Info(ctx, "Stopping ledger balance subscriber")
			return
		default:
		}

		streams, err := ewsi.XReadWithSpan(ctx, streamName, &redis.XReadArgs{
			Streams: []string{streamName, lastID},
			Count:   int64(ewsi.streamReadCount),
			Block:   5 * time.Second,
		})
		if err != nil {
			if err == redis.Nil {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			logger.WithStream(streamName, "consume").
				Error(ctx, "Ledger balance subscriber read error", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for _, stream := range streams {
			msgs := stream.Messages
			if len(msgs) == 0 {
				continue
			}
			lastID = msgs[len(msgs)-1].ID

			internal.RunStreamMessagesParallel(ewsi.consumeParallelism, msgs, func(msg redis.XMessage) {
				// Wrap message handling with tracing; XDEL after success keeps event.ledger bounded (single consumer).
				if err := internal.TraceEventProcessing(ctx, streamName, msg, func(ctx context.Context, msg redis.XMessage) error {
					raw, ok := msg.Values["event"].(string)
					if !ok {
						return fmt.Errorf("ledger message missing event field")
					}
					return ewsi.handleLedgerMessage(ctx, handler, raw)
				}, nil); err != nil {
					logger.WithStream(streamName, "consume").
						Errorf(ctx, "Failed to handle ledger message %s: %v", msg.ID, err)
				} else {
					if err := ewsi.XDelWithSpan(ctx, streamName, msg.ID); err != nil {
						logger.WithStream(streamName, "consume").
							Warnf(ctx, "XDEL after successful ledger event processing failed for %s: %v", msg.ID, err)
					}
				}
			})
		}
	}
}

// StartLightningInvoiceSubscriber listens for lightning invoice events and forwards updates via MQTT
func (ewsi *EastWestStreamInterface) StartLightningInvoiceSubscriber(ctx context.Context, publisher *SouthboundPublisher) {
	handler := NewEastWestStreamHandler(publisher)
	streamCtx := ewsi.Context()
	for _, name := range []string{internal.StreamLightning, internal.StreamLightningEphemeral} {
		err := ewsi.XGroupCreateMkStreamWithSpan(streamCtx, name, deviceStreamGroup, "0")
		if err != nil && !busyGroup(err) {
			logger.WithStream(internal.StreamLightning, "consume").
				Errorf(ctx, "Failed to create device lightning consumer group on %s: %v", name, err)
			return
		}
	}
	go ewsi.startDeviceLightningPendingRetry(ctx, internal.StreamLightning, handler)
	go ewsi.startDeviceLightningPendingRetry(ctx, internal.StreamLightningEphemeral, handler)
	go ewsi.consumeLightningInvoiceEvents(ctx, handler)
}

func (ewsi *EastWestStreamInterface) consumeLightningInvoiceEvents(ctx context.Context, handler *EastWestStreamHandler) {
	streamCtx := ewsi.Context()

	logger.WithStream(internal.StreamLightning, "consume").
		Infof(ctx, "Starting lightning invoice subscriber (XREADGROUP group=%s consumer=%s parallelism=%d count=%d)",
			deviceStreamGroup, ewsi.streamConsumerName, ewsi.consumeParallelism, ewsi.streamReadCount)

	for {
		select {
		case <-ctx.Done():
			logger.WithStream(internal.StreamLightning, "consume").
				Info(ctx, "Stopping lightning invoice subscriber")
			return
		default:
		}

		streams, err := ewsi.XReadGroupWithSpan(streamCtx, internal.StreamLightning, deviceStreamGroup, ewsi.streamConsumerName, &redis.XReadGroupArgs{
			Group:    deviceStreamGroup,
			Consumer: ewsi.streamConsumerName,
			Streams: []string{
				internal.StreamLightning,
				internal.StreamLightningEphemeral,
				">", ">",
			},
			Count: int64(ewsi.streamReadCount),
			Block: 5 * time.Second,
		})
		if err != nil {
			if err == redis.Nil {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			logger.WithStream(internal.StreamLightning, "consume").
				Error(ctx, "Lightning invoice subscriber read error", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for _, stream := range streams {
			streamName := stream.Stream
			msgs := stream.Messages
			if len(msgs) == 0 {
				continue
			}
			internal.RunStreamMessagesParallel(ewsi.consumeParallelism, msgs, func(msg redis.XMessage) {
				ewsi.processDeviceLightningMessage(streamCtx, streamName, msg, handler)
			})
		}
	}
}

func (ewsi *EastWestStreamInterface) processDeviceLightningMessage(streamCtx context.Context, streamName string, msg redis.XMessage, handler *EastWestStreamHandler) {
	ackFn := func(ctx context.Context, msg redis.XMessage) error {
		if err := ewsi.XAckWithSpan(streamCtx, streamName, deviceStreamGroup, msg.ID, &msg); err != nil {
			return err
		}
		if streamName == internal.StreamLightningEphemeral {
			if err := ewsi.XDelWithSpan(streamCtx, streamName, msg.ID); err != nil {
				logger.WithStream(streamName, "consume").
					Warnf(streamCtx, "XDEL after successful ephemeral lightning event failed for %s: %v", msg.ID, err)
			}
		}
		return nil
	}
	err := internal.TraceEventProcessing(streamCtx, streamName, msg, func(ctx context.Context, msg redis.XMessage) error {
		raw, ok := msg.Values["event"].(string)
		if !ok {
			return fmt.Errorf("lightning message missing event field")
		}
		return ewsi.handleLightningMessage(ctx, handler, raw)
	}, ackFn)
	if err != nil {
		logger.WithStream(streamName, "consume").
			Errorf(streamCtx, "Failed to handle lightning message %s: %v", msg.ID, err)
	}
}

// startDeviceLightningPendingRetry claims idle pending messages for this stream (same pattern as ledger).
func (ewsi *EastWestStreamInterface) startDeviceLightningPendingRetry(ctx context.Context, streamName string, handler *EastWestStreamHandler) {
	streamCtx := ewsi.Context()
	retryConsumerName := ewsi.streamConsumerName + "-retry"
	logger.WithStream(streamName, "consume").
		Infof(streamCtx, "Starting device lightning pending retry (stream=%s)", streamName)

	client := ewsi.Client()
	minIdleTime := 5 * time.Second

	for {
		select {
		case <-ctx.Done():
			logger.WithStream(streamName, "consume").
				Info(streamCtx, "Stopping device lightning pending retry")
			return
		default:
			pending, err := client.XPendingExt(ctx, &redis.XPendingExtArgs{
				Stream:   streamName,
				Group:    deviceStreamGroup,
				Start:    "-",
				End:      "+",
				Count:    10,
				Consumer: ewsi.streamConsumerName,
			}).Result()
			if err != nil {
				if err == redis.Nil {
					time.Sleep(1 * time.Second)
					continue
				}
				logger.WithStream(streamName, "consume").
					Errorf(streamCtx, "Error checking pending device lightning messages: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}

			var messageIDs []string
			for _, p := range pending {
				if p.Idle >= minIdleTime {
					messageIDs = append(messageIDs, p.ID)
				}
			}
			if len(messageIDs) == 0 {
				time.Sleep(1 * time.Second)
				continue
			}

			claimed, err := client.XClaim(ctx, &redis.XClaimArgs{
				Stream:   streamName,
				Group:    deviceStreamGroup,
				Consumer: retryConsumerName,
				MinIdle:  minIdleTime,
				Messages: messageIDs,
			}).Result()
			if err != nil {
				logger.WithStream(streamName, "consume").
					Errorf(streamCtx, "Error claiming pending device lightning messages: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}

			for _, msg := range claimed {
				ewsi.processDeviceLightningMessage(streamCtx, streamName, msg, handler)
			}
		}
	}
}

// handleLedgerMessage decodes ledger event and calls appropriate handler method
func (ewsi *EastWestStreamInterface) handleLedgerMessage(ctx context.Context, handler *EastWestStreamHandler, rawEvent string) error {
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}

	var ledgerEvent ledgermodel.LedgerEvent
	if err := opts.Unmarshal([]byte(rawEvent), &ledgerEvent); err != nil {
		return fmt.Errorf("failed to unmarshal ledger event: %w", err)
	}

	switch ledgerEvent.GetType() {
	case ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_DEVICE_CREDITED:
		payload := ledgerEvent.GetDeviceCredited()
		if payload == nil {
			return fmt.Errorf("ledger event missing DeviceCredited payload")
		}
		return handler.HandleDeviceCredited(ctx, payload)

	case ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_DEVICE_DEBITED:
		payload := ledgerEvent.GetDeviceDebited()
		if payload == nil {
			return fmt.Errorf("ledger event missing DeviceDebited payload")
		}
		return handler.HandleDeviceDebited(ctx, payload)

	case ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_COMPLETED:
		payload := ledgerEvent.GetAuthorizationCompleted()
		if payload == nil {
			return fmt.Errorf("ledger event missing AuthorizationCompleted payload")
		}
		return handler.HandleAuthorizationCompleted(ctx, payload)

	case ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_EXPIRED:
		payload := ledgerEvent.GetAuthorizationExpired()
		if payload == nil {
			return fmt.Errorf("ledger event missing AuthorizationExpired payload")
		}
		return handler.HandleAuthorizationExpired(ctx, payload)

	case ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_DEBIT_FAILED:
		payload := ledgerEvent.GetAuthorizationDebitFailed()
		if payload == nil {
			return fmt.Errorf("ledger event missing AuthorizationDebitFailed payload")
		}
		return handler.HandleAuthorizationDebitFailed(ctx, payload)

	default:
		return nil
	}
}

// handleLightningMessage decodes lightning event and calls appropriate handler method
func (ewsi *EastWestStreamInterface) handleLightningMessage(ctx context.Context, handler *EastWestStreamHandler, rawEvent string) error {
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}

	var lightningEvent lightningmodel.LightningEvent
	if err := opts.Unmarshal([]byte(rawEvent), &lightningEvent); err != nil {
		return fmt.Errorf("failed to unmarshal lightning event: %w", err)
	}

	switch lightningEvent.GetType() {
	case lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_SETTLED:
		payload := lightningEvent.GetInvoiceSettled()
		if payload == nil {
			return fmt.Errorf("lightning event missing InvoiceSettled payload")
		}
		return handler.HandleInvoiceSettled(ctx, payload)

	case lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_EXPIRED:
		payload := lightningEvent.GetInvoiceExpired()
		if payload == nil {
			return fmt.Errorf("lightning event missing InvoiceExpired payload")
		}
		return handler.HandleInvoiceExpired(ctx, payload)

	default:
		logger.DebugWithFields(ctx, "Ignoring lightning event type", map[string]interface{}{
			"type": lightningEvent.GetType().String(),
		})
		return nil
	}
}

// Close closes the Redis client connection (delegates to embedded internal client)
func (ewsi *EastWestStreamInterface) Close() error {
	return ewsi.StreamClient.Close()
}
