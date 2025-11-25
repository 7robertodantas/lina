package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestLedgerRepo(t *testing.T) *LedgerRepository {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "ledger.db")
	repo, err := NewLedgerRepository(dbPath, 1000)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = repo.Close()
	})

	return repo
}

func TestApplyCreditCreatesLedgerEntry(t *testing.T) {
	repo := newTestLedgerRepo(t)
	ctx := context.Background()

	tx, err := repo.BeginTx(ctx, &sql.TxOptions{})
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

	checkTx, err := repo.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer checkTx.Rollback()

	balance, err := repo.GetBalance(ctx, checkTx, req.DeviceID)
	require.NoError(t, err)
	require.Equal(t, req.AmountMsat, balance)
}

func TestApplyDebitPreventsNegativeBalance(t *testing.T) {
	repo := newTestLedgerRepo(t)
	ctx := context.Background()

	tx, err := repo.BeginTx(ctx, &sql.TxOptions{})
	require.NoError(t, err)
	_, err = repo.ApplyCredit(ctx, tx, CreditRequest{DeviceID: "device-debit-1", AmountMsat: 2_000})
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	tx2, err := repo.BeginTx(ctx, &sql.TxOptions{})
	require.NoError(t, err)

	_, err = repo.ApplyDebit(ctx, tx2, DebitRequest{DeviceID: "device-debit-1", AmountMsat: 3_000})
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient funds")
	_ = tx2.Rollback()
}

func TestApplyDebitReducesBalance(t *testing.T) {
	repo := newTestLedgerRepo(t)
	ctx := context.Background()

	tx, err := repo.BeginTx(ctx, &sql.TxOptions{})
	require.NoError(t, err)
	_, err = repo.ApplyCredit(ctx, tx, CreditRequest{DeviceID: "device-debit-2", AmountMsat: 5_000})
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	tx2, err := repo.BeginTx(ctx, &sql.TxOptions{})
	require.NoError(t, err)
	entry, err := repo.ApplyDebit(ctx, tx2, DebitRequest{DeviceID: "device-debit-2", AmountMsat: 1_500})
	require.NoError(t, err)
	require.Equal(t, int64(3_500), entry.BalanceAfter)
	require.NoError(t, tx2.Commit())

	checkTx, err := repo.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer checkTx.Rollback()

	balance, err := repo.GetBalance(ctx, checkTx, "device-debit-2")
	require.NoError(t, err)
	require.Equal(t, int64(3_500), balance)
}
