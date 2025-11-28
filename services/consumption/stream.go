package main

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/robertodantas/lnpay/internal"
	consumptionpb "github.com/robertodantas/lnpay/proto/gen/model/consumption"
	devicepb "github.com/robertodantas/lnpay/proto/gen/model/device"
)

// StreamHandler handles Redis stream operations for the consumption service
type StreamHandler struct {
	streamClient *internal.StreamClient
	cfg          Config
	repository   *ConsumptionRepository
	consumerName string
	groupName    string
}

// NewStreamHandler creates a new stream handler
func NewStreamHandler(streamClient *internal.StreamClient, cfg Config, repository *ConsumptionRepository) *StreamHandler {
	return &StreamHandler{
		streamClient: streamClient,
		cfg:          cfg,
		repository:   repository,
		consumerName: "consumption-service",
		groupName:    "consumption-consumers",
	}
}

// StartDeviceConsumer starts consuming from the event.device stream
func (sh *StreamHandler) StartDeviceConsumer(ctx context.Context) error {
	streamName := "event.device"
	streamCtx := sh.streamClient.Context()

	// Create consumer group if it doesn't exist
	err := sh.streamClient.XGroupCreateMkStreamWithSpan(streamCtx, streamName, sh.groupName, "0")
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		logger.WithStream(streamName, "consume").
			Warnf(streamCtx, "Failed to create consumer group: %v", err)
		// Continue anyway, group might already exist
	}

	logger.WithStream(streamName, "consume").
		Info(streamCtx, "Starting device event consumer")

	for {
		select {
		case <-ctx.Done():
			logger.WithStream(streamName, "consume").
				Info(streamCtx, "Stopping device event consumer")
			return ctx.Err()
		default:
			// Read from stream with blocking read (wait up to 5 seconds)
			streams, err := sh.streamClient.XReadGroupWithSpan(streamCtx, streamName, sh.groupName, sh.consumerName, &redis.XReadGroupArgs{
				Group:    sh.groupName,
				Consumer: sh.consumerName,
				Streams:  []string{streamName, ">"},
				Count:    10, // Read up to 10 messages at a time
				Block:    5 * time.Second,
			})

			if err != nil {
				if err == redis.Nil {
					// No messages, continue
					continue
				}
				logger.WithStream(streamName, "consume").
					Error(streamCtx, "Error reading from stream", err)
				time.Sleep(1 * time.Second)
				continue
			}

			// Process messages
			for _, stream := range streams {
				for _, msg := range stream.Messages {
					// Wrap event processing with tracing
					if err := internal.TraceEventProcessing(ctx, streamName, msg, sh.handleDeviceEvent); err != nil {
						logger.WithStream(streamName, "consume").
							Errorf(streamCtx, "Error handling device event %s: %v", msg.ID, err)
						// Continue processing other messages
					} else {
						// Acknowledge the message
						sh.streamClient.XAckWithSpan(streamCtx, streamName, sh.groupName, msg.ID, &msg)
					}
				}
			}
		}
	}
}

// handleDeviceEvent processes a DeviceUsageReported event from event.device stream
func (sh *StreamHandler) handleDeviceEvent(ctx context.Context, msg redis.XMessage) error {
	// Extract event JSON from message
	eventJSON, ok := msg.Values["event"].(string)
	if !ok {
		return fmt.Errorf("invalid event format: missing 'event' field")
	}

	// Unmarshal DeviceEvent
	var deviceEvent devicepb.DeviceEvent
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal([]byte(eventJSON), &deviceEvent); err != nil {
		return fmt.Errorf("failed to unmarshal device event: %w", err)
	}

	// Check event type
	if deviceEvent.GetType() != devicepb.DeviceEventType_DEVICE_EVENT_TYPE_USAGE_REPORTED {
		logger.WithStream("event.device", "consume").
			Debugf(ctx, "Skipping event type: %v", deviceEvent.GetType())
		return nil
	}

	usageReported := deviceEvent.GetUsageReported()
	if usageReported == nil || usageReported.GetUsage() == nil {
		return fmt.Errorf("missing usage_reported payload")
	}

	usage := usageReported.GetUsage()
	logger.WithStream("event.device", "consume").
		WithDeviceID(usage.GetDeviceId()).
		InfoWithFields(ctx, "Device event received", map[string]interface{}{
			"report_id":           usage.GetReportId(),
			"measure":             usage.GetMeasure(),
			"unit":                usage.GetUnit(),
			"price_per_unit_msat": usage.GetPricePerUnitMsat(),
		})

	// Process the usage: calculate debit and store in outbox
	return sh.processUsageReport(ctx, usage)
}

// processUsageReport calculates debit amount and stores in database with outbox pattern
func (sh *StreamHandler) processUsageReport(ctx context.Context, usage *devicepb.UsageRecord) error {
	reportID := usage.GetReportId()
	if reportID == "" {
		return fmt.Errorf("missing report_id")
	}

	deviceID := usage.GetDeviceId()
	measure := usage.GetMeasure()
	pricePerUnitMsat := usage.GetPricePerUnitMsat()

	tx, err := sh.repository.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Check idempotency: if report_id already exists, skip
	exists, err := sh.repository.CheckReportExists(ctx, tx, reportID)
	if err != nil {
		return err
	}
	if exists {
		// Report already processed, skip (idempotency)
		logger.WithDeviceID(deviceID).
			DebugWithFields(ctx, "Report already processed, skipping (idempotency)", map[string]interface{}{
				"report_id": reportID,
			})
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit: %w", err)
		}
		return nil
	}

	// Get current accumulated fractional msat for this device from ledger
	accumulated, err := sh.repository.GetDeviceAccumulatedAmount(ctx, tx, deviceID)
	if err != nil {
		return err
	}

	logger.WithDeviceID(deviceID).
		InfoWithFields(ctx, "Processing report", map[string]interface{}{
			"report_id":        reportID,
			"measure":          measure,
			"unit":             usage.GetUnit(),
			"price_msat":       pricePerUnitMsat,
			"accumulated_msat": accumulated,
		})

	// Calculate exact debit amount from this usage report
	usageDebitMsat := float64(pricePerUnitMsat) * measure

	// Calculate total including previously accumulated amount
	totalMsat := accumulated + usageDebitMsat

	// Extract integer part for actual debit (we can only debit whole msats >= 1)
	actualDebitMsat := int64(totalMsat)

	// Remaining fractional part stays in accumulator
	remainingFractionalMsat := totalMsat - float64(actualDebitMsat)

	// Append to accumulation ledger:
	// 1. Add the new usage amount
	balanceAfterAdd := accumulated + usageDebitMsat
	if err := sh.repository.AppendAccumulationLedger(ctx, tx, deviceID, reportID, "add", usageDebitMsat, balanceAfterAdd); err != nil {
		return err
	}
	logger.WithDeviceID(deviceID).
		DebugWithFields(ctx, "Ledger: added amount", map[string]interface{}{
			"amount_msat":  usageDebitMsat,
			"balance_msat": balanceAfterAdd,
		})

	// 2. If we can debit (actualDebitMsat >= 1), record consumption
	if actualDebitMsat >= 1 {
		// We're consuming the integer part, leaving the fractional remainder
		consumedMsat := float64(actualDebitMsat)
		balanceAfterConsume := balanceAfterAdd - consumedMsat
		if err := sh.repository.AppendAccumulationLedger(ctx, tx, deviceID, reportID, "consume", consumedMsat, balanceAfterConsume); err != nil {
			return err
		}
		logger.WithDeviceID(deviceID).
			DebugWithFields(ctx, "Ledger: consumed amount", map[string]interface{}{
				"consumed_msat": consumedMsat,
				"balance_msat":  balanceAfterConsume,
			})
	}

	// Insert into consumption_records and optionally outbox (only if actualDebitMsat >= 1)
	err = sh.repository.CreateConsumptionRecord(ctx, tx, reportID, deviceID, actualDebitMsat, measure, pricePerUnitMsat, usage.GetUnit(), usage.GetTimestamp())
	if err != nil {
		return err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	if actualDebitMsat >= 1 {
		logger.WithDeviceID(deviceID).
			InfoWithFields(ctx, "Consumption recorded", map[string]interface{}{
				"report_id":                 reportID,
				"debit_msat":                actualDebitMsat,
				"remaining_fractional_msat": remainingFractionalMsat,
			})
	} else {
		logger.WithDeviceID(deviceID).
			InfoWithFields(ctx, "Consumption accumulated", map[string]interface{}{
				"report_id":              reportID,
				"usage_msat":             usageDebitMsat,
				"total_accumulated_msat": totalMsat,
			})
	}

	return nil
}

// StartOutboxPublisher starts publishing events from outbox to event.consumption stream
func (sh *StreamHandler) StartOutboxPublisher(ctx context.Context) error {
	logger.WithStream("event.consumption", "produce").
		Info(ctx, "Starting outbox publisher")

	ticker := time.NewTicker(1 * time.Second) // Check every second
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.WithStream("event.consumption", "produce").
				Info(ctx, "Stopping outbox publisher")
			return ctx.Err()
		case <-ticker.C:
			if err := sh.publishOutboxEvents(ctx); err != nil {
				logger.WithStream("event.consumption", "produce").
					Error(ctx, "Error publishing outbox events", err)
			}
		}
	}
}

// publishOutboxEvents publishes unpublished events from outbox to event.consumption stream
func (sh *StreamHandler) publishOutboxEvents(ctx context.Context) error {
	// Get unpublished events by joining outbox with consumption_records
	// This avoids duplication - outbox is minimal, records is the source of truth
	events, err := sh.repository.GetUnpublishedOutboxEvents(ctx, 10)
	if err != nil {
		return err
	}

	if len(events) == 0 {
		return nil // No events to publish
	}

	// Publish each event
	for _, e := range events {
		if err := sh.publishConsumptionEvent(ctx, e.ReportID, e.DeviceID, e.DebitMsat, e.Timestamp); err != nil {
			logger.WithDeviceID(e.DeviceID).
				WithStream("event.consumption", "produce").
				Errorf(ctx, "Failed to publish event for report %s: %v", e.ReportID, err)
			continue
		}

		// Mark as published
		if err := sh.repository.MarkOutboxAsPublished(ctx, e.ReportID); err != nil {
			logger.WithDeviceID(e.DeviceID).
				Errorf(ctx, "Failed to mark report %s as published: %v", e.ReportID, err)
			// Continue anyway, we'll retry on next run
		}
	}

	if len(events) > 0 {
		logger.WithStream("event.consumption", "produce").
			InfoWithFields(ctx, "Published events from outbox", map[string]interface{}{
				"count": len(events),
			})
	}

	return nil
}

// publishConsumptionEvent publishes a DeviceConsumptionRecorded event to event.consumption stream
func (sh *StreamHandler) publishConsumptionEvent(ctx context.Context, reportID, deviceID string, debitMsat int64, timestamp string) error {
	// Create DeviceConsumptionRecordedEvent
	event := &consumptionpb.DeviceConsumptionRecordedEvent{
		DeviceId:  deviceID,
		DebitMsat: debitMsat,
		Timestamp: timestamp,
	}

	// Wrap in ConsumptionEvent envelope
	consumptionEvent := &consumptionpb.ConsumptionEvent{
		Type: consumptionpb.ConsumptionEventType_CONSUMPTION_EVENT_TYPE_DEVICE_CONSUMPTION_RECORDED,
		Payload: &consumptionpb.ConsumptionEvent_DeviceConsumptionRecorded{
			DeviceConsumptionRecorded: event,
		},
	}

	// Serialize to JSON
	opts := protojson.MarshalOptions{UseProtoNames: true}
	jsonBytes, err := opts.Marshal(consumptionEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal consumption event to JSON: %w", err)
	}

	// Publish to Redis stream "event.consumption"
	streamName := "event.consumption"
	values := map[string]interface{}{
		"event":     string(jsonBytes),
		"timestamp": time.Now().UnixMilli(),
	}

	// Use XADD to add entry to stream
	// Clean event type: "CONSUMPTION_EVENT_TYPE_DEVICE_CONSUMPTION_RECORDED" -> "DEVICE_CONSUMPTION_RECORDED"
	eventTypeFull := consumptionEvent.Type.String()
	eventType := eventTypeFull
	if len(eventTypeFull) > len("CONSUMPTION_EVENT_TYPE_") && eventTypeFull[:len("CONSUMPTION_EVENT_TYPE_")] == "CONSUMPTION_EVENT_TYPE_" {
		eventType = eventTypeFull[len("CONSUMPTION_EVENT_TYPE_"):]
	}
	streamID, err := sh.streamClient.XAddWithSpan(ctx, streamName, &redis.XAddArgs{
		Stream: streamName,
		Values: values,
	}, eventType)

	if err != nil {
		return fmt.Errorf("failed to publish to Redis stream %s: %w", streamName, err)
	}

	logger.WithDeviceID(deviceID).
		WithStream(streamName, "produce").
		InfoWithFields(ctx, "Published DeviceConsumptionRecorded event", map[string]interface{}{
			"report_id":  reportID,
			"debit_msat": debitMsat,
			"stream_id":  streamID,
		})
	return nil
}

// StartOutboxCleanup periodically removes old published records from outbox
// This keeps the outbox table small and only contains recent unpublished events
func (sh *StreamHandler) StartOutboxCleanup(ctx context.Context) error {
	logger.Info(ctx, "Starting outbox cleanup")

	ticker := time.NewTicker(1 * time.Hour) // Run cleanup every hour
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info(ctx, "Stopping outbox cleanup")
			return ctx.Err()
		case <-ticker.C:
			if err := sh.cleanupOutbox(ctx); err != nil {
				logger.Error(ctx, "Error cleaning up outbox", err)
			}
		}
	}
}

// cleanupOutbox removes published records older than retention period (default: 7 days)
// This is a common pattern: keep published records for debugging/audit, then clean up
func (sh *StreamHandler) cleanupOutbox(ctx context.Context) error {
	// Retention period: 7 days (configurable)
	retentionDays := 7
	rowsAffected, err := sh.repository.CleanupOutbox(ctx, retentionDays)
	if err != nil {
		return err
	}

	if rowsAffected > 0 {
		logger.InfoWithFields(ctx, "Cleaned up old published records from outbox", map[string]interface{}{
			"rows_affected":  rowsAffected,
			"retention_days": retentionDays,
		})
	}

	return nil
}
