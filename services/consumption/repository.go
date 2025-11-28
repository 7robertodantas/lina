package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ConsumptionRepository manages database operations for consumption records
type ConsumptionRepository struct {
	db *sql.DB
}

// NewConsumptionRepository creates and initializes the SQLite database with schema
func NewConsumptionRepository(dbPath string, busyTimeoutMS int) (*ConsumptionRepository, error) {
	// WAL + busy_timeout for concurrent writers on edge devices.
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath, busyTimeoutMS)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
	}

	// Create tables and indexes
	stmts := []string{
		// Consumption records table - stores processed usage records per device_id with idempotency
		// This is the source of truth for business data
		`CREATE TABLE IF NOT EXISTS consumption_records (
			report_id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL,
			debit_msat INTEGER NOT NULL,
			measure REAL NOT NULL,
			price_per_unit_msat INTEGER NOT NULL,
			unit TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		// Device accumulation ledger - append-only log of fractional msat accumulations per device
		// Each entry represents either:
		// - A new fractional amount added (type='add')
		// - A consumption of accumulated amount when debited (type='consume')
		// accumulated_balance_msat stores the running balance AFTER this entry for O(1) lookups
		`CREATE TABLE IF NOT EXISTS device_accumulation_ledger (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL,
			report_id TEXT NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('add', 'consume')),
			amount_msat REAL NOT NULL,
			accumulated_balance_msat REAL NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		// Outbox table - minimal table for transactional outbox pattern
		// References consumption_records via report_id (acts as foreign key)
		// Only stores what's needed for publishing: report_id and published status
		`CREATE TABLE IF NOT EXISTS consumption_outbox (
			report_id TEXT PRIMARY KEY,
			published INTEGER NOT NULL DEFAULT 0,
			published_at INTEGER,
			created_at INTEGER NOT NULL
		)`,
		// Indexes for consumption_records
		`CREATE INDEX IF NOT EXISTS idx_device_id ON consumption_records (device_id)`,
		// Index for consumption_outbox
		`CREATE INDEX IF NOT EXISTS idx_published_created ON consumption_outbox (published, created_at)`,
		// Index for device_accumulation_ledger
		`CREATE INDEX IF NOT EXISTS idx_device_ledger ON device_accumulation_ledger (device_id, created_at)`,
	}

	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, fmt.Errorf("failed to create schema: %w", err)
		}
	}

	return &ConsumptionRepository{db: db}, nil
}

// OutboxEvent represents an unpublished event from the outbox
type OutboxEvent struct {
	ReportID  string
	DeviceID  string
	DebitMsat int64
	Timestamp string
}

// CheckReportExists checks if a report_id already exists (for idempotency)
func (r *ConsumptionRepository) CheckReportExists(ctx context.Context, tx *sql.Tx, reportID string) (bool, error) {
	var existingReportID string
	err := tx.QueryRowContext(ctx, `
		SELECT report_id FROM consumption_records WHERE report_id = ?`,
		reportID,
	).Scan(&existingReportID)

	if err == nil {
		return true, nil
	} else if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check idempotency: %w", err)
}

// CreateConsumptionRecord creates a new consumption record and outbox entry in a transaction
// Only creates outbox entry if debitMsat >= 1 (actual debit will occur)
func (r *ConsumptionRepository) CreateConsumptionRecord(ctx context.Context, tx *sql.Tx, reportID, deviceID string, debitMsat int64, measure float64, pricePerUnitMsat int64, unit, timestamp string) error {
	now := time.Now().Unix()

	// Insert into consumption_records
	_, err := tx.ExecContext(ctx, `
		INSERT INTO consumption_records (
			report_id, device_id, debit_msat,
			measure, price_per_unit_msat, unit, timestamp, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		reportID, deviceID, debitMsat,
		measure, pricePerUnitMsat, unit, timestamp, now,
	)
	if err != nil {
		return fmt.Errorf("failed to insert consumption record: %w", err)
	}

	// Only insert into outbox if there's an actual debit to publish (>= 1 msat)
	// If debitMsat is 0, the fractional amount was accumulated but not debited yet
	if debitMsat >= 1 {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO consumption_outbox (
				report_id, published, created_at
			) VALUES (?, 0, ?)`,
			reportID, now,
		)
		if err != nil {
			return fmt.Errorf("failed to insert into outbox: %w", err)
		}
	}

	return nil
}

// GetUnpublishedOutboxEvents retrieves unpublished events from the outbox
func (r *ConsumptionRepository) GetUnpublishedOutboxEvents(ctx context.Context, limit int) ([]OutboxEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT o.report_id, r.device_id, r.debit_msat, r.timestamp
		FROM consumption_outbox o
		INNER JOIN consumption_records r ON o.report_id = r.report_id
		WHERE o.published = 0
		ORDER BY o.created_at ASC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query outbox: %w", err)
	}
	defer rows.Close()

	var events []OutboxEvent
	for rows.Next() {
		var e OutboxEvent
		if err := rows.Scan(&e.ReportID, &e.DeviceID, &e.DebitMsat, &e.Timestamp); err != nil {
			logger.Error("Error scanning outbox row", err)
			continue
		}
		events = append(events, e)
	}

	return events, nil
}

// MarkOutboxAsPublished marks an outbox entry as published
func (r *ConsumptionRepository) MarkOutboxAsPublished(ctx context.Context, reportID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE consumption_outbox
		SET published = 1, published_at = ?
		WHERE report_id = ?`,
		time.Now().Unix(), reportID,
	)
	if err != nil {
		return fmt.Errorf("failed to mark report %s as published: %w", reportID, err)
	}
	return nil
}

// CleanupOutbox removes published records older than the retention period
func (r *ConsumptionRepository) CleanupOutbox(ctx context.Context, retentionDays int) (int64, error) {
	retentionSeconds := int64(retentionDays * 24 * 60 * 60)
	cutoffTime := time.Now().Unix() - retentionSeconds

	result, err := r.db.ExecContext(ctx, `
		DELETE FROM consumption_outbox
		WHERE published = 1 AND published_at < ?`,
		cutoffTime,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup outbox: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected, nil
}

// GetDeviceAccumulatedAmount retrieves the current accumulated fractional msat for a device
// by reading the most recent ledger entry's balance (O(1) lookup)
func (r *ConsumptionRepository) GetDeviceAccumulatedAmount(ctx context.Context, tx *sql.Tx, deviceID string) (float64, error) {
	var balance sql.NullFloat64
	err := tx.QueryRowContext(ctx, `
		SELECT accumulated_balance_msat
		FROM device_accumulation_ledger
		WHERE device_id = ?
		ORDER BY id DESC
		LIMIT 1`,
		deviceID,
	).Scan(&balance)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No entries yet for this device
			return 0.0, nil
		}
		return 0.0, fmt.Errorf("failed to get device accumulated amount: %w", err)
	}

	if !balance.Valid {
		return 0.0, nil
	}

	return balance.Float64, nil
}

// AppendAccumulationLedger appends an entry to the device accumulation ledger
// type should be 'add' (adding fractional msat) or 'consume' (consuming accumulated amount)
// newBalance is the running balance AFTER this entry (pre-calculated by caller)
func (r *ConsumptionRepository) AppendAccumulationLedger(ctx context.Context, tx *sql.Tx, deviceID, reportID, entryType string, amountMsat, newBalance float64) error {
	if entryType != "add" && entryType != "consume" {
		return fmt.Errorf("invalid ledger entry type: %s (must be 'add' or 'consume')", entryType)
	}

	if amountMsat < 0 {
		return fmt.Errorf("amount_msat cannot be negative: %f", amountMsat)
	}

	if newBalance < 0 {
		return fmt.Errorf("accumulated_balance_msat cannot be negative: %f", newBalance)
	}

	now := time.Now().Unix()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO device_accumulation_ledger (device_id, report_id, type, amount_msat, accumulated_balance_msat, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		deviceID, reportID, entryType, amountMsat, newBalance, now,
	)

	if err != nil {
		return fmt.Errorf("failed to append to accumulation ledger: %w", err)
	}

	return nil
}

// BeginTx starts a new transaction
func (r *ConsumptionRepository) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, opts)
}

// Close closes the database connection
func (r *ConsumptionRepository) Close() error {
	return r.db.Close()
}
