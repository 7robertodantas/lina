package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/robertodantas/lnpay/internal"
	"go.opentelemetry.io/otel/attribute"
	_ "modernc.org/sqlite"
)

// ConsumptionRepository manages database operations for consumption records
type ConsumptionRepository struct {
	db        *sql.DB
	sqlTracer *internal.SQLTracer
}

// NewConsumptionRepository creates and initializes the SQLite database with schema
func NewConsumptionRepository(dbPath string, busyTimeoutMS int) (*ConsumptionRepository, error) {
	// WAL + busy_timeout + performance optimizations for high load
	// - WAL mode: allows concurrent readers and one writer
	// - busy_timeout: how long to wait when database is locked (in ms)
	// - synchronous(NORMAL): good balance between safety and performance with WAL
	// - cache_size: increase cache to 8MB (negative = KB, so -8192 = 8MB, default is -2000 = 2MB)
	// - temp_store: use memory for temporary tables/indexes (2 = memory)
	// - mmap_size: use memory-mapped I/O for better performance (268435456 = 256MB)
	// - foreign_keys: enable foreign key constraints
	dsn := fmt.Sprintf(
		"%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-8192)&_pragma=temp_store(2)&_pragma=mmap_size(268435456)&_pragma=foreign_keys(1)",
		dbPath, busyTimeoutMS,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
	}

	// Configure connection pool for SQLite
	// SQLite works best with limited connections due to its locking model
	// With WAL mode, we can have multiple readers but only one writer at a time
	// Set max open connections to a reasonable number (10-20 is good for WAL mode)
	db.SetMaxOpenConns(20)
	// Keep some connections idle for reuse
	db.SetMaxIdleConns(5)
	// Connection lifetime - close idle connections after 5 minutes
	db.SetConnMaxLifetime(5 * time.Minute)
	// Idle timeout - close idle connections after 10 minutes
	db.SetConnMaxIdleTime(10 * time.Minute)

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
			traceparent TEXT,
			created_at INTEGER NOT NULL
		)`,
		// Indexes for consumption_records
		`CREATE INDEX IF NOT EXISTS idx_device_id ON consumption_records (device_id)`,
		// Index for consumption_outbox
		`CREATE INDEX IF NOT EXISTS idx_published_created ON consumption_outbox (published, created_at)`,
		// Index for device_accumulation_ledger
		`CREATE INDEX IF NOT EXISTS idx_device_ledger ON device_accumulation_ledger (device_id, created_at)`,
	}

	repo := &ConsumptionRepository{
		db:        db,
		sqlTracer: internal.NewSQLTracer("repository.consumption"),
	}

	ctx := context.Background()
	attrs := []attribute.KeyValue{
		attribute.String("db.operation", "CREATE TABLE/INDEX"),
	}
	for _, s := range stmts {
		if _, err := repo.sqlTracer.ExecWithSpan(ctx, "[repository] create schema", attrs, db, s); err != nil {
			return nil, fmt.Errorf("failed to create schema: %w", err)
		}
	}

	return repo, nil
}

// OutboxEvent represents an event stored in the outbox
type OutboxEvent struct {
	ReportID     string
	DeviceID     string
	DebitMsat    int64
	Timestamp    string
	TraceContext map[string]string
}

// CheckReportExists checks if a report_id already exists (for idempotency)
func (r *ConsumptionRepository) CheckReportExists(ctx context.Context, tx *sql.Tx, reportID string) (bool, error) {
	query := `SELECT report_id FROM consumption_records WHERE report_id = ?`
	attrs := []attribute.KeyValue{
		attribute.String("db.operation", "SELECT"),
		attribute.String("db.table", "consumption_records"),
		attribute.String("report.id", reportID),
	}
	row := r.sqlTracer.QueryRowWithSpan(ctx, "[repository] check report exists", attrs, tx, query, reportID)

	var existingReportID string
	err := row.Scan(&existingReportID)

	if err == nil {
		return true, nil
	} else if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check idempotency: %w", err)
}

// CreateConsumptionRecord creates a new consumption record and outbox entry in a transaction
// Only creates outbox entry if debitMsat >= 1 (actual debit will occur)
func (r *ConsumptionRepository) CreateConsumptionRecord(ctx context.Context, tx *sql.Tx, reportID, deviceID string, debitMsat int64, measure float64, pricePerUnitMsat int64, unit, timestamp string, traceContext map[string]string) error {
	now := time.Now().Unix()

	// Insert into consumption_records
	attrs := []attribute.KeyValue{
		attribute.String("db.operation", "INSERT"),
		attribute.String("db.table", "consumption_records"),
		attribute.String("report.id", reportID),
		attribute.String("device.id", deviceID),
		attribute.Int64("debit_msat", debitMsat),
	}
	_, err := r.sqlTracer.ExecWithSpan(ctx, "[repository] create consumption record", attrs, tx, `
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
		// Extract W3C traceparent from trace context (single string, not JSON)
		traceparent := ""
		if traceContext != nil {
			// W3C Trace Context uses "traceparent" key
			traceparent = traceContext["traceparent"]
		}

		outboxAttrs := []attribute.KeyValue{
			attribute.String("db.operation", "INSERT"),
			attribute.String("db.table", "consumption_outbox"),
			attribute.String("report.id", reportID),
		}
		_, err := r.sqlTracer.ExecWithSpan(ctx, "[repository] create outbox entry", outboxAttrs, tx, `
			INSERT INTO consumption_outbox (report_id, published, traceparent, created_at)
			VALUES (?, 0, ?, ?)`,
			reportID, traceparent, now,
		)
		if err != nil {
			return fmt.Errorf("failed to insert into outbox: %w", err)
		}
	}

	return nil
}

// GetUnpublishedOutboxEvents retrieves unpublished events from the outbox
func (r *ConsumptionRepository) GetUnpublishedOutboxEvents(ctx context.Context, limit int) ([]OutboxEvent, error) {
	query := `
		SELECT o.report_id, c.device_id, c.debit_msat, c.timestamp, o.traceparent
		FROM consumption_outbox o
		INNER JOIN consumption_records c ON o.report_id = c.report_id
		WHERE o.published = 0
		ORDER BY c.created_at ASC
		LIMIT ?
	`

	attrs := []attribute.KeyValue{
		attribute.String("db.operation", "SELECT"),
		attribute.String("db.table", "consumption_outbox"),
		attribute.Int("limit", limit),
	}
	rows, err := r.sqlTracer.QueryWithSpan(ctx, "[repository] get unpublished outbox events", attrs, r.db, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query unpublished outbox events: %w", err)
	}
	defer rows.Close()

	var events []OutboxEvent
	for rows.Next() {
		var e OutboxEvent
		var traceparent sql.NullString
		if err := rows.Scan(&e.ReportID, &e.DeviceID, &e.DebitMsat, &e.Timestamp, &traceparent); err != nil {
			return nil, fmt.Errorf("failed to scan outbox event: %w", err)
		}

		// Reconstruct trace context map from W3C traceparent
		if traceparent.Valid && traceparent.String != "" {
			e.TraceContext = map[string]string{
				"traceparent": traceparent.String,
			}
		}

		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating outbox events: %w", err)
	}

	return events, nil
}

// MarkOutboxAsPublished marks an outbox entry as published
func (r *ConsumptionRepository) MarkOutboxAsPublished(ctx context.Context, reportID string) error {
	query := `
		UPDATE consumption_outbox
		SET published = 1, published_at = ?
		WHERE report_id = ?`
	attrs := []attribute.KeyValue{
		attribute.String("db.operation", "UPDATE"),
		attribute.String("db.table", "consumption_outbox"),
		attribute.String("report.id", reportID),
	}
	if _, err := r.sqlTracer.ExecWithSpan(ctx, "[repository] mark outbox as published", attrs, r.db, query, time.Now().Unix(), reportID); err != nil {
		return fmt.Errorf("failed to mark report %s as published: %w", reportID, err)
	}
	return nil
}

// CleanupOutbox removes published records older than the retention period
func (r *ConsumptionRepository) CleanupOutbox(ctx context.Context, retentionDays int) (int64, error) {
	retentionSeconds := int64(retentionDays * 24 * 60 * 60)
	cutoffTime := time.Now().Unix() - retentionSeconds

	query := `
		DELETE FROM consumption_outbox
		WHERE published = 1 AND published_at < ?`
	attrs := []attribute.KeyValue{
		attribute.String("db.operation", "DELETE"),
		attribute.String("db.table", "consumption_outbox"),
		attribute.Int("retention_days", retentionDays),
	}
	result, err := r.sqlTracer.ExecWithSpan(ctx, "[repository] cleanup outbox", attrs, r.db, query, cutoffTime)
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
	query := `
		SELECT accumulated_balance_msat
		FROM device_accumulation_ledger
		WHERE device_id = ?
		ORDER BY id DESC
		LIMIT 1`
	attrs := []attribute.KeyValue{
		attribute.String("db.operation", "SELECT"),
		attribute.String("db.table", "device_accumulation_ledger"),
		attribute.String("device.id", deviceID),
	}
	row := r.sqlTracer.QueryRowWithSpan(ctx, "[repository] get device accumulated amount", attrs, tx, query, deviceID)

	var balance sql.NullFloat64
	err := row.Scan(&balance)

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
	query := `
		INSERT INTO device_accumulation_ledger (device_id, report_id, type, amount_msat, accumulated_balance_msat, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`
	attrs := []attribute.KeyValue{
		attribute.String("db.operation", "INSERT"),
		attribute.String("db.table", "device_accumulation_ledger"),
		attribute.String("device.id", deviceID),
		attribute.String("report.id", reportID),
		attribute.String("entry.type", entryType),
		attribute.Float64("amount_msat", amountMsat),
		attribute.Float64("new_balance_msat", newBalance),
	}
	if _, err := r.sqlTracer.ExecWithSpan(ctx, "[repository] append accumulation ledger", attrs, tx, query,
		deviceID, reportID, entryType, amountMsat, newBalance, now,
	); err != nil {
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

// ListDeviceConsumptions retrieves consumption records for a device with outbox status
func (r *ConsumptionRepository) ListDeviceConsumptions(ctx context.Context, deviceID string, limit int) ([]ConsumptionResponse, error) {
	query := `
		SELECT 
			c.report_id, 
			c.device_id, 
			c.debit_msat, 
			c.measure, 
			c.price_per_unit_msat, 
			c.unit, 
			c.timestamp, 
			c.created_at,
			COALESCE(o.published, 0) as published,
			o.traceparent
		FROM consumption_records c
		LEFT JOIN consumption_outbox o ON c.report_id = o.report_id
		WHERE c.device_id = ?
		ORDER BY c.created_at DESC
		LIMIT ?
	`

	attrs := []attribute.KeyValue{
		attribute.String("db.operation", "SELECT"),
		attribute.String("db.table", "consumption_records"),
		attribute.String("device.id", deviceID),
		attribute.Int("limit", limit),
	}
	rows, err := r.sqlTracer.QueryWithSpan(ctx, "[repository] list device consumptions", attrs, r.db, query, deviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query device consumptions: %w", err)
	}
	defer rows.Close()

	var results []ConsumptionResponse
	for rows.Next() {
		var resp ConsumptionResponse
		var published int
		var traceparent sql.NullString

		if err := rows.Scan(
			&resp.ReportID,
			&resp.DeviceID,
			&resp.DebitMsat,
			&resp.Measure,
			&resp.PricePerUnitMsat,
			&resp.Unit,
			&resp.Timestamp,
			&resp.CreatedAt,
			&published,
			&traceparent,
		); err != nil {
			return nil, fmt.Errorf("failed to scan consumption: %w", err)
		}

		resp.Published = published == 1
		if traceparent.Valid {
			resp.Traceparent = traceparent.String
		}

		results = append(results, resp)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating consumptions: %w", err)
	}

	return results, nil
}

// ListDeviceAccumulations retrieves accumulation ledger entries for a device
func (r *ConsumptionRepository) ListDeviceAccumulations(ctx context.Context, deviceID string, limit int) ([]AccumulationResponse, error) {
	query := `
		SELECT 
			id, 
			device_id, 
			report_id, 
			type, 
			amount_msat, 
			accumulated_balance_msat, 
			created_at
		FROM device_accumulation_ledger
		WHERE device_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`

	attrs := []attribute.KeyValue{
		attribute.String("db.operation", "SELECT"),
		attribute.String("db.table", "device_accumulation_ledger"),
		attribute.String("device.id", deviceID),
		attribute.Int("limit", limit),
	}
	rows, err := r.sqlTracer.QueryWithSpan(ctx, "[repository] list device accumulations", attrs, r.db, query, deviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query device accumulations: %w", err)
	}
	defer rows.Close()

	var results []AccumulationResponse
	for rows.Next() {
		var resp AccumulationResponse

		if err := rows.Scan(
			&resp.ID,
			&resp.DeviceID,
			&resp.ReportID,
			&resp.Type,
			&resp.AmountMsat,
			&resp.AccumulatedBalanceMsat,
			&resp.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan accumulation: %w", err)
		}

		results = append(results, resp)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating accumulations: %w", err)
	}

	return results, nil
}
