package main

import (
	"context"
	"fmt"

	"github.com/robertodantas/lina/internal"
)

// OutboxEvent represents an event stored in the outbox.
type OutboxEvent struct {
	ReportID     string
	DeviceID     string
	DebitMsat    int64
	Timestamp    string // device-reported usage time
	CreatedAt    int64
	TraceContext map[string]string
}

// ConsumptionRepository is implemented by Pebble and SQLite backends.
type ConsumptionRepository interface {
	CreateConsumptionRecord(ctx context.Context, reportID, deviceID string, debitMsat int64, fractionalMsat float64, measure float64, pricePerUnitMsat int64, unit, timestamp string, traceContext map[string]string) (inserted bool, err error)
	GetUnpublishedOutboxEvents(ctx context.Context, limit int) ([]OutboxEvent, error)
	MarkOutboxAsPublished(ctx context.Context, reportID string) error
	CleanupOutbox(ctx context.Context, retentionDays int) (int64, error)
	Close() error
	ListDeviceConsumptions(ctx context.Context, deviceID string, limit int) ([]ConsumptionResponse, error)
}

// OpenConsumptionRepository opens the configured consumption backend.
// resolvedPath is the actual filesystem path used (Pebble directory or SQLite file).
func OpenConsumptionRepository(cfg Config) (repo ConsumptionRepository, implementation string, resolvedPath string, err error) {
	switch cfg.RepositoryType {
	case "", "pebble":
		path := internal.PebbleStorePath(cfg.DBPath)
		r, e := openConsumptionRepoPebble(path)
		if e != nil {
			return nil, "", "", e
		}
		return r, "pebble", path, nil
	case "sqlite":
		path := internal.SQLiteDBPath(cfg.DBPath)
		r, e := openConsumptionRepoSQLite(path, cfg.BusyTimeoutMS)
		if e != nil {
			return nil, "", "", e
		}
		return r, "sqlite", path, nil
	default:
		return nil, "", "", fmt.Errorf("unsupported REPOSITORY_TYPE %q (want pebble or sqlite)", cfg.RepositoryType)
	}
}
