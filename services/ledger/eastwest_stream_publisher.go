package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/robertodantas/lina/internal"
	ledgermodel "github.com/robertodantas/lina/proto/gen/model/ledger"
)

// EastWestStreamPublisher handles publishing messages to Redis streams for east-west communication
type EastWestStreamPublisher struct {
	streamClient *internal.StreamClient
}

type ledgerEventEnvelope struct {
	event    *ledgermodel.LedgerEvent
	deviceID string
}

// NewEastWestStreamPublisher creates a new east-west stream publisher
func NewEastWestStreamPublisher(streamClient *internal.StreamClient) *EastWestStreamPublisher {
	return &EastWestStreamPublisher{
		streamClient: streamClient,
	}
}

// PublishAuthorizationCreated publishes an AuthorizationCreated event to event.ledger
func (esp *EastWestStreamPublisher) PublishAuthorizationCreated(ctx context.Context, auth *ledgermodel.Authorization) error {
	event := &ledgermodel.AuthorizationCreatedEvent{
		Authorization: auth,
	}

	ledgerEvent := &ledgermodel.LedgerEvent{
		Type: ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_CREATED,
		Payload: &ledgermodel.LedgerEvent_AuthorizationCreated{
			AuthorizationCreated: event,
		},
	}

	deviceID := ""
	if auth != nil {
		deviceID = auth.DeviceId
	}

	return esp.publishLedgerEvent(ctx, ledgerEvent, deviceID)
}

// PublishAuthorizationCompleted publishes an AuthorizationCompleted event to event.ledger
func (esp *EastWestStreamPublisher) PublishAuthorizationCompleted(ctx context.Context, authorizationID, deviceID, timestamp string) error {
	return esp.publishLedgerEvent(ctx, newAuthorizationCompletedEvent(authorizationID, deviceID, timestamp), deviceID)
}

// PublishAuthorizationExpired publishes an AuthorizationExpired event to event.ledger
func (esp *EastWestStreamPublisher) PublishAuthorizationExpired(ctx context.Context, authorizationID, deviceID, timestamp string) error {
	event := &ledgermodel.AuthorizationExpiredEvent{
		AuthorizationId: authorizationID,
		DeviceId:        deviceID,
		Timestamp:       timestamp,
	}

	ledgerEvent := &ledgermodel.LedgerEvent{
		Type: ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_EXPIRED,
		Payload: &ledgermodel.LedgerEvent_AuthorizationExpired{
			AuthorizationExpired: event,
		},
	}

	return esp.publishLedgerEvent(ctx, ledgerEvent, deviceID)
}

// PublishDeviceCredited publishes a DeviceCreditedEvent to event.ledger
func (esp *EastWestStreamPublisher) PublishDeviceCredited(ctx context.Context, deviceID string, amountMsat, newBalanceMsat int64, timestamp string) error {
	event := &ledgermodel.DeviceCreditedEvent{
		DeviceId:       deviceID,
		AmountMsat:     amountMsat,
		NewBalanceMsat: newBalanceMsat,
		Timestamp:      timestamp,
	}

	ledgerEvent := &ledgermodel.LedgerEvent{
		Type: ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_DEVICE_CREDITED,
		Payload: &ledgermodel.LedgerEvent_DeviceCredited{
			DeviceCredited: event,
		},
	}

	return esp.publishLedgerEvent(ctx, ledgerEvent, deviceID)
}

// PublishDeviceDebited publishes a DeviceDebitedEvent to event.ledger
func (esp *EastWestStreamPublisher) PublishDeviceDebited(ctx context.Context, deviceID, authorizationID string, amountMsat, newBalanceMsat int64, timestamp string) error {
	return esp.publishLedgerEvent(ctx, newDeviceDebitedEvent(deviceID, authorizationID, amountMsat, newBalanceMsat, timestamp), deviceID)
}

// PublishAuthorizationDebited publishes an AuthorizationDebitedEvent to event.ledger
func (esp *EastWestStreamPublisher) PublishAuthorizationDebited(ctx context.Context, authorizationID, deviceID string, amountMsat, remainingMsat int64, timestamp string) error {
	return esp.publishLedgerEvent(ctx, newAuthorizationDebitedEvent(authorizationID, deviceID, amountMsat, remainingMsat, timestamp), deviceID)
}

// PublishAuthorizationDebitFailed publishes an AuthorizationDebitFailedEvent to event.ledger
func (esp *EastWestStreamPublisher) PublishAuthorizationDebitFailed(ctx context.Context, authorizationID, deviceID string, requestedMsat, remainingMsat int64, reason, timestamp string) error {
	event := &ledgermodel.AuthorizationDebitFailedEvent{
		AuthorizationId: authorizationID,
		DeviceId:        deviceID,
		RequestedMsat:   requestedMsat,
		RemainingMsat:   remainingMsat,
		Reason:          reason,
		Timestamp:       timestamp,
	}

	ledgerEvent := &ledgermodel.LedgerEvent{
		Type: ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_DEBIT_FAILED,
		Payload: &ledgermodel.LedgerEvent_AuthorizationDebitFailed{
			AuthorizationDebitFailed: event,
		},
	}

	return esp.publishLedgerEvent(ctx, ledgerEvent, deviceID)
}

func newAuthorizationDebitedEvent(authorizationID, deviceID string, amountMsat, remainingMsat int64, timestamp string) *ledgermodel.LedgerEvent {
	event := &ledgermodel.AuthorizationDebitedEvent{
		AuthorizationId: authorizationID,
		DeviceId:        deviceID,
		AmountMsat:      amountMsat,
		RemainingMsat:   remainingMsat,
		Timestamp:       timestamp,
	}

	return &ledgermodel.LedgerEvent{
		Type: ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_DEBITED,
		Payload: &ledgermodel.LedgerEvent_AuthorizationDebited{
			AuthorizationDebited: event,
		},
	}
}

func newAuthorizationCompletedEvent(authorizationID, deviceID, timestamp string) *ledgermodel.LedgerEvent {
	event := &ledgermodel.AuthorizationCompletedEvent{
		AuthorizationId: authorizationID,
		DeviceId:        deviceID,
		Timestamp:       timestamp,
	}

	return &ledgermodel.LedgerEvent{
		Type: ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_COMPLETED,
		Payload: &ledgermodel.LedgerEvent_AuthorizationCompleted{
			AuthorizationCompleted: event,
		},
	}
}

func newDeviceDebitedEvent(deviceID, authorizationID string, amountMsat, newBalanceMsat int64, timestamp string) *ledgermodel.LedgerEvent {
	event := &ledgermodel.DeviceDebitedEvent{
		DeviceId:        deviceID,
		AuthorizationId: authorizationID,
		AmountMsat:      amountMsat,
		NewBalanceMsat:  newBalanceMsat,
		Timestamp:       timestamp,
	}

	return &ledgermodel.LedgerEvent{
		Type: ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_DEVICE_DEBITED,
		Payload: &ledgermodel.LedgerEvent_DeviceDebited{
			DeviceDebited: event,
		},
	}
}

func (esp *EastWestStreamPublisher) publishLedgerEventsBatch(ctx context.Context, events []ledgerEventEnvelope) error {
	if len(events) == 0 {
		return nil
	}
	if len(events) == 1 {
		return esp.publishLedgerEvent(ctx, events[0].event, events[0].deviceID)
	}

	streamName := internal.StreamLedger
	pipe := esp.streamClient.Client().Pipeline()
	cmds := make([]*redis.StringCmd, 0, len(events))
	eventTypes := make([]string, 0, len(events))

	for _, event := range events {
		values, eventType, err := ledgerEventValues(ctx, event.event)
		if err != nil {
			return err
		}
		cmds = append(cmds, pipe.XAdd(ctx, &redis.XAddArgs{
			Stream: streamName,
			Values: values,
		}))
		eventTypes = append(eventTypes, eventType)
	}

	_, execErr := pipe.Exec(ctx)
	var firstErr error
	for i, cmd := range cmds {
		if err := cmd.Err(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			logger.WithStream(streamName, "produce").
				Errorf(ctx, "Failed to publish batched LedgerEvent type=%s: %v", eventTypes[i], err)
			continue
		}
		logEntry := logger.WithStream(streamName, "produce")
		if events[i].deviceID != "" {
			logEntry = logEntry.WithDeviceID(events[i].deviceID)
		}
		logEntry.DebugWithFields(ctx, "Published LedgerEvent", map[string]interface{}{
			"event_type": events[i].event.GetType().String(),
			"stream_id":  cmd.Val(),
		})
	}
	if firstErr != nil {
		return fmt.Errorf("failed to publish batched ledger event to Redis stream %s: %w", streamName, firstErr)
	}
	if execErr != nil {
		return fmt.Errorf("failed to publish batched ledger events to Redis stream %s: %w", streamName, execErr)
	}
	return nil
}

// publishLedgerEvent publishes a LedgerEvent to the event.ledger stream
func (esp *EastWestStreamPublisher) publishLedgerEvent(ctx context.Context, ledgerEvent *ledgermodel.LedgerEvent, deviceID string) error {
	values, eventType, err := ledgerEventValues(ctx, ledgerEvent)
	if err != nil {
		return err
	}

	streamName := internal.StreamLedger
	// Use XADD to add entry to stream
	streamID, err := esp.streamClient.XAddWithSpan(ctx, streamName, &redis.XAddArgs{
		Stream: streamName,
		Values: values,
	}, eventType)

	if err != nil {
		return fmt.Errorf("failed to publish to Redis stream %s: %w", streamName, err)
	}

	logEntry := logger.WithStream(streamName, "produce")
	if deviceID != "" {
		logEntry = logEntry.WithDeviceID(deviceID)
	}
	logEntry.DebugWithFields(ctx, "Published LedgerEvent", map[string]interface{}{
		"event_type": ledgerEvent.GetType().String(),
		"stream_id":  streamID,
	})
	return nil
}

func ledgerEventValues(ctx context.Context, ledgerEvent *ledgermodel.LedgerEvent) (map[string]interface{}, string, error) {
	eventStr, err := internal.MarshalStreamEvent(ledgerEvent)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal ledger event: %w", err)
	}

	eventType := strings.TrimPrefix(ledgerEvent.GetType().String(), "LEDGER_EVENT_TYPE_")
	values := map[string]interface{}{
		"event":      eventStr,
		"event_type": eventType,
		"timestamp":  time.Now().UnixMilli(),
	}
	internal.InjectTraceContext(ctx, values)
	return values, eventType, nil
}
