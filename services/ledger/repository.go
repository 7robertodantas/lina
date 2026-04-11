package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/robertodantas/lina/internal"
	ledgermodel "github.com/robertodantas/lina/proto/gen/model/ledger"
)

// ErrNotFound is returned when a looked-up row or key does not exist (replaces sql.ErrNoRows).
var ErrNotFound = errors.New("ledger: not found")

// LedgerTx is a committed transaction handle (Pebble batch or sql.Tx).
type LedgerTx interface {
	Commit() error
	Rollback() error
}

// LedgerTxOptions controls BeginTx behavior (read-only vs read-write).
type LedgerTxOptions struct {
	ReadOnly bool
}

// LedgerRepository is implemented by Pebble and SQLite backends.
type LedgerRepository interface {
	BeginTx(ctx context.Context, opts *LedgerTxOptions) (LedgerTx, error)
	Close() error

	EnsureBalanceRow(ctx context.Context, tx LedgerTx, deviceID string) error
	GetBalance(ctx context.Context, tx LedgerTx, deviceID string) (int64, error)
	UpdateBalance(ctx context.Context, tx LedgerTx, deviceID string, amountMsat int64) error
	CreateLedgerEntry(ctx context.Context, tx LedgerTx, entry EntryResponse) error
	ListLedgerEntries(ctx context.Context, deviceID string, cursorCreated int64, cursorID string, limit int) ([]EntryResponse, error)
	ApplyCredit(ctx context.Context, tx LedgerTx, in CreditRequest) (EntryResponse, error)
	ApplyDebit(ctx context.Context, tx LedgerTx, in DebitRequest) (EntryResponse, error)
	GetCachedIdem(ctx context.Context, key string) (kind string, resp []byte, ok bool, err error)
	SaveIdem(ctx context.Context, tx LedgerTx, key, kind, reqHash string, response any) error
	CreateAuthorization(ctx context.Context, tx LedgerTx, authID, deviceID, requestID string, grantedMsat int64, issuedAt, expiresAt string) error
	GetAuthorizationByRequestID(ctx context.Context, tx LedgerTx, requestID string) (*ledgermodel.Authorization, string, error)
	GetActiveAuthorization(ctx context.Context, tx LedgerTx, deviceID string, expiresAfter string) (authorizationID string, remainingMsat int64, grantedMsat int64, overflowMsat int64, expiresAt string, status string, err error)
	GetActiveAuthorizationForDevice(ctx context.Context, tx LedgerTx, deviceID string) (*ledgermodel.Authorization, string, error)
	UpdateAuthorization(ctx context.Context, tx LedgerTx, authorizationID string, remainingMsat int64, consumedMsat int64, overflowMsat int64, status string) error
	ConsumeAuthorization(ctx context.Context, tx LedgerTx, authorizationID string, debitAmount int64) (newRemaining int64, newConsumed int64, newOverflow int64, newStatus string, err error)
	GetExpiredAuthorizations(ctx context.Context, expiresBefore string) ([]ExpiredAuthorization, error)
	GetActiveAuthorizationByID(ctx context.Context, tx LedgerTx, authorizationID string) (deviceID string, remainingMsat int64, err error)
	MarkAuthorizationExpired(ctx context.Context, tx LedgerTx, authorizationID string) error
	ListAuthorizations(ctx context.Context, deviceID string, statusFilter string) ([]AuthorizationResponse, error)
}

func now() int64 { return time.Now().Unix() }

// ExpiredAuthorization represents an expired authorization row reference.
type ExpiredAuthorization struct {
	AuthorizationID string
	DeviceID        string
	ExpiresAt       string
}

// OpenLedgerRepository opens the configured ledger backend.
// resolvedPath is the actual filesystem path used (Pebble directory or SQLite file).
func OpenLedgerRepository(cfg Config) (repo LedgerRepository, implementation string, resolvedPath string, err error) {
	switch cfg.RepositoryType {
	case "", "pebble":
		path := internal.PebbleStorePath(cfg.DBPath)
		r, e := openLedgerRepoPebble(path)
		if e != nil {
			return nil, "", "", e
		}
		return r, "pebble", path, nil
	case "sqlite":
		path := internal.SQLiteDBPath(cfg.DBPath)
		r, e := openLedgerRepoSQLite(path, cfg.BusyTimeoutMS)
		if e != nil {
			return nil, "", "", e
		}
		return r, "sqlite", path, nil
	default:
		return nil, "", "", fmt.Errorf("unsupported REPOSITORY_TYPE %q (want pebble or sqlite)", cfg.RepositoryType)
	}
}
