package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/robertodantas/lnpay/library"
	consumptionpb "github.com/robertodantas/lnpay/proto/gen/model/consumption"
	devicepb "github.com/robertodantas/lnpay/proto/gen/model/device"
)

// StreamHandler handles Redis stream operations for the consumption service
type StreamHandler struct {
	streamClient *library.StreamClient
	svc          *Service
	consumerName string
	groupName    string
}

// NewStreamHandler creates a new stream handler
func NewStreamHandler(streamClient *library.StreamClient, svc *Service) *StreamHandler {
	return &StreamHandler{
		streamClient: streamClient,
		svc:          svc,
		consumerName: "consumption-service",
		groupName:    "consumption-consumers",
	}
}

// StartDeviceConsumer starts consuming from the event.device stream
func (sh *StreamHandler) StartDeviceConsumer(ctx context.Context) error {
	streamName := "event.device"
	client := sh.streamClient.Client()
	streamCtx := sh.streamClient.Context()

	// Create consumer group if it doesn't exist
	err := client.XGroupCreateMkStream(streamCtx, streamName, sh.groupName, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		log.Printf("Warning: failed to create consumer group: %v", err)
		// Continue anyway, group might already exist
	}

	log.Printf("Starting device event consumer for stream: %s", streamName)

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping device event consumer...")
			return ctx.Err()
		default:
			// Read from stream with blocking read (wait up to 5 seconds)
			streams, err := client.XReadGroup(streamCtx, &redis.XReadGroupArgs{
				Group:    sh.groupName,
				Consumer: sh.consumerName,
				Streams:  []string{streamName, ">"},
				Count:    10, // Read up to 10 messages at a time
				Block:    5 * time.Second,
			}).Result()

			if err != nil {
				if err == redis.Nil {
					// No messages, continue
					continue
				}
				log.Printf("Error reading from stream %s: %v", streamName, err)
				time.Sleep(1 * time.Second)
				continue
			}

			// Process messages
			for _, stream := range streams {
				for _, msg := range stream.Messages {
					if err := sh.handleDeviceEvent(streamCtx, msg); err != nil {
						log.Printf("Error handling device event %s: %v", msg.ID, err)
						// Continue processing other messages
					} else {
						// Acknowledge the message
						client.XAck(streamCtx, streamName, sh.groupName, msg.ID)
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
		log.Printf("Skipping event type: %v", deviceEvent.GetType())
		return nil
	}

	usageReported := deviceEvent.GetUsageReported()
	if usageReported == nil || usageReported.GetUsage() == nil {
		return fmt.Errorf("missing usage_reported payload")
	}

	usage := usageReported.GetUsage()
	log.Printf("[DEVICE EVENT] Device: %s, ReportID: %s, Measure: %.2f %s, PricePerUnit: %d msat",
		usage.GetDeviceId(), usage.GetReportId(), usage.GetMeasure(), usage.GetUnit(), usage.GetPricePerUnitMsat())

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

	// Calculate debit amount: price_per_unit * measure
	// Convert measure (float64) to int64 msat
	debitMsat := int64(float64(pricePerUnitMsat) * measure)

	if debitMsat <= 0 {
		log.Printf("Warning: calculated debit amount is 0 or negative for report %s, skipping", reportID)
		return nil
	}

	// Get active authorization for device (if any)
	// TODO: This might need to be fetched from ledger service via gRPC
	// For now, we'll leave it empty and let the ledger service handle it
	authorizationID := ""

	tx, err := sh.svc.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Check idempotency: if report_id already exists, skip
	var existingReportID string
	err = tx.QueryRowContext(ctx, `
		SELECT report_id FROM consumption_records WHERE report_id = ?`,
		reportID,
	).Scan(&existingReportID)

	if err == nil {
		// Report already processed, skip (idempotency)
		log.Printf("Report %s already processed, skipping (idempotency)", reportID)
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit: %w", err)
		}
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to check idempotency: %w", err)
	}

	// Insert into consumption_records
	now := time.Now()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO consumption_records (
			report_id, device_id, authorization_id, debit_msat,
			measure, price_per_unit_msat, unit, timestamp, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		reportID, deviceID, authorizationID, debitMsat,
		measure, pricePerUnitMsat, usage.GetUnit(), usage.GetTimestamp(), now.Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to insert consumption record: %w", err)
	}

	// Insert into outbox (for publishing to event.consumption)
	// Minimal entry - we'll join with consumption_records when publishing
	_, err = tx.ExecContext(ctx, `
		INSERT INTO consumption_outbox (
			report_id, published, created_at
		) VALUES (?, 0, ?)`,
		reportID, now.Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to insert into outbox: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("[CONSUMPTION RECORDED] Report: %s, Device: %s, Debit: %d msat",
		reportID, deviceID, debitMsat)

	return nil
}

// StartOutboxPublisher starts publishing events from outbox to event.consumption stream
func (sh *StreamHandler) StartOutboxPublisher(ctx context.Context) error {
	log.Println("Starting outbox publisher...")

	ticker := time.NewTicker(1 * time.Second) // Check every second
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping outbox publisher...")
			return ctx.Err()
		case <-ticker.C:
			if err := sh.publishOutboxEvents(ctx); err != nil {
				log.Printf("Error publishing outbox events: %v", err)
			}
		}
	}
}

// publishOutboxEvents publishes unpublished events from outbox to event.consumption stream
func (sh *StreamHandler) publishOutboxEvents(ctx context.Context) error {
	// Get unpublished events by joining outbox with consumption_records
	// This avoids duplication - outbox is minimal, records is the source of truth
	rows, err := sh.svc.db.QueryContext(ctx, `
		SELECT o.report_id, r.device_id, r.authorization_id, r.debit_msat, r.timestamp
		FROM consumption_outbox o
		INNER JOIN consumption_records r ON o.report_id = r.report_id
		WHERE o.published = 0
		ORDER BY o.created_at ASC
		LIMIT 10`,
	)
	if err != nil {
		return fmt.Errorf("failed to query outbox: %w", err)
	}
	defer rows.Close()

	var events []struct {
		reportID        string
		deviceID        string
		authorizationID string
		debitMsat       int64
		timestamp       string
	}

	for rows.Next() {
		var e struct {
			reportID        string
			deviceID        string
			authorizationID string
			debitMsat       int64
			timestamp       string
		}
		if err := rows.Scan(&e.reportID, &e.deviceID, &e.authorizationID, &e.debitMsat, &e.timestamp); err != nil {
			log.Printf("Error scanning outbox row: %v", err)
			continue
		}
		events = append(events, e)
	}

	if len(events) == 0 {
		return nil // No events to publish
	}

	// Publish each event
	for _, e := range events {
		if err := sh.publishConsumptionEvent(ctx, e.reportID, e.deviceID, e.authorizationID, e.debitMsat, e.timestamp); err != nil {
			log.Printf("Failed to publish event for report %s: %v", e.reportID, err)
			continue
		}

		// Mark as published
		_, err := sh.svc.db.ExecContext(ctx, `
			UPDATE consumption_outbox
			SET published = 1, published_at = ?
			WHERE report_id = ?`,
			time.Now().Unix(), e.reportID,
		)
		if err != nil {
			log.Printf("Failed to mark report %s as published: %v", e.reportID, err)
			// Continue anyway, we'll retry on next run
		}
	}

	if len(events) > 0 {
		log.Printf("Published %d events from outbox", len(events))
	}

	return nil
}

// publishConsumptionEvent publishes a DeviceConsumptionRecorded event to event.consumption stream
func (sh *StreamHandler) publishConsumptionEvent(ctx context.Context, reportID, deviceID, authorizationID string, debitMsat int64, timestamp string) error {
	// Create DeviceConsumptionRecordedEvent
	event := &consumptionpb.DeviceConsumptionRecordedEvent{
		DeviceId:        deviceID,
		AuthorizationId: authorizationID,
		DebitMsat:       debitMsat,
		Timestamp:       timestamp,
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
	result := sh.streamClient.Client().XAdd(ctx, &redis.XAddArgs{
		Stream: streamName,
		Values: values,
	})

	if result.Err() != nil {
		return fmt.Errorf("failed to publish to Redis stream %s: %w", streamName, result.Err())
	}

	log.Printf("Published DeviceConsumptionRecorded event (report: %s, device: %s, debit: %d msat) to stream %s (ID: %s)",
		reportID, deviceID, debitMsat, streamName, result.Val())
	return nil
}

// StartOutboxCleanup periodically removes old published records from outbox
// This keeps the outbox table small and only contains recent unpublished events
func (sh *StreamHandler) StartOutboxCleanup(ctx context.Context) error {
	log.Println("Starting outbox cleanup...")

	ticker := time.NewTicker(1 * time.Hour) // Run cleanup every hour
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping outbox cleanup...")
			return ctx.Err()
		case <-ticker.C:
			if err := sh.cleanupOutbox(ctx); err != nil {
				log.Printf("Error cleaning up outbox: %v", err)
			}
		}
	}
}

// cleanupOutbox removes published records older than retention period (default: 7 days)
// This is a common pattern: keep published records for debugging/audit, then clean up
func (sh *StreamHandler) cleanupOutbox(ctx context.Context) error {
	// Retention period: 7 days (configurable)
	retentionDays := 7
	retentionSeconds := int64(retentionDays * 24 * 60 * 60)
	cutoffTime := time.Now().Unix() - retentionSeconds

	// Delete published records older than retention period
	result, err := sh.svc.db.ExecContext(ctx, `
		DELETE FROM consumption_outbox
		WHERE published = 1 AND published_at < ?`,
		cutoffTime,
	)
	if err != nil {
		return fmt.Errorf("failed to cleanup outbox: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected > 0 {
		log.Printf("Cleaned up %d old published records from outbox (older than %d days)", rowsAffected, retentionDays)
	}

	return nil
}
