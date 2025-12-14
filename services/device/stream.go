package main

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/robertodantas/lnpay/internal"
	devicepb "github.com/robertodantas/lnpay/proto/gen/model/device"
	ledgermodel "github.com/robertodantas/lnpay/proto/gen/model/ledger"
	lightningmodel "github.com/robertodantas/lnpay/proto/gen/model/lightning"
	mqttpb "github.com/robertodantas/lnpay/proto/gen/model/mqtt"
)

// StreamClient wraps the internal StreamClient with device-specific methods
type StreamClient struct {
	*internal.StreamClient
}

// NewStreamClient creates a new Redis stream client using the internal package
func NewStreamClient(ctx context.Context) (*StreamClient, error) {
	libClient, err := internal.NewStreamClientFromEnv(ctx)
	if err != nil {
		return nil, err
	}

	return &StreamClient{
		StreamClient: libClient,
	}, nil
}

// convertReportingStrategy converts MQTT ReportingStrategy to device UsageReportingStrategy
func convertReportingStrategy(strategy mqttpb.ReportingStrategy) devicepb.UsageReportingStrategy {
	switch strategy {
	case mqttpb.ReportingStrategy_REPORTING_STRATEGY_INTERVAL:
		return devicepb.UsageReportingStrategy_USAGE_STRATEGY_INTERVAL
	case mqttpb.ReportingStrategy_REPORTING_STRATEGY_DELTA:
		return devicepb.UsageReportingStrategy_USAGE_STRATEGY_DELTA
	case mqttpb.ReportingStrategy_REPORTING_STRATEGY_TOTAL:
		return devicepb.UsageReportingStrategy_USAGE_STRATEGY_TOTAL
	default:
		return devicepb.UsageReportingStrategy_USAGE_STRATEGY_UNSPECIFIED
	}
}

// PublishDeviceUsageReportedEvent publishes a DeviceEvent containing DeviceUsageReportedEvent to the Redis stream
// It fetches device config from the repository to append price_per_unit_msat
func (sc *StreamClient) PublishDeviceUsageReportedEvent(ctx context.Context, payload *mqttpb.UsagePayload, repo *DeviceRepository) error {
	// Fetch device config to get price_per_unit
	device, err := repo.GetDevice(ctx, payload.GetDeviceId())
	if err != nil {
		return fmt.Errorf("failed to get device config for %s: %w", payload.GetDeviceId(), err)
	}

	// Use unit_price_msat directly (already in msat)
	pricePerUnitMsat := device.UnitPriceMsat

	// Convert MQTT UsagePayload to device UsageRecord
	usageRecord := &devicepb.UsageRecord{
		DeviceId:         payload.GetDeviceId(),
		ReportId:         payload.GetReportId(),
		Strategy:         convertReportingStrategy(payload.GetStrategy()),
		Measure:          payload.GetMeasure(),
		Unit:             payload.GetUnit(),
		Timestamp:        payload.GetTimestamp(),
		PricePerUnitMsat: pricePerUnitMsat,
	}

	// Create the DeviceUsageReportedEvent
	usageReportedEvent := &devicepb.DeviceUsageReportedEvent{
		Usage: usageRecord,
	}

	// Wrap in DeviceEvent envelope
	deviceEvent := &devicepb.DeviceEvent{
		Type: devicepb.DeviceEventType_DEVICE_EVENT_TYPE_USAGE_REPORTED,
		Payload: &devicepb.DeviceEvent_UsageReported{
			UsageReported: usageReportedEvent,
		},
	}

	// Serialize to JSON for Redis stream
	opts := protojson.MarshalOptions{UseProtoNames: true}
	jsonBytes, err := opts.Marshal(deviceEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal device event to JSON: %w", err)
	}

	// Publish to Redis stream "event.device"
	streamName := "event.device"
	values := map[string]interface{}{
		"event":     string(jsonBytes),
		"timestamp": time.Now().UnixMilli(),
	}

	// Use XAddWithSpan to add entry to stream with tracing
	streamID, err := sc.XAddWithSpan(ctx, streamName, &redis.XAddArgs{
		Stream: streamName,
		Values: values,
	}, "USAGE_REPORTED")

	if err != nil {
		return fmt.Errorf("failed to publish to Redis stream %s: %w", streamName, err)
	}

	logger.WithDeviceID(payload.GetDeviceId()).
		WithStream(streamName, "produce").
		InfoWithFields(ctx, "Published DeviceEvent (usage reported) on southbound mqtt", map[string]interface{}{
			"stream_id": streamID,
			"report_id": payload.GetReportId(),
		})
	return nil
}

// StartLedgerBalanceSubscriber listens for ledger balance events and forwards updates via MQTT
func (sc *StreamClient) StartLedgerBalanceSubscriber(ctx context.Context, mqttClient *MQTTClient) {
	go sc.consumeLedgerBalanceEvents(ctx, mqttClient)
}

func (sc *StreamClient) consumeLedgerBalanceEvents(ctx context.Context, mqttClient *MQTTClient) {
	streamName := "event.ledger"
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

		streams, err := sc.XReadWithSpan(ctx, streamName, &redis.XReadArgs{
			Streams: []string{streamName, lastID},
			Count:   20,
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
			for _, msg := range stream.Messages {
				lastID = msg.ID

				// Wrap message handling with tracing (no ack needed for XRead)
				if err := internal.TraceEventProcessing(ctx, streamName, msg, func(ctx context.Context, msg redis.XMessage) error {
					opts := protojson.UnmarshalOptions{DiscardUnknown: true}
					return sc.handleLedgerMessage(ctx, mqttClient, msg, opts)
				}, nil); err != nil {
					logger.WithStream(streamName, "consume").
						Errorf(ctx, "Failed to handle ledger message %s: %v", msg.ID, err)
				}
			}
		}
	}
}

func (sc *StreamClient) handleLedgerMessage(ctx context.Context, mqttClient *MQTTClient, msg redis.XMessage, opts protojson.UnmarshalOptions) error {
	raw, ok := msg.Values["event"].(string)
	if !ok {
		return fmt.Errorf("ledger message missing event field")
	}

	var ledgerEvent ledgermodel.LedgerEvent
	if err := opts.Unmarshal([]byte(raw), &ledgerEvent); err != nil {
		return fmt.Errorf("failed to unmarshal ledger event: %w", err)
	}

	switch ledgerEvent.GetType() {
	case ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_DEVICE_CREDITED:
		payload := ledgerEvent.GetDeviceCredited()
		if payload == nil {
			return fmt.Errorf("ledger event missing DeviceCredited payload")
		}
		return sc.publishBalanceUpdate(ctx, mqttClient, payload.GetDeviceId(), payload.GetNewBalanceMsat(), payload.GetTimestamp())
	case ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_DEVICE_DEBITED:
		payload := ledgerEvent.GetDeviceDebited()
		if payload == nil {
			return fmt.Errorf("ledger event missing DeviceDebited payload")
		}
		logger.WithDeviceID(payload.GetDeviceId()).
			InfoWithFields(ctx, "Device debited via eastwest gRPC", map[string]interface{}{
				"authorization_id": payload.GetAuthorizationId(),
				"amount_msat":      payload.GetAmountMsat(),
				"new_balance_msat": payload.GetNewBalanceMsat(),
			})
		return sc.publishBalanceUpdate(ctx, mqttClient, payload.GetDeviceId(), payload.GetNewBalanceMsat(), payload.GetTimestamp())
	case ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_COMPLETED:
		payload := ledgerEvent.GetAuthorizationCompleted()
		if payload == nil {
			return fmt.Errorf("ledger event missing AuthorizationCompleted payload")
		}
		return sc.publishAuthorizationControl(ctx, mqttClient, payload.GetDeviceId(), payload.GetAuthorizationId(), "COMPLETED")
	case ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_EXPIRED:
		payload := ledgerEvent.GetAuthorizationExpired()
		if payload == nil {
			return fmt.Errorf("ledger event missing AuthorizationExpired payload")
		}
		return sc.publishAuthorizationControl(ctx, mqttClient, payload.GetDeviceId(), payload.GetAuthorizationId(), "EXPIRED")
	case ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_DEBIT_FAILED:
		payload := ledgerEvent.GetAuthorizationDebitFailed()
		if payload == nil {
			return fmt.Errorf("ledger event missing AuthorizationDebitFailed payload")
		}
		logger.WithDeviceID(payload.GetDeviceId()).
			WarnWithFields(ctx, "Authorization debit failed via eastwest gRPC", map[string]interface{}{
				"authorization_id": payload.GetAuthorizationId(),
				"reason":           payload.GetReason(),
				"requested_msat":   payload.GetRequestedMsat(),
				"remaining_msat":   payload.GetRemainingMsat(),
			})
		return sc.publishAuthorizationControl(ctx, mqttClient, payload.GetDeviceId(), payload.GetAuthorizationId(), "AUTHORIZE")
	default:
		return nil
	}
}

func (sc *StreamClient) publishBalanceUpdate(ctx context.Context, mqttClient *MQTTClient, deviceID string, availableMsat int64, ts string) error {
	if deviceID == "" {
		return fmt.Errorf("ledger event missing device_id")
	}
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}

	payload := &mqttpb.BalancePayload{
		DeviceId:      deviceID,
		AvailableMsat: availableMsat,
		ReservedMsat:  0,
		TotalMsat:     availableMsat,
		Timestamp:     ts,
	}

	marshalOpts := protojson.MarshalOptions{UseProtoNames: true}
	msgBytes, err := marshalOpts.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal balance payload: %w", err)
	}

	topic := fmt.Sprintf("/devices/%s/balance", deviceID)
	if err := mqttClient.Publish(ctx, topic, 1, true, msgBytes); err != nil {
		return fmt.Errorf("failed to publish balance to MQTT: %w", err)
	}

	logger.WithDeviceID(deviceID).
		InfoWithFields(ctx, "Published updated balance on southbound mqtt", map[string]interface{}{
			"available_msat": availableMsat,
		})
	return nil
}

// publishAuthorizationControl publishes an AUTHORIZATION control command to the device
func (sc *StreamClient) publishAuthorizationControl(ctx context.Context, mqttClient *MQTTClient, deviceID string, authorizationID string, reason string) error {
	if deviceID == "" {
		return fmt.Errorf("ledger event missing device_id")
	}

	payload := &mqttpb.ControlPayload{
		Command:         mqttpb.ControlCommand_CONTROL_COMMAND_AUTHORIZATION,
		Reason:          reason,
		AuthorizationId: authorizationID,
	}

	marshalOpts := protojson.MarshalOptions{UseProtoNames: true}
	msgBytes, err := marshalOpts.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal control payload: %w", err)
	}

	topic := fmt.Sprintf("/devices/%s/control", deviceID)
	if err := mqttClient.Publish(ctx, topic, 1, false, msgBytes); err != nil {
		return fmt.Errorf("failed to publish control command to MQTT: %w", err)
	}

	logger.WithDeviceID(deviceID).
		InfoWithFields(ctx, "Published AUTHORIZATION control on southbound mqtt", map[string]interface{}{
			"authorization_id": authorizationID,
			"reason":           reason,
		})
	return nil
}

// StartLightningInvoiceSubscriber listens for lightning invoice events and forwards updates via MQTT
func (sc *StreamClient) StartLightningInvoiceSubscriber(ctx context.Context, mqttClient *MQTTClient) {
	go sc.consumeLightningInvoiceEvents(ctx, mqttClient)
}

func (sc *StreamClient) consumeLightningInvoiceEvents(ctx context.Context, mqttClient *MQTTClient) {
	streamName := "event.lightning"
	lastID := "$"

	logger.WithStream(streamName, "consume").
		Info(ctx, "Starting lightning invoice subscriber")

	for {
		select {
		case <-ctx.Done():
			logger.WithStream(streamName, "consume").
				Info(ctx, "Stopping lightning invoice subscriber")
			return
		default:
		}

		streams, err := sc.XReadWithSpan(ctx, streamName, &redis.XReadArgs{
			Streams: []string{streamName, lastID},
			Count:   20,
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
				Error(ctx, "Lightning invoice subscriber read error", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				lastID = msg.ID

				// Wrap message handling with tracing
				if err := internal.TraceEventProcessing(ctx, streamName, msg, func(ctx context.Context, msg redis.XMessage) error {
					opts := protojson.UnmarshalOptions{DiscardUnknown: true}
					return sc.handleLightningMessage(ctx, mqttClient, msg, opts)
				}, nil); err != nil {
					logger.WithStream(streamName, "consume").
						Errorf(ctx, "Failed to handle lightning message %s: %v", msg.ID, err)
				}
			}
		}
	}
}

func (sc *StreamClient) handleLightningMessage(ctx context.Context, mqttClient *MQTTClient, msg redis.XMessage, opts protojson.UnmarshalOptions) error {
	raw, ok := msg.Values["event"].(string)
	if !ok {
		return fmt.Errorf("lightning message missing event field")
	}

	var lightningEvent lightningmodel.LightningEvent
	if err := opts.Unmarshal([]byte(raw), &lightningEvent); err != nil {
		return fmt.Errorf("failed to unmarshal lightning event: %w", err)
	}

	switch lightningEvent.GetType() {
	case lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_SETTLED:
		payload := lightningEvent.GetInvoiceSettled()
		if payload == nil {
			return fmt.Errorf("lightning event missing InvoiceSettled payload")
		}
		logger.WithDeviceID(payload.GetDeviceId()).
			InfoWithFields(ctx, "Processing InvoiceSettled event from lightning stream", map[string]interface{}{
				"invoice_id": payload.GetInvoiceId(),
				"amount_msat": payload.GetAmountReceivedMsat(),
			})
		// Publish invoice event
		if err := sc.publishInvoiceEvent(ctx, mqttClient, payload.GetDeviceId(), payload.GetInvoiceId(), mqttpb.InvoiceStatus_INVOICE_STATUS_SETTLED, payload.GetAmountReceivedMsat(), payload.GetNewBalanceMsat(), payload.GetTimestamp()); err != nil {
			return err
		}
		// Note: Balance update will be published when DEVICE_CREDITED event is received from ledger service
		// Send RESUME command after invoice settlement to allow device to resume operation
		if err := sc.publishControlCommand(ctx, mqttClient, payload.GetDeviceId(), mqttpb.ControlCommand_CONTROL_COMMAND_RESUME, "INVOICE_SETTLED"); err != nil {
			logger.WithDeviceID(payload.GetDeviceId()).
				Error(ctx, "Error publishing RESUME control command after invoice settlement", err)
			// Don't return error - invoice event was already published successfully
		}
		return nil
	case lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_EXPIRED:
		payload := lightningEvent.GetInvoiceExpired()
		if payload == nil {
			return fmt.Errorf("lightning event missing InvoiceExpired payload")
		}
		logger.WithDeviceID(payload.GetDeviceId()).
			InfoWithFields(ctx, "Processing InvoiceExpired event from lightning stream", map[string]interface{}{
				"invoice_id": payload.GetInvoiceId(),
			})
		return sc.publishInvoiceEvent(ctx, mqttClient, payload.GetDeviceId(), payload.GetInvoiceId(), mqttpb.InvoiceStatus_INVOICE_STATUS_EXPIRED, 0, 0, payload.GetTimestamp())
	default:
		logger.WithStream("event.lightning", "consume").
			DebugWithFields(ctx, "Ignoring lightning event type", map[string]interface{}{
				"type": lightningEvent.GetType().String(),
			})
		return nil
	}
}

func (sc *StreamClient) publishInvoiceEvent(ctx context.Context, mqttClient *MQTTClient, deviceID string, invoiceID string, status mqttpb.InvoiceStatus, amountReceivedMsat int64, balanceMsat int64, timestamp string) error {
	if deviceID == "" {
		return fmt.Errorf("lightning event missing device_id")
	}
	if timestamp == "" {
		timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	payload := &mqttpb.InvoiceEventPayload{
		DeviceId:  deviceID,
		InvoiceId: invoiceID,
		Status:    status,
		Timestamp: timestamp,
	}

	// Only set amount and balance for settled invoices
	if status == mqttpb.InvoiceStatus_INVOICE_STATUS_SETTLED {
		payload.AmountReceivedMsat = amountReceivedMsat
		payload.BalanceMsat = balanceMsat
	}

	marshalOpts := protojson.MarshalOptions{UseProtoNames: true}
	msgBytes, err := marshalOpts.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal invoice event payload: %w", err)
	}

	topic := fmt.Sprintf("/devices/%s/events/invoice", deviceID)
	if err := mqttClient.Publish(ctx, topic, 1, false, msgBytes); err != nil {
		return fmt.Errorf("failed to publish invoice event to MQTT: %w", err)
	}

	logger.WithDeviceID(deviceID).
		InfoWithFields(ctx, "Published invoice event on southbound mqtt", map[string]interface{}{
			"invoice_id": invoiceID,
			"status":     status.String(),
		})
	return nil
}

// publishControlCommand publishes a control command to the device
func (sc *StreamClient) publishControlCommand(ctx context.Context, mqttClient *MQTTClient, deviceID string, command mqttpb.ControlCommand, reason string) error {
	if deviceID == "" {
		return fmt.Errorf("device ID is required")
	}

	payload := &mqttpb.ControlPayload{
		Command: command,
		Reason:  reason,
	}

	marshalOpts := protojson.MarshalOptions{UseProtoNames: true}
	msgBytes, err := marshalOpts.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal control payload: %w", err)
	}

	topic := fmt.Sprintf("/devices/%s/control", deviceID)
	if err := mqttClient.Publish(ctx, topic, 1, false, msgBytes); err != nil {
		return fmt.Errorf("failed to publish control command to MQTT: %w", err)
	}

	logger.WithDeviceID(deviceID).
		InfoWithFields(ctx, "Published control command on southbound mqtt", map[string]interface{}{
			"command": command.String(),
			"reason":  reason,
		})
	return nil
}

// Close closes the Redis client connection (delegates to embedded internal client)
func (sc *StreamClient) Close() error {
	return sc.StreamClient.Close()
}
