package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestLedgerRepo(t *testing.T) LedgerRepository {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "ledger-pebble")
	repo, err := openLedgerRepoPebble(dir)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = repo.Close()
	})

	return repo
}

func TestApplyCreditCreatesLedgerEntry(t *testing.T) {
	repo := newTestLedgerRepo(t)
	ctx := context.Background()

	tx, err := repo.BeginTx(ctx, &LedgerTxOptions{})
	require.NoError(t, err)

	req := CreditRequest{
		DeviceID:   "device-credit-1",
		AmountMsat: 5_000,
		Reason:     "topup",
	}

	entry, err := repo.ApplyCredit(ctx, tx, req)
	require.NoError(t, err)
	require.Equal(t, req.DeviceID, entry.DeviceID)
	require.Equal(t, "credit", entry.EntryType)
	require.Equal(t, req.AmountMsat, entry.AmountMsat)
	require.Equal(t, req.AmountMsat, entry.BalanceAfter)

	require.NoError(t, tx.Commit())

	checkTx, err := repo.BeginTx(ctx, &LedgerTxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer checkTx.Rollback()

	balance, err := repo.GetBalance(ctx, checkTx, req.DeviceID)
	require.NoError(t, err)
	require.Equal(t, req.AmountMsat, balance)
}

func TestApplyDebitPreventsNegativeBalance(t *testing.T) {
	repo := newTestLedgerRepo(t)
	ctx := context.Background()

	tx, err := repo.BeginTx(ctx, &LedgerTxOptions{})
	require.NoError(t, err)
	_, err = repo.ApplyCredit(ctx, tx, CreditRequest{DeviceID: "device-debit-1", AmountMsat: 2_000})
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	tx2, err := repo.BeginTx(ctx, &LedgerTxOptions{})
	require.NoError(t, err)

	_, err = repo.ApplyDebit(ctx, tx2, DebitRequest{DeviceID: "device-debit-1", AmountMsat: 3_000})
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient funds")
	_ = tx2.Rollback()
}

func TestApplyDebitReducesBalance(t *testing.T) {
	repo := newTestLedgerRepo(t)
	ctx := context.Background()

	tx, err := repo.BeginTx(ctx, &LedgerTxOptions{})
	require.NoError(t, err)
	_, err = repo.ApplyCredit(ctx, tx, CreditRequest{DeviceID: "device-debit-2", AmountMsat: 5_000})
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	tx2, err := repo.BeginTx(ctx, &LedgerTxOptions{})
	require.NoError(t, err)
	entry, err := repo.ApplyDebit(ctx, tx2, DebitRequest{DeviceID: "device-debit-2", AmountMsat: 1_500})
	require.NoError(t, err)
	require.Equal(t, int64(3_500), entry.BalanceAfter)
	require.NoError(t, tx2.Commit())

	checkTx, err := repo.BeginTx(ctx, &LedgerTxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer checkTx.Rollback()

	balance, err := repo.GetBalance(ctx, checkTx, "device-debit-2")
	require.NoError(t, err)
	require.Equal(t, int64(3_500), balance)
}

func TestMarkAuthorizationExpiredZeroesRemainingAndFetchesActive(t *testing.T) {
	repo := newTestLedgerRepo(t)
	ctx := context.Background()

	tx, err := repo.BeginTx(ctx, &LedgerTxOptions{})
	require.NoError(t, err)

	now := time.Now().UTC()
	authID := "auth-expire-1"
	deviceID := "device-expire-1"
	err = repo.CreateAuthorization(
		ctx,
		tx,
		authID,
		deviceID,
		"req-expire-1",
		2_000,
		now.Format(time.RFC3339),
		now.Add(time.Minute).Format(time.RFC3339),
	)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	tx2, err := repo.BeginTx(ctx, &LedgerTxOptions{})
	require.NoError(t, err)

	gotDeviceID, remaining, err := repo.GetActiveAuthorizationByID(ctx, tx2, authID)
	require.NoError(t, err)
	require.Equal(t, deviceID, gotDeviceID)
	require.Equal(t, int64(2_000), remaining)

	require.NoError(t, repo.UpdateAuthorization(ctx, tx2, authID, 500, 1_500, 100, "active"))
	require.NoError(t, tx2.Commit())

	tx3, err := repo.BeginTx(ctx, &LedgerTxOptions{})
	require.NoError(t, err)
	require.NoError(t, repo.MarkAuthorizationExpired(ctx, tx3, authID))
	require.NoError(t, tx3.Commit())

	checkTx, err := repo.BeginTx(ctx, &LedgerTxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer checkTx.Rollback()

	rec, err := repo.(*ledgerRepoPebble).loadAuthorization(ctx, checkTx.(*pebbleLedgerTx), authID)
	require.NoError(t, err)
	require.Equal(t, "expired", rec.Status)
	require.Equal(t, int64(0), rec.RemainingMsat)
	require.Equal(t, int64(1_500), rec.ConsumedMsat)
	require.Equal(t, int64(100), rec.OverflowMsat)
}

func TestUpdateAuthorizationTracksConsumed(t *testing.T) {
	repo := newTestLedgerRepo(t)
	ctx := context.Background()

	tx, err := repo.BeginTx(ctx, &LedgerTxOptions{})
	require.NoError(t, err)

	now := time.Now().UTC()
	authID := "auth-consumed-1"
	deviceID := "device-consumed-1"
	err = repo.CreateAuthorization(
		ctx,
		tx,
		authID,
		deviceID,
		"req-consumed-1",
		3_000,
		now.Format(time.RFC3339),
		now.Add(time.Minute).Format(time.RFC3339),
	)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	tx2, err := repo.BeginTx(ctx, &LedgerTxOptions{})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateAuthorization(ctx, tx2, authID, 1_000, 2_000, 250, "active"))
	require.NoError(t, tx2.Commit())

	checkTx, err := repo.BeginTx(ctx, &LedgerTxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer checkTx.Rollback()

	rec, err := repo.(*ledgerRepoPebble).loadAuthorization(ctx, checkTx.(*pebbleLedgerTx), authID)
	require.NoError(t, err)
	require.Equal(t, int64(1_000), rec.RemainingMsat)
	require.Equal(t, int64(2_000), rec.ConsumedMsat)
	require.Equal(t, int64(250), rec.OverflowMsat)
}
