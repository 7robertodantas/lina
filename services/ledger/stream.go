package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/robertodantas/lnpay/internal"
	consumptionpb "github.com/robertodantas/lnpay/proto/gen/model/consumption"
	ledgermodel "github.com/robertodantas/lnpay/proto/gen/model/ledger"
	lightningmodel "github.com/robertodantas/lnpay/proto/gen/model/lightning"
)

const (
	authorizationExpiredReason    = "AUTHORIZATION_EXPIRED"
	lightningInvoiceSettledReason = "LIGHTNING_INVOICE_SETTLED"
)

// messageRetryInfo tracks retry information for a message
type messageRetryInfo struct {
	retryCount  int
	lastRetryAt time.Time
	firstSeenAt time.Time
}

// StreamHandler handles Redis stream operations for the ledger service
type StreamHandler struct {
	streamClient *internal.StreamClient
	repo         *LedgerRepository
	consumerName string
	groupName    string
	// retryTracker tracks retry counts and timestamps for messages
	retryTracker sync.Map // map[string]*messageRetryInfo
}

// NewStreamHandler creates a new stream handler
func NewStreamHandler(streamClient *internal.StreamClient, repo *LedgerRepository) *StreamHandler {
	return &StreamHandler{
		streamClient: streamClient,
		repo:         repo,
		consumerName: "ledger-service",
		groupName:    "ledger-consumers",
	}
}

// StartLightningConsumer starts consuming from the event.lightning stream
func (sh *StreamHandler) StartLightningConsumer(ctx context.Context) error {
	streamName := "event.lightning"

	// Create consumer group if it doesn't exist
	err := sh.streamClient.XGroupCreateMkStreamWithSpan(ctx, streamName, sh.groupName, "0")
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		logger.WithStream(streamName, "consume").
			Warnf(ctx, "Failed to create consumer group: %v", err)
	}

	logger.WithStream(streamName, "consume").
		Info(ctx, "Starting lightning consumer")

	// Start pending message retry mechanism in a separate goroutine
	go sh.startPendingMessageRetry(ctx, streamName, sh.handleLightningEvent)

	for {
		select {
		case <-ctx.Done():
			logger.WithStream(streamName, "consume").
				Info(ctx, "Stopping lightning consumer")
			return ctx.Err()
		default:
			streams, err := sh.streamClient.XReadGroupWithSpan(ctx, streamName, sh.groupName, sh.consumerName, &redis.XReadGroupArgs{
				Group:    sh.groupName,
				Consumer: sh.consumerName,
				Streams:  []string{streamName, ">"},
				Count:    10,
				Block:    5 * time.Second,
			})

			if err != nil {
				if err == redis.Nil {
					continue
				}
				logger.WithStream(streamName, "consume").
					Error(ctx, "Error reading from stream", err)
				time.Sleep(1 * time.Second)
				continue
			}

			for _, stream := range streams {
				for _, msg := range stream.Messages {
					// Create ack function
					ackFn := func(ctx context.Context, msg redis.XMessage) error {
						return sh.streamClient.XAckWithSpan(ctx, streamName, sh.groupName, msg.ID, &msg)
					}

					if err := internal.TraceEventProcessing(ctx, streamName, msg, sh.handleLightningEvent, ackFn); err != nil {
						logger.WithStream(streamName, "consume").
							Errorf(ctx, "Error handling lightning event %s: %v", msg.ID, err)
					}
				}
			}
		}
	}
}

func (sh *StreamHandler) handleLightningEvent(ctx context.Context, msg redis.XMessage) error {
	eventJSON, ok := msg.Values["event"].(string)
	if !ok {
		return fmt.Errorf("invalid lightning event format: missing 'event' field")
	}

	var lightningEvent lightningmodel.LightningEvent
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal([]byte(eventJSON), &lightningEvent); err != nil {
		return fmt.Errorf("failed to unmarshal lightning event: %w", err)
	}

	if lightningEvent.GetType() != lightningmodel.LightningEventType_LIGHTNING_EVENT_TYPE_INVOICE_SETTLED {
		logger.WithStream("event.lightning", "consume").
			Debugf(ctx, "Skipping lightning event type: %v", lightningEvent.GetType())
		return nil
	}

	settled := lightningEvent.GetInvoiceSettled()
	if settled == nil {
		return fmt.Errorf("missing invoice_settled payload")
	}

	return sh.processInvoiceSettled(ctx, settled)
}

// StartConsumptionConsumer starts consuming from the event.consumption stream
func (sh *StreamHandler) StartConsumptionConsumer(ctx context.Context) error {
	streamName := "event.consumption"

	// Create consumer group if it doesn't exist
	err := sh.streamClient.XGroupCreateMkStreamWithSpan(ctx, streamName, sh.groupName, "0")
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		logger.WithStream(streamName, "consume").
			Warnf(ctx, "Failed to create consumer group: %v", err)
	}

	logger.WithStream(streamName, "consume").
		Info(ctx, "Starting consumption consumer")

	// Start pending message retry mechanism in a separate goroutine
	go sh.startPendingMessageRetry(ctx, streamName, sh.handleConsumptionEvent)

	for {
		select {
		case <-ctx.Done():
			logger.WithStream(streamName, "consume").
				Info(ctx, "Stopping consumption consumer")
			return ctx.Err()
		default:
			// Read from stream - this creates a span and returns a context with that span
			streams, err := sh.streamClient.XReadGroupWithSpan(ctx, streamName, sh.groupName, sh.consumerName, &redis.XReadGroupArgs{
				Group:    sh.groupName,
				Consumer: sh.consumerName,
				Streams:  []string{streamName, ">"},
				Count:    10,
				Block:    5 * time.Second,
			})

			if err != nil {
				if err == redis.Nil {
					continue
				}
				logger.WithStream(streamName, "consume").
					Error(ctx, "Error reading from stream", err)
				time.Sleep(1 * time.Second)
				continue
			}

			// Process messages with the context that has the read span
			for _, stream := range streams {
				for _, msg := range stream.Messages {
					// Create ack function that will be called within the processing span
					ackFn := func(ctx context.Context, msg redis.XMessage) error {
						return sh.streamClient.XAckWithSpan(ctx, streamName, sh.groupName, msg.ID, &msg)
					}

					// TraceEventProcessing now handles both processing and ack within same span
					err := internal.TraceEventProcessing(ctx, streamName, msg, sh.handleConsumptionEvent, ackFn)
					if err != nil {
						// Check if this is an expected failure
						var expectedErr *ExpectedFailureError
						if errors.As(err, &expectedErr) {
							// Expected failure - don't ACK, let it go to pending for retry with backoff
							// The pending retry mechanism will handle backoff and max retries
							logger.WithStream(streamName, "consume").
								Debugf(ctx, "Expected failure, message will go to pending for retry: %v", expectedErr.Err)
						} else {
							// Unexpected failure - don't ACK, let it go to pending for retry
							logger.WithStream(streamName, "consume").
								Errorf(ctx, "Error handling consumption event %s: %v", msg.ID, err)
						}
					}
				}
			}
		}
	}
}

// handleConsumptionEvent processes a DeviceConsumptionRecorded event
func (sh *StreamHandler) handleConsumptionEvent(ctx context.Context, msg redis.XMessage) error {
	// Extract event JSON from message
	eventJSON, ok := msg.Values["event"].(string)
	if !ok {
		return fmt.Errorf("invalid event format: missing 'event' field")
	}

	// Unmarshal ConsumptionEvent
	var consumptionEvent consumptionpb.ConsumptionEvent
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal([]byte(eventJSON), &consumptionEvent); err != nil {
		return fmt.Errorf("failed to unmarshal consumption event: %w", err)
	}

	// Check event type
	if consumptionEvent.GetType() != consumptionpb.ConsumptionEventType_CONSUMPTION_EVENT_TYPE_DEVICE_CONSUMPTION_RECORDED {
		logger.WithStream("event.consumption", "consume").
			Debugf(ctx, "Skipping event type: %v", consumptionEvent.GetType())
		return nil
	}

	recorded := consumptionEvent.GetDeviceConsumptionRecorded()
	if recorded == nil {
		return fmt.Errorf("missing device_consumption_recorded payload")
	}

	logger.WithStream("event.consumption", "consume").
		WithDeviceID(recorded.GetDeviceId()).
		InfoWithFields(ctx, "Consumption received", map[string]interface{}{
			"debit_msat": recorded.GetDebitMsat(),
		})

	// Process the consumption: debit from authorization
	return sh.processConsumption(ctx, recorded)
}

// ExpectedFailureError indicates an expected failure that should be ACKed
// (e.g., no active authorization - we've already published a failed event)
type ExpectedFailureError struct {
	Err error
}

func (e *ExpectedFailureError) Error() string {
	return e.Err.Error()
}

func (e *ExpectedFailureError) Unwrap() error {
	return e.Err
}

// processConsumption debits from an authorization and updates its status
func (sh *StreamHandler) processConsumption(ctx context.Context, recorded *consumptionpb.DeviceConsumptionRecordedEvent) error {
	deviceID := recorded.GetDeviceId()
	if deviceID == "" {
		return fmt.Errorf("missing device_id in consumption event")
	}

	tx, err := sh.repo.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Find active authorization for the device
	// Order by created_at DESC to get the most recent active authorization
	now := time.Now().Format(time.RFC3339)
	authorizationID, remainingMsat, grantedMsat, overflowMsat, _, _, err := sh.repo.GetActiveAuthorization(ctx, tx, deviceID, now)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No active authorization found - publish failed event
			// This is an expected failure scenario (device may not have authorization yet)
			// We've handled it appropriately by publishing the failed event, so we should ACK the message
			logger.WithDeviceID(deviceID).
				Warn(ctx, "No active authorization found")
			timestamp := time.Now().Format(time.RFC3339)
			if err := sh.PublishAuthorizationDebitFailed(ctx, "", deviceID, recorded.GetDebitMsat(), 0, "NO_ACTIVE_AUTHORIZATION", timestamp); err != nil {
				logger.WithDeviceID(deviceID).
					WithStream("event.ledger", "produce").
					Error(ctx, "Failed to publish AuthorizationDebitFailed event", err)
			}
			// Return ExpectedFailureError so the consumer knows to ACK this message
			return &ExpectedFailureError{Err: fmt.Errorf("no active authorization found for device %s", deviceID)}
		}
		return fmt.Errorf("failed to get authorization: %w", err)
	}

	debitAmount := recorded.GetDebitMsat()
	if debitAmount <= 0 {
		return fmt.Errorf("invalid debit amount: %d", debitAmount)
	}

	// Check if we have enough remaining
	if remainingMsat < debitAmount {
		logger.WithDeviceID(deviceID).
			WarnWithFields(ctx, "Insufficient remaining in authorization", map[string]interface{}{
				"authorization_id": authorizationID,
				"remaining_msat":   remainingMsat,
				"requested_msat":   debitAmount,
			})
		// Still debit what we can, but mark as completed
		debitAmount = remainingMsat
	}

	// Update authorization: subtract debit amount
	newRemaining := remainingMsat - debitAmount
	newStatus := "active"
	if newRemaining <= 0 {
		newStatus = "completed"
	}

	currentConsumed := grantedMsat - remainingMsat
	if currentConsumed < 0 {
		currentConsumed = 0
	}
	newConsumed := currentConsumed + debitAmount
	if newConsumed > grantedMsat {
		newConsumed = grantedMsat
	}

	overflowDelta := recorded.GetDebitMsat() - debitAmount
	if overflowDelta < 0 {
		overflowDelta = 0
	}
	newOverflow := overflowMsat + overflowDelta

	if err := sh.repo.UpdateAuthorization(ctx, tx, authorizationID, newRemaining, newConsumed, newOverflow, newStatus); err != nil {
		return fmt.Errorf("failed to update authorization: %w", err)
	}

	// Create debit entry for overflow if any
	var overflowEntry *EntryResponse
	if newOverflow > 0 {
		entry, err := sh.repo.ApplyDebit(ctx, tx, DebitRequest{
			DeviceID:      deviceID,
			AmountMsat:    newOverflow,
			Reason:        "AUTHORIZATION_OVERFLOW",
			CorrelationID: authorizationID,
			AllowNegative: true,
		})
		if err != nil {
			return fmt.Errorf("failed to apply overflow debit: %w", err)
		}
		overflowEntry = &entry
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Publish events based on new status
	timestamp := time.Now().Format(time.RFC3339)

	// Publish AuthorizationDebited event
	if err := sh.PublishAuthorizationDebited(ctx, authorizationID, deviceID, debitAmount, newRemaining, timestamp); err != nil {
		logger.WithDeviceID(deviceID).
			WithStream("event.ledger", "produce").
			Error(ctx, "Failed to publish AuthorizationDebited event", err)
	}

	if newStatus == "completed" {
		// Publish AuthorizationCompleted event
		if err := sh.PublishAuthorizationCompleted(ctx, authorizationID, deviceID, timestamp); err != nil {
			logger.WithDeviceID(deviceID).
				WithStream("event.ledger", "produce").
				Error(ctx, "Failed to publish AuthorizationCompleted event", err)
		}
	}

	// Publish DeviceDebited event for overflow if any
	if overflowEntry != nil {
		overflowTimestamp := time.Unix(overflowEntry.CreatedAt, 0).UTC().Format(time.RFC3339)
		if err := sh.PublishDeviceDebited(ctx, deviceID, authorizationID, overflowEntry.AmountMsat, overflowEntry.BalanceAfter, overflowTimestamp); err != nil {
			logger.WithDeviceID(deviceID).
				WithStream("event.ledger", "produce").
				Error(ctx, "Failed to publish DeviceDebited event for overflow", err)
		}
	}

	return nil
}

// startPendingMessageRetry continuously retries pending messages that failed to process
// This handles transient failures (e.g., temporary DB issues) that might resolve later
// Uses blocking reads to process pending messages immediately when they become available
// handlerFn is the function to call for processing each message
func (sh *StreamHandler) startPendingMessageRetry(ctx context.Context, streamName string, handlerFn func(context.Context, redis.XMessage) error) {
	logger.WithStream(streamName, "consume").
		Info(ctx, "Starting pending message retry mechanism (continuous)")

	// Cleanup old retry tracking entries periodically
	go sh.cleanupRetryTracker(ctx)

	for {
		select {
		case <-ctx.Done():
			logger.WithStream(streamName, "consume").
				Info(ctx, "Stopping pending message retry")
			return
		default:
			// Read pending messages with blocking read
			// Using "0" instead of ">" reads from pending entries
			// Block: 5 seconds - will wait for pending messages or timeout
			streams, err := sh.streamClient.XReadGroupWithSpan(ctx, streamName, sh.groupName, sh.consumerName, &redis.XReadGroupArgs{
				Group:    sh.groupName,
				Consumer: sh.consumerName,
				Streams:  []string{streamName, "0"}, // "0" reads pending messages
				Count:    10,                        // Process up to 10 pending messages at a time
				Block:    5 * time.Second,           // Blocking - waits for pending messages or times out
			})

			if err != nil {
				if err == redis.Nil {
					// No pending messages, continue loop to check again
					continue
				}
				logger.WithStream(streamName, "consume").
					Errorf(ctx, "Error reading pending messages: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}

			// Process pending messages
			retried := 0
			acked := 0
			skipped := 0
			now := time.Now()
			var earliestRetryTime *time.Time // Track earliest retry time among skipped messages

			for _, stream := range streams {
				for _, msg := range stream.Messages {
					ackFn := func(ctx context.Context, msg redis.XMessage) error {
						return sh.streamClient.XAckWithSpan(ctx, streamName, sh.groupName, msg.ID, &msg)
					}

					// Check if we should retry this message based on backoff
					// This checks if enough time has passed since the last retry attempt
					retryInfo, shouldRetry := sh.shouldRetryMessageWithInfo(ctx, msg.ID, now)
					if !shouldRetry {
						skipped++
						// Track earliest retry time for optimization
						if retryInfo != nil && retryInfo.retryCount > 0 {
							backoffDuration := sh.calculateBackoffDuration(retryInfo.retryCount)
							nextRetryTime := retryInfo.lastRetryAt.Add(backoffDuration)
							if earliestRetryTime == nil || nextRetryTime.Before(*earliestRetryTime) {
								earliestRetryTime = &nextRetryTime
							}
						}
						continue
					}

					// Retry info already obtained from shouldRetryMessageWithInfo above
					attemptTime := time.Now()

					// Retry processing the message
					err := internal.TraceEventProcessing(ctx, streamName, msg, handlerFn, ackFn)
					if err != nil {
						// Check if this is an expected failure
						var expectedErr *ExpectedFailureError
						if errors.As(err, &expectedErr) {
							// Expected failure - apply backoff and retry limit
							retryInfo.retryCount++
							retryInfo.lastRetryAt = attemptTime // Set when failure occurred

							const maxRetries = 10 // Maximum retries before giving up
							if retryInfo.retryCount >= maxRetries {
								// Max retries reached - ACK the message to stop retrying
								if ackErr := ackFn(ctx, msg); ackErr != nil {
									logger.WithStream(streamName, "consume").
										Errorf(ctx, "Failed to ACK max-retry message %s: %v", msg.ID, ackErr)
								} else {
									acked++
									sh.retryTracker.Delete(msg.ID) // Clean up tracking
									logger.WithStream(streamName, "consume").
										WarnWithFields(ctx, "Max retries reached for expected failure, ACKed message", map[string]interface{}{
											"message_id":  msg.ID,
											"retry_count": retryInfo.retryCount,
											"error":       expectedErr.Error(),
										})
								}
							} else {
								// Will retry later with backoff - message stays in pending
								// Log the backoff for the NEXT retry (retryCount + 1)
								nextRetryBackoff := sh.calculateBackoffDuration(retryInfo.retryCount + 1)
								logger.WithStream(streamName, "consume").
									DebugWithFields(ctx, "Expected failure, will retry with backoff", map[string]interface{}{
										"message_id":    msg.ID,
										"retry_count":   retryInfo.retryCount,
										"next_retry_in": nextRetryBackoff,
										"error":         expectedErr.Error(),
									})
							}
						} else {
							// Unexpected failure - check if it's a database lock error
							if isDatabaseLockError(err) {
								// Database lock error - apply backoff to avoid contention
								retryInfo.retryCount++
								retryInfo.lastRetryAt = attemptTime // Set when failure occurred

								// Will retry later with backoff - message stays in pending
								// Log the backoff for the NEXT retry (retryCount + 1)
								nextRetryBackoff := sh.calculateBackoffDuration(retryInfo.retryCount + 1)
								logger.WithStream(streamName, "consume").
									WarnWithFields(ctx, "Database lock error, will retry with backoff", map[string]interface{}{
										"message_id":    msg.ID,
										"retry_count":   retryInfo.retryCount,
										"next_retry_in": nextRetryBackoff,
										"error":         err.Error(),
									})
							} else {
								// Other unexpected failure - reset retry count (might be transient)
								sh.retryTracker.Delete(msg.ID)
								// Still failing - log but don't ACK, will retry again later
								logger.WithStream(streamName, "consume").
									Warnf(ctx, "Pending message %s still failing: %v (will retry later)", msg.ID, err)
							}
						}
					} else {
						// Successfully processed - clear retry tracking
						sh.retryTracker.Delete(msg.ID)
						retried++
						logger.WithStream(streamName, "consume").
							Infof(ctx, "Successfully retried pending message %s", msg.ID)
					}
				}
			}

			if retried > 0 || acked > 0 || skipped > 0 {
				logger.WithStream(streamName, "consume").
					InfoWithFields(ctx, "Processed pending messages", map[string]interface{}{
						"retried_count": retried,
						"acked_count":   acked,
						"skipped_count": skipped,
					})
			}

			// If all messages were skipped and we have an earliest retry time, sleep until then
			// This avoids constantly checking messages that are all in backoff
			if skipped > 0 && retried == 0 && acked == 0 && earliestRetryTime != nil {
				sleepDuration := time.Until(*earliestRetryTime)
				// Cap sleep at 30 seconds to ensure we check for new messages periodically
				maxSleep := 30 * time.Second
				if sleepDuration > 0 && sleepDuration < maxSleep {
					logger.WithStream(streamName, "consume").
						Debugf(ctx, "All messages in backoff, sleeping for %v until earliest retry", sleepDuration)
					time.Sleep(sleepDuration)
				} else if sleepDuration > maxSleep {
					// If earliest retry is more than 30s away, sleep for maxSleep
					logger.WithStream(streamName, "consume").
						Debugf(ctx, "Earliest retry is %v away, sleeping for %v", sleepDuration, maxSleep)
					time.Sleep(maxSleep)
				}
			}
		}
	}
}

// processInvoiceSettled credits the device balance when an invoice settles
func (sh *StreamHandler) processInvoiceSettled(ctx context.Context, settled *lightningmodel.InvoiceSettledEvent) error {
	if settled == nil {
		return errors.New("invoice settled payload is nil")
	}

	invoiceID := settled.GetInvoiceId()
	deviceID := settled.GetDeviceId()
	amountMsat := settled.GetAmountReceivedMsat()

	if invoiceID == "" {
		return errors.New("missing invoice_id in lightning event")
	}
	if deviceID == "" {
		return errors.New("missing device_id in lightning event")
	}
	if amountMsat <= 0 {
		return fmt.Errorf("invalid amount for invoice %s: %d", invoiceID, amountMsat)
	}

	creditReq := CreditRequest{
		DeviceID:       deviceID,
		AmountMsat:     amountMsat,
		Reason:         lightningInvoiceSettledReason,
		CorrelationID:  invoiceID,
		IdempotencyKey: invoiceID,
	}

	// Fast path for duplicate events
	if kind, _, ok, err := sh.repo.GetCachedIdem(ctx, creditReq.IdempotencyKey); err != nil {
		return fmt.Errorf("failed to check idempotency for invoice %s: %w", invoiceID, err)
	} else if ok {
		if kind == "credit" {
			logger.WithDeviceID(deviceID).
				WithStream("event.lightning", "consume").
				InfoWithFields(ctx, "Invoice already credited, skipping", map[string]interface{}{
					"invoice_id": invoiceID,
				})
			return nil
		}
		return fmt.Errorf("idempotency key %s already used for kind %s", invoiceID, kind)
	}

	tx, err := sh.repo.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin tx for invoice %s: %w", invoiceID, err)
	}
	defer func() { _ = tx.Rollback() }()

	entry, err := sh.repo.ApplyCredit(ctx, tx, creditReq)
	if err != nil {
		return fmt.Errorf("failed to apply credit for invoice %s: %w", invoiceID, err)
	}

	if err := sh.repo.SaveIdem(ctx, tx, creditReq.IdempotencyKey, "credit", hashReq("credit", creditReq), entry); err != nil {
		return fmt.Errorf("failed to store idempotency for invoice %s: %w", invoiceID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit credit for invoice %s: %w", invoiceID, err)
	}

	logger.WithDeviceID(deviceID).
		WithStream("event.lightning", "consume").
		InfoWithFields(ctx, "Credited device from invoice", map[string]interface{}{
			"invoice_id":    invoiceID,
			"amount_msat":   entry.AmountMsat,
			"balance_after": entry.BalanceAfter,
		})

	timestamp := time.Unix(entry.CreatedAt, 0).UTC().Format(time.RFC3339)
	if err := sh.PublishDeviceCredited(ctx, entry.DeviceID, entry.AmountMsat, entry.BalanceAfter, timestamp); err != nil {
		logger.WithDeviceID(deviceID).
			WithStream("event.ledger", "produce").
			Errorf(ctx, "Failed to publish DeviceCreditedEvent for invoice %s: %v", invoiceID, err)
	}

	return nil
}

// PublishAuthorizationCreated publishes an AuthorizationCreated event to event.ledger
func (sh *StreamHandler) PublishAuthorizationCreated(ctx context.Context, auth *ledgermodel.Authorization) error {
	event := &ledgermodel.AuthorizationCreatedEvent{
		Authorization: auth,
	}

	ledgerEvent := &ledgermodel.LedgerEvent{
		Type: ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_CREATED,
		Payload: &ledgermodel.LedgerEvent_AuthorizationCreated{
			AuthorizationCreated: event,
		},
	}

	return sh.publishLedgerEvent(ctx, ledgerEvent)
}

// PublishAuthorizationCompleted publishes an AuthorizationCompleted event to event.ledger
func (sh *StreamHandler) PublishAuthorizationCompleted(ctx context.Context, authorizationID, deviceID, timestamp string) error {
	event := &ledgermodel.AuthorizationCompletedEvent{
		AuthorizationId: authorizationID,
		DeviceId:        deviceID,
		Timestamp:       timestamp,
	}

	ledgerEvent := &ledgermodel.LedgerEvent{
		Type: ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_COMPLETED,
		Payload: &ledgermodel.LedgerEvent_AuthorizationCompleted{
			AuthorizationCompleted: event,
		},
	}

	return sh.publishLedgerEvent(ctx, ledgerEvent)
}

// PublishAuthorizationExpired publishes an AuthorizationExpired event to event.ledger
func (sh *StreamHandler) PublishAuthorizationExpired(ctx context.Context, authorizationID, deviceID, timestamp string) error {
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

	return sh.publishLedgerEvent(ctx, ledgerEvent)
}

// PublishDeviceCredited publishes a DeviceCreditedEvent to event.ledger
func (sh *StreamHandler) PublishDeviceCredited(ctx context.Context, deviceID string, amountMsat, newBalanceMsat int64, timestamp string) error {
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

	return sh.publishLedgerEvent(ctx, ledgerEvent)
}

// PublishDeviceDebited publishes a DeviceDebitedEvent to event.ledger
func (sh *StreamHandler) PublishDeviceDebited(ctx context.Context, deviceID, authorizationID string, amountMsat, newBalanceMsat int64, timestamp string) error {
	event := &ledgermodel.DeviceDebitedEvent{
		DeviceId:        deviceID,
		AuthorizationId: authorizationID,
		AmountMsat:      amountMsat,
		NewBalanceMsat:  newBalanceMsat,
		Timestamp:       timestamp,
	}

	ledgerEvent := &ledgermodel.LedgerEvent{
		Type: ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_DEVICE_DEBITED,
		Payload: &ledgermodel.LedgerEvent_DeviceDebited{
			DeviceDebited: event,
		},
	}

	return sh.publishLedgerEvent(ctx, ledgerEvent)
}

// PublishAuthorizationDebited publishes an AuthorizationDebitedEvent to event.ledger
func (sh *StreamHandler) PublishAuthorizationDebited(ctx context.Context, authorizationID, deviceID string, amountMsat, remainingMsat int64, timestamp string) error {
	event := &ledgermodel.AuthorizationDebitedEvent{
		AuthorizationId: authorizationID,
		DeviceId:        deviceID,
		AmountMsat:      amountMsat,
		RemainingMsat:   remainingMsat,
		Timestamp:       timestamp,
	}

	ledgerEvent := &ledgermodel.LedgerEvent{
		Type: ledgermodel.LedgerEventType_LEDGER_EVENT_TYPE_AUTHORIZATION_DEBITED,
		Payload: &ledgermodel.LedgerEvent_AuthorizationDebited{
			AuthorizationDebited: event,
		},
	}

	return sh.publishLedgerEvent(ctx, ledgerEvent)
}

// PublishAuthorizationDebitFailed publishes an AuthorizationDebitFailedEvent to event.ledger
func (sh *StreamHandler) PublishAuthorizationDebitFailed(ctx context.Context, authorizationID, deviceID string, requestedMsat, remainingMsat int64, reason, timestamp string) error {
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

	return sh.publishLedgerEvent(ctx, ledgerEvent)
}

// publishLedgerEvent publishes a LedgerEvent to the event.ledger stream
func (sh *StreamHandler) publishLedgerEvent(ctx context.Context, ledgerEvent *ledgermodel.LedgerEvent) error {
	// Serialize to JSON
	opts := protojson.MarshalOptions{UseProtoNames: true}
	jsonBytes, err := opts.Marshal(ledgerEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal ledger event to JSON: %w", err)
	}

	// Publish to Redis stream "event.ledger"
	streamName := "event.ledger"
	values := map[string]interface{}{
		"event":     string(jsonBytes),
		"timestamp": time.Now().UnixMilli(),
	}

	// Use XADD to add entry to stream
	// Clean event type: "LEDGER_EVENT_TYPE_AUTHORIZATION_DEBITED" -> "AUTHORIZATION_DEBITED"
	eventTypeFull := ledgerEvent.GetType().String()
	eventType := eventTypeFull
	if len(eventTypeFull) > len("LEDGER_EVENT_TYPE_") && eventTypeFull[:len("LEDGER_EVENT_TYPE_")] == "LEDGER_EVENT_TYPE_" {
		eventType = eventTypeFull[len("LEDGER_EVENT_TYPE_"):]
	}
	streamID, err := sh.streamClient.XAddWithSpan(ctx, streamName, &redis.XAddArgs{
		Stream: streamName,
		Values: values,
	}, eventType)

	if err != nil {
		return fmt.Errorf("failed to publish to Redis stream %s: %w", streamName, err)
	}

	// Extract device_id from event if available
	deviceID := extractDeviceIDFromLedgerEvent(ledgerEvent)
	logEntry := logger.WithStream(streamName, "produce")
	if deviceID != "" {
		logEntry = logEntry.WithDeviceID(deviceID)
	}
	logEntry.InfoWithFields(ctx, "Published LedgerEvent", map[string]interface{}{
		"event_type": ledgerEvent.GetType().String(),
		"stream_id":  streamID,
	})
	return nil
}

// extractDeviceIDFromLedgerEvent extracts device_id from various ledger event types
func extractDeviceIDFromLedgerEvent(event *ledgermodel.LedgerEvent) string {
	switch payload := event.GetPayload().(type) {
	case *ledgermodel.LedgerEvent_AuthorizationCreated:
		if payload.AuthorizationCreated != nil && payload.AuthorizationCreated.Authorization != nil {
			return payload.AuthorizationCreated.Authorization.DeviceId
		}
	case *ledgermodel.LedgerEvent_AuthorizationDebited:
		if payload.AuthorizationDebited != nil {
			return payload.AuthorizationDebited.DeviceId
		}
	case *ledgermodel.LedgerEvent_AuthorizationCompleted:
		if payload.AuthorizationCompleted != nil {
			return payload.AuthorizationCompleted.DeviceId
		}
	case *ledgermodel.LedgerEvent_AuthorizationExpired:
		if payload.AuthorizationExpired != nil {
			return payload.AuthorizationExpired.DeviceId
		}
	case *ledgermodel.LedgerEvent_AuthorizationDebitFailed:
		if payload.AuthorizationDebitFailed != nil {
			return payload.AuthorizationDebitFailed.DeviceId
		}
	case *ledgermodel.LedgerEvent_DeviceCredited:
		if payload.DeviceCredited != nil {
			return payload.DeviceCredited.DeviceId
		}
	case *ledgermodel.LedgerEvent_DeviceDebited:
		if payload.DeviceDebited != nil {
			return payload.DeviceDebited.DeviceId
		}
	}
	return ""
}

// StartExpirationChecker periodically checks for expired authorizations
func (sh *StreamHandler) StartExpirationChecker(ctx context.Context) error {
	logger.Info(ctx, "Starting authorization expiration checker")

	ticker := time.NewTicker(1 * time.Minute) // Check every minute
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info(ctx, "Stopping expiration checker")
			return ctx.Err()
		case <-ticker.C:
			if err := sh.checkExpiredAuthorizations(ctx); err != nil {
				logger.Error(ctx, "Error checking expired authorizations", err)
			}
		}
	}
}

// checkExpiredAuthorizations finds and marks expired authorizations
func (sh *StreamHandler) checkExpiredAuthorizations(ctx context.Context) error {
	now := time.Now().Format(time.RFC3339)

	// Find expired active authorizations
	expired, err := sh.repo.GetExpiredAuthorizations(ctx, now)
	if err != nil {
		return fmt.Errorf("failed to query expired authorizations: %w", err)
	}

	processed := 0

	// Update expired authorizations and publish events
	for _, auth := range expired {
		tx, err := sh.repo.BeginTx(ctx, &sql.TxOptions{})
		if err != nil {
			logger.Error(ctx, "Failed to begin transaction for expiration", err)
			continue
		}

		deviceID, remainingMsat, err := sh.repo.GetActiveAuthorizationByID(ctx, tx, auth.AuthorizationID)
		if err != nil {
			_ = tx.Rollback()
			if errors.Is(err, sql.ErrNoRows) {
				logger.Debugf(ctx, "Authorization %s already processed, skipping", auth.AuthorizationID)
				continue
			}
			logger.Errorf(ctx, "Failed to load authorization %s: %v", auth.AuthorizationID, err)
			continue
		}

		var creditEntry *EntryResponse
		if remainingMsat > 0 {
			entry, err := sh.repo.ApplyCredit(ctx, tx, CreditRequest{
				DeviceID:      deviceID,
				AmountMsat:    remainingMsat,
				Reason:        authorizationExpiredReason,
				CorrelationID: auth.AuthorizationID,
			})
			if err != nil {
				_ = tx.Rollback()
				logger.WithDeviceID(deviceID).
					Errorf(ctx, "Failed to credit device for expired authorization %s: %v", auth.AuthorizationID, err)
				continue
			}
			creditEntry = &entry
		}

		if err := sh.repo.MarkAuthorizationExpired(ctx, tx, auth.AuthorizationID); err != nil {
			_ = tx.Rollback()
			logger.WithDeviceID(deviceID).
				Errorf(ctx, "Failed to update expired authorization %s: %v", auth.AuthorizationID, err)
			continue
		}

		if err := tx.Commit(); err != nil {
			logger.WithDeviceID(deviceID).
				Errorf(ctx, "Failed to commit expiration update for %s: %v", auth.AuthorizationID, err)
			continue
		}

		processed++

		// Publish expiration event
		timestamp := time.Now().Format(time.RFC3339)
		if err := sh.PublishAuthorizationExpired(ctx, auth.AuthorizationID, deviceID, timestamp); err != nil {
			logger.WithDeviceID(deviceID).
				WithStream("event.ledger", "produce").
				Error(ctx, "Failed to publish AuthorizationExpired event", err)
		}

		if creditEntry != nil {
			creditTimestamp := time.Unix(creditEntry.CreatedAt, 0).UTC().Format(time.RFC3339)
			if err := sh.PublishDeviceCredited(ctx, deviceID, creditEntry.AmountMsat, creditEntry.BalanceAfter, creditTimestamp); err != nil {
				logger.WithDeviceID(deviceID).
					WithStream("event.ledger", "produce").
					Errorf(ctx, "Failed to publish DeviceCreditedEvent for authorization %s: %v", auth.AuthorizationID, err)
			}
		}
	}

	if processed > 0 {
		logger.InfoWithFields(ctx, "Marked authorizations as expired", map[string]interface{}{
			"count": processed,
		})
	}

	return nil
}

// shouldRetryMessage checks if enough time has passed since last retry (exponential backoff)
func (sh *StreamHandler) shouldRetryMessage(ctx context.Context, messageID string) bool {
	_, shouldRetry := sh.shouldRetryMessageWithInfo(ctx, messageID, time.Now())
	return shouldRetry
}

// shouldRetryMessageWithInfo checks if enough time has passed and returns retry info
// This allows callers to get retry info without an extra lookup
func (sh *StreamHandler) shouldRetryMessageWithInfo(ctx context.Context, messageID string, now time.Time) (*messageRetryInfo, bool) {
	retryInfo := sh.getOrCreateRetryInfo(messageID)

	// First retry - allow immediately
	if retryInfo.retryCount == 0 {
		return retryInfo, true
	}

	// Calculate backoff duration based on retry count
	backoffDuration := sh.calculateBackoffDuration(retryInfo.retryCount)

	// Check if enough time has passed since last retry
	timeSinceLastRetry := now.Sub(retryInfo.lastRetryAt)
	shouldRetry := timeSinceLastRetry >= backoffDuration

	// Only log debug messages occasionally to reduce noise (when close to retry or randomly)
	// This reduces log spam when many messages are in backoff
	if !shouldRetry {
		remaining := backoffDuration - timeSinceLastRetry
		// Only log if remaining time is less than 5 seconds (close to retry) or randomly (1% chance)
		if remaining < 5*time.Second || now.UnixNano()%100 == 0 {
			logger.WithStream("", "consume").
				Debugf(ctx, "Message %s backoff not expired yet, remaining: %v (retry_count=%d)",
					messageID, remaining, retryInfo.retryCount)
		}
	}

	return retryInfo, shouldRetry
}

// calculateBackoffDuration calculates the backoff duration based on retry count
func (sh *StreamHandler) calculateBackoffDuration(retryCount int) time.Duration {
	// Exponential backoff: 2^retryCount seconds, capped at 5 minutes
	backoffSeconds := 1 << retryCount // 2^retryCount
	if backoffSeconds > 300 {         // Cap at 5 minutes
		backoffSeconds = 300
	}
	return time.Duration(backoffSeconds) * time.Second
}

// getOrCreateRetryInfo gets or creates retry info for a message
func (sh *StreamHandler) getOrCreateRetryInfo(messageID string) *messageRetryInfo {
	// Try to get existing info
	if val, ok := sh.retryTracker.Load(messageID); ok {
		return val.(*messageRetryInfo)
	}

	// Create new retry info
	info := &messageRetryInfo{
		retryCount:  0,
		lastRetryAt: time.Now(),
		firstSeenAt: time.Now(),
	}
	sh.retryTracker.Store(messageID, info)
	return info
}

// isDatabaseLockError checks if an error is a SQLite database lock error
func isDatabaseLockError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "database is locked") ||
		strings.Contains(errStr, "sqlite_busy") ||
		strings.Contains(errStr, "sqlite: database is locked")
}

// cleanupRetryTracker periodically cleans up old retry tracking entries
func (sh *StreamHandler) cleanupRetryTracker(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			cleaned := 0
			sh.retryTracker.Range(func(key, value interface{}) bool {
				info := value.(*messageRetryInfo)
				// Remove entries older than 1 hour that haven't been retried recently
				if now.Sub(info.firstSeenAt) > 1*time.Hour && now.Sub(info.lastRetryAt) > 30*time.Minute {
					sh.retryTracker.Delete(key)
					cleaned++
				}
				return true
			})
			if cleaned > 0 {
				logger.Debugf(ctx, "Cleaned up %d old retry tracking entries", cleaned)
			}
		}
	}
}
