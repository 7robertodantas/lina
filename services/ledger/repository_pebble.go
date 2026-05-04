package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/google/uuid"
	ledgermodel "github.com/robertodantas/lina/proto/gen/model/ledger"
)

func expectPebbleTx(tx LedgerTx) (*pebbleLedgerTx, error) {
	if tx == nil {
		return nil, errors.New("ledger: nil transaction")
	}
	pt, ok := tx.(*pebbleLedgerTx)
	if !ok {
		return nil, fmt.Errorf("ledger: expected pebble transaction, got %T", tx)
	}
	return pt, nil
}

// Key prefixes — device_id, authorization_id, entry_id, request_id must not contain '/'.
const (
	keyPrefixBalance          = "balance/"
	keyPrefixLedgerEntry      = "ledger/entry/"
	keyPrefixLedgerByDevice   = "ledger/by_device/"
	keyPrefixIdem             = "idem/"
	keyPrefixAuthRecord       = "auth/record/"
	keyPrefixAuthByRequest    = "auth/by_request/"
	keyPrefixAuthByDevice     = "auth/by_device/"
	keyPrefixAuthActiveByDev  = "auth/active_by_device/"
)

// ledgerRepoPebble is the Pebble implementation of LedgerRepository.
type ledgerRepoPebble struct {
	db *pebble.DB
}

// pebbleLedgerTx is a read-write Pebble batch or a read-only view over the DB.
type pebbleLedgerTx struct {
	db       *pebble.DB
	batch    *pebble.Batch
	readOnly bool
}

// Commit applies the batch (sync). No-op for read-only transactions.
func (t *pebbleLedgerTx) Commit() error {
	if t.readOnly || t.batch == nil {
		return nil
	}
	err := t.batch.Commit(&pebble.WriteOptions{Sync: true})
	_ = t.batch.Close()
	t.batch = nil
	return err
}

// Rollback discards the batch.
func (t *pebbleLedgerTx) Rollback() error {
	if t.batch != nil {
		_ = t.batch.Close()
		t.batch = nil
	}
	return nil
}

// openLedgerRepoPebble opens a Pebble store at storePath (directory).
func openLedgerRepoPebble(storePath string) (LedgerRepository, error) {
	db, err := pebble.Open(storePath, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("open pebble store: %w", err)
	}
	return &ledgerRepoPebble{db: db}, nil
}

func (r *ledgerRepoPebble) getRaw(tx *pebbleLedgerTx, key []byte) ([]byte, io.Closer, error) {
	if tx != nil && tx.batch != nil {
		return tx.batch.Get(key)
	}
	return r.db.Get(key)
}

func (r *ledgerRepoPebble) setRaw(tx *pebbleLedgerTx, key, val []byte) error {
	if tx == nil || tx.batch == nil {
		return errors.New("ledger: write requires a write transaction")
	}
	return tx.batch.Set(key, val, nil)
}

// BeginTx starts a read-only session or a write batch.
func (r *ledgerRepoPebble) BeginTx(ctx context.Context, opts *LedgerTxOptions) (LedgerTx, error) {
	_ = ctx
	if opts != nil && opts.ReadOnly {
		return &pebbleLedgerTx{db: r.db, readOnly: true}, nil
	}
	return &pebbleLedgerTx{db: r.db, batch: r.db.NewIndexedBatch()}, nil
}

// Close closes the Pebble store.
func (r *ledgerRepoPebble) Close() error {
	return r.db.Close()
}


type storedBalance struct {
	BalanceMsat int64 `json:"balance_msat"`
	UpdatedAt   int64 `json:"updated_at"`
}

type storedIdem struct {
	Kind         string `json:"kind"`
	RequestHash  string `json:"request_hash"`
	ResponseJSON string `json:"response_json"`
	CreatedAt    int64  `json:"created_at"`
}

type storedAuthorization struct {
	AuthorizationID string `json:"authorization_id"`
	DeviceID        string `json:"device_id"`
	RequestID       string `json:"request_id"`
	GrantedMsat     int64  `json:"granted_msat"`
	RemainingMsat   int64  `json:"remaining_msat"`
	ConsumedMsat    int64  `json:"consumed_msat"`
	OverflowMsat    int64  `json:"overflow_msat"`
	IssuedAt        string `json:"issued_at"`
	ExpiresAt       string `json:"expires_at"`
	Status          string `json:"status"`
	CreatedAt       int64  `json:"created_at"`
}

type storedLedgerEntry struct {
	EntryID       string `json:"entry_id"`
	DeviceID      string `json:"device_id"`
	EntryType     string `json:"entry_type"`
	AmountMsat    int64  `json:"amount_msat"`
	BalanceAfter  int64  `json:"balance_after"`
	Reason        string `json:"reason,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	CreatedAt     int64  `json:"created_at"`
}

func keyBalance(deviceID string) []byte {
	return []byte(keyPrefixBalance + deviceID)
}

func keyLedgerEntry(entryID string) []byte {
	return []byte(keyPrefixLedgerEntry + entryID)
}

func keyLedgerByDevice(deviceID string, createdAt int64, entryID string) []byte {
	inv := invCreatedUnix(createdAt)
	return []byte(fmt.Sprintf("%s%s/%016x/%s", keyPrefixLedgerByDevice, deviceID, inv, entryID))
}

func invCreatedUnix(createdAt int64) uint64 {
	return ^uint64(0) - uint64(createdAt)
}

func keyIdem(idemKey string) []byte {
	return []byte(keyPrefixIdem + idemKey)
}

func keyAuthRecord(authID string) []byte {
	return []byte(keyPrefixAuthRecord + authID)
}

func keyAuthByRequest(requestID string) []byte {
	return []byte(keyPrefixAuthByRequest + requestID)
}

func keyAuthByDevice(deviceID string, createdAt int64, authID string) []byte {
	inv := invCreatedUnix(createdAt)
	return []byte(fmt.Sprintf("%s%s/%016x/%s", keyPrefixAuthByDevice, deviceID, inv, authID))
}

func keyAuthActiveByDevice(deviceID string) []byte {
	return []byte(keyPrefixAuthActiveByDev + deviceID)
}

func prefixUpperBound(prefix []byte) []byte {
	end := make([]byte, len(prefix))
	copy(end, prefix)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end
		}
	}
	return nil
}

func entryKeyBeforeCursor(createdAt int64, entryID string, cursorCreated int64, cursorID string) bool {
	if createdAt < cursorCreated {
		return true
	}
	if createdAt > cursorCreated {
		return false
	}
	return entryID < cursorID
}

/*
   =========================================
   Balance operations
   =========================================
*/

// EnsureBalanceRow ensures a balance row exists for a device.
func (r *ledgerRepoPebble) EnsureBalanceRow(ctx context.Context, tx LedgerTx, deviceID string) error {
	_ = ctx
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return err
	}
	k := keyBalance(deviceID)
	_, closer, err := r.getRaw(pt, k)
	if err == nil {
		closer.Close()
		return nil
	}
	if !errors.Is(err, pebble.ErrNotFound) {
		return err
	}
	b := storedBalance{BalanceMsat: 0, UpdatedAt: now()}
	return r.setRaw(pt, k, b.marshalBinary())
}

// GetBalance retrieves the balance for a device.
func (r *ledgerRepoPebble) GetBalance(ctx context.Context, tx LedgerTx, deviceID string) (int64, error) {
	_ = ctx
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return 0, err
	}
	k := keyBalance(deviceID)
	val, closer, err := r.getRaw(pt, k)
	if errors.Is(err, pebble.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer closer.Close()
	var b storedBalance
	if err := b.unmarshalBinary(val); err != nil {
		return 0, err
	}
	return b.BalanceMsat, nil
}

// UpdateBalance adds or subtracts from a device's balance.
func (r *ledgerRepoPebble) UpdateBalance(ctx context.Context, tx LedgerTx, deviceID string, amountMsat int64) error {
	_ = ctx
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return err
	}
	k := keyBalance(deviceID)
	val, closer, err := r.getRaw(pt, k)
	if errors.Is(err, pebble.ErrNotFound) {
		b := storedBalance{BalanceMsat: amountMsat, UpdatedAt: now()}
		return r.setRaw(pt, k, b.marshalBinary())
	}
	if err != nil {
		return err
	}
	defer closer.Close()
	var b storedBalance
	if err := b.unmarshalBinary(val); err != nil {
		return err
	}
	b.BalanceMsat += amountMsat
	b.UpdatedAt = now()
	return r.setRaw(pt, k, b.marshalBinary())
}

/*
   =========================================
   Ledger entry operations
   =========================================
*/

// CreateLedgerEntry creates a new ledger entry.
func (r *ledgerRepoPebble) CreateLedgerEntry(ctx context.Context, tx LedgerTx, entry EntryResponse) error {
	_ = ctx
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return err
	}
	st := storedLedgerEntry{
		EntryID: entry.EntryID, DeviceID: entry.DeviceID, EntryType: entry.EntryType,
		AmountMsat: entry.AmountMsat, BalanceAfter: entry.BalanceAfter, Reason: entry.Reason,
		CorrelationID: entry.CorrelationID, CreatedAt: entry.CreatedAt,
	}
	if err := r.setRaw(pt, keyLedgerEntry(entry.EntryID), st.marshalBinary()); err != nil {
		return err
	}
	idx := keyLedgerByDevice(entry.DeviceID, entry.CreatedAt, entry.EntryID)
	return r.setRaw(pt, idx, []byte{1})
}

// ListLedgerEntries retrieves ledger entries for a device with pagination (newest first).
func (r *ledgerRepoPebble) ListLedgerEntries(ctx context.Context, deviceID string, cursorCreated int64, cursorID string, limit int) ([]EntryResponse, error) {
	_ = ctx
	prefix := []byte(fmt.Sprintf("%s%s/", keyPrefixLedgerByDevice, deviceID))
	var resp []EntryResponse
	iter, err := r.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		if len(resp) >= limit {
			break
		}
		keyStr := string(iter.Key())
		parts := strings.Split(keyStr, "/")
		if len(parts) < 5 {
			continue
		}
		entryID := parts[len(parts)-1]
		invHex := parts[len(parts)-2]
		var inv uint64
		if _, scanErr := fmt.Sscanf(invHex, "%x", &inv); scanErr != nil {
			continue
		}
		createdAt := int64(^uint64(0) - inv)
		if !entryKeyBeforeCursor(createdAt, entryID, cursorCreated, cursorID) {
			continue
		}
		val, closer, err := r.db.Get(keyLedgerEntry(entryID))
		if errors.Is(err, pebble.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var st storedLedgerEntry
		if err := st.unmarshalBinary(val); err != nil {
			closer.Close()
			continue
		}
		closer.Close()
		resp = append(resp, EntryResponse{
			EntryID: st.EntryID, DeviceID: st.DeviceID, EntryType: st.EntryType,
			AmountMsat: st.AmountMsat, BalanceAfter: st.BalanceAfter, Reason: st.Reason,
			CreatedAt: st.CreatedAt, CorrelationID: st.CorrelationID,
		})
	}
	return resp, nil
}

/*
   =========================================
   Credit/Debit operations
   =========================================
*/

// ApplyCredit applies a credit to a device's balance and creates a ledger entry.
func (r *ledgerRepoPebble) ApplyCredit(ctx context.Context, tx LedgerTx, in CreditRequest) (EntryResponse, error) {
	if in.AmountMsat <= 0 {
		return EntryResponse{}, errors.New("amount must be > 0")
	}
	if err := r.EnsureBalanceRow(ctx, tx, in.DeviceID); err != nil {
		return EntryResponse{}, err
	}
	if err := r.UpdateBalance(ctx, tx, in.DeviceID, in.AmountMsat); err != nil {
		return EntryResponse{}, err
	}
	bal, err := r.GetBalance(ctx, tx, in.DeviceID)
	if err != nil {
		return EntryResponse{}, err
	}
	entry := EntryResponse{
		EntryID:       uuid.NewString(),
		DeviceID:      in.DeviceID,
		EntryType:     "credit",
		AmountMsat:    in.AmountMsat,
		BalanceAfter:  bal,
		Reason:        in.Reason,
		CreatedAt:     now(),
		CorrelationID: in.CorrelationID,
	}
	if err := r.CreateLedgerEntry(ctx, tx, entry); err != nil {
		return EntryResponse{}, err
	}
	return entry, nil
}

// ApplyDebit applies a debit to a device's balance and creates a ledger entry.
func (r *ledgerRepoPebble) ApplyDebit(ctx context.Context, tx LedgerTx, in DebitRequest) (EntryResponse, error) {
	if in.AmountMsat <= 0 {
		return EntryResponse{}, errors.New("amount must be > 0")
	}
	if err := r.EnsureBalanceRow(ctx, tx, in.DeviceID); err != nil {
		return EntryResponse{}, err
	}
	if !in.AllowNegative {
		bal, err := r.GetBalance(ctx, tx, in.DeviceID)
		if err != nil {
			return EntryResponse{}, err
		}
		if bal < in.AmountMsat {
			return EntryResponse{}, fmt.Errorf("insufficient funds: have %d need %d", bal, in.AmountMsat)
		}
	}
	if err := r.UpdateBalance(ctx, tx, in.DeviceID, -in.AmountMsat); err != nil {
		return EntryResponse{}, err
	}
	bal, err := r.GetBalance(ctx, tx, in.DeviceID)
	if err != nil {
		return EntryResponse{}, err
	}
	entry := EntryResponse{
		EntryID:       uuid.NewString(),
		DeviceID:      in.DeviceID,
		EntryType:     "debit",
		AmountMsat:    in.AmountMsat,
		BalanceAfter:  bal,
		Reason:        in.Reason,
		CreatedAt:     now(),
		CorrelationID: in.CorrelationID,
	}
	if err := r.CreateLedgerEntry(ctx, tx, entry); err != nil {
		return EntryResponse{}, err
	}
	return entry, nil
}

/*
   =========================================
   Idempotency operations
   =========================================
*/

// GetCachedIdem retrieves a cached idempotency response.
func (r *ledgerRepoPebble) GetCachedIdem(ctx context.Context, key string) (kind string, resp []byte, ok bool, err error) {
	_ = ctx
	val, closer, err := r.db.Get(keyIdem(key))
	if errors.Is(err, pebble.ErrNotFound) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	defer closer.Close()
	var st storedIdem
	if err := st.unmarshalBinary(val); err != nil {
		return "", nil, false, err
	}
	return st.Kind, []byte(st.ResponseJSON), true, nil
}

// SaveIdem saves an idempotency response.
func (r *ledgerRepoPebble) SaveIdem(ctx context.Context, tx LedgerTx, key, kind, reqHash string, response any) error {
	_ = ctx
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return err
	}
	// ResponseJSON stays as JSON: callers fetch this opaque blob via GetCachedIdem and pass it
	// straight back as an HTTP response body (already JSON-shaped).
	js, _ := json.Marshal(response)
	st := storedIdem{
		Kind: kind, RequestHash: reqHash, ResponseJSON: string(js), CreatedAt: now(),
	}
	return r.setRaw(pt, keyIdem(key), st.marshalBinary())
}

/*
   =========================================
   Authorization operations
   =========================================
*/

// loadActiveAuthPointer returns the authID currently marked as the device's active authorization.
// Pointer absence is not an error — returns ok=false.
func (r *ledgerRepoPebble) loadActiveAuthPointer(tx *pebbleLedgerTx, deviceID string) (string, bool, error) {
	val, closer, err := r.getRaw(tx, keyAuthActiveByDevice(deviceID))
	if errors.Is(err, pebble.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer closer.Close()
	return string(val), true, nil
}

// setActiveAuthPointer writes the device → authID active-auth pointer (overwrites any prior value).
func (r *ledgerRepoPebble) setActiveAuthPointer(tx *pebbleLedgerTx, deviceID, authID string) error {
	return r.setRaw(tx, keyAuthActiveByDevice(deviceID), []byte(authID))
}

// clearActiveAuthPointerIfMatches deletes the pointer only when it currently points to expectedAuthID.
// This guards against deleting a pointer that has already been overwritten by a newer authorization.
func (r *ledgerRepoPebble) clearActiveAuthPointerIfMatches(tx *pebbleLedgerTx, deviceID, expectedAuthID string) error {
	cur, ok, err := r.loadActiveAuthPointer(tx, deviceID)
	if err != nil {
		return err
	}
	if !ok || cur != expectedAuthID {
		return nil
	}
	if tx == nil || tx.batch == nil {
		return errors.New("ledger: clear pointer requires a write transaction")
	}
	return tx.batch.Delete(keyAuthActiveByDevice(deviceID), nil)
}

func (r *ledgerRepoPebble) loadAuthorization(ctx context.Context, tx *pebbleLedgerTx, authorizationID string) (*storedAuthorization, error) {
	_ = ctx
	k := keyAuthRecord(authorizationID)
	val, closer, err := r.getRaw(tx, k)
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	var st storedAuthorization
	if err := st.unmarshalBinary(val); err != nil {
		return nil, err
	}
	return &st, nil
}

func (r *ledgerRepoPebble) putAuthorization(tx *pebbleLedgerTx, st *storedAuthorization) error {
	return r.setRaw(tx, keyAuthRecord(st.AuthorizationID), st.marshalBinary())
}

// CreateAuthorization creates a new authorization.
func (r *ledgerRepoPebble) CreateAuthorization(ctx context.Context, tx LedgerTx, authID, deviceID, requestID string, grantedMsat int64, issuedAt, expiresAt string) error {
	_ = ctx
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return err
	}
	createdAt := time.Now().Unix()
	st := &storedAuthorization{
		AuthorizationID: authID,
		DeviceID:        deviceID,
		RequestID:       requestID,
		GrantedMsat:     grantedMsat,
		RemainingMsat:   grantedMsat,
		ConsumedMsat:    0,
		OverflowMsat:    0,
		IssuedAt:        issuedAt,
		ExpiresAt:       expiresAt,
		Status:          "active",
		CreatedAt:       createdAt,
	}
	if err := r.putAuthorization(pt, st); err != nil {
		return err
	}
	if err := r.setRaw(pt, keyAuthByRequest(requestID), []byte(authID)); err != nil {
		return err
	}
	idx := keyAuthByDevice(deviceID, createdAt, authID)
	if err := r.setRaw(pt, idx, []byte{1}); err != nil {
		return err
	}
	return r.setActiveAuthPointer(pt, deviceID, authID)
}

// GetAuthorizationByRequestID retrieves an authorization by request_id (latest).
func (r *ledgerRepoPebble) GetAuthorizationByRequestID(ctx context.Context, tx LedgerTx, requestID string) (*ledgermodel.Authorization, string, error) {
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return nil, "", err
	}
	val, closer, err := r.getRaw(pt, keyAuthByRequest(requestID))
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	defer closer.Close()
	authID := string(val)
	st, err := r.loadAuthorization(ctx, pt, authID)
	if err != nil {
		return nil, "", err
	}
	auth := &ledgermodel.Authorization{
		DeviceId: st.DeviceID, AuthorizationId: st.AuthorizationID,
		GrantedMsat: st.GrantedMsat, RemainingMsat: st.RemainingMsat,
		IssuedAt: st.IssuedAt, ExpiresAt: st.ExpiresAt,
	}
	return auth, st.Status, nil
}

// GetActiveAuthorization retrieves the most recent active authorization for a device.
func (r *ledgerRepoPebble) GetActiveAuthorization(ctx context.Context, tx LedgerTx, deviceID string, expiresAfter string) (string, int64, int64, int64, string, string, error) {
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return "", 0, 0, 0, "", "", err
	}
	st, err := r.findActiveAuthorization(ctx, pt, deviceID, expiresAfter)
	if err != nil {
		return "", 0, 0, 0, "", "", err
	}
	return st.AuthorizationID, st.RemainingMsat, st.GrantedMsat, st.OverflowMsat, st.ExpiresAt, st.Status, nil
}

func (r *ledgerRepoPebble) findActiveAuthorization(ctx context.Context, tx *pebbleLedgerTx, deviceID string, expiresAfter string) (*storedAuthorization, error) {
	_ = ctx
	// Fast path: follow the active-auth pointer (O(1) Get + decode).
	if authID, ok, err := r.loadActiveAuthPointer(tx, deviceID); err == nil && ok {
		if st, lerr := r.loadAuthorization(ctx, tx, authID); lerr == nil &&
			st.Status == "active" && st.ExpiresAt > expiresAfter {
			return st, nil
		}
		// Pointer present but stale (auth completed/expired since last write); fall through to scan + repair.
	}

	// Slow path: linear scan over auth/by_device — used when no pointer exists or the cached one is stale.
	prefix := []byte(fmt.Sprintf("%s%s/", keyPrefixAuthByDevice, deviceID))
	iter, err := r.iterForTx(tx, prefix)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		keyStr := string(iter.Key())
		parts := strings.Split(keyStr, "/")
		if len(parts) < 5 {
			continue
		}
		authID := parts[len(parts)-1]
		st, err := r.loadAuthorization(ctx, tx, authID)
		if err != nil {
			continue
		}
		if st.Status == "active" && st.ExpiresAt > expiresAfter {
			// Self-heal: repair the pointer when we have a write batch to amortise future lookups.
			if tx != nil && tx.batch != nil {
				_ = r.setActiveAuthPointer(tx, deviceID, st.AuthorizationID)
			}
			return st, nil
		}
	}
	return nil, ErrNotFound
}

func (r *ledgerRepoPebble) iterForTx(tx *pebbleLedgerTx, prefix []byte) (*pebble.Iterator, error) {
	if tx != nil && tx.batch != nil {
		return tx.batch.NewIter(&pebble.IterOptions{
			LowerBound: prefix,
			UpperBound: prefixUpperBound(prefix),
		})
	}
	return r.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
}

// GetActiveAuthorizationForDevice retrieves the most recent active authorization for a device.
func (r *ledgerRepoPebble) GetActiveAuthorizationForDevice(ctx context.Context, tx LedgerTx, deviceID string) (*ledgermodel.Authorization, string, error) {
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return nil, "", err
	}
	nowStr := time.Now().Format(time.RFC3339)
	st, err := r.findActiveAuthorization(ctx, pt, deviceID, nowStr)
	if err != nil {
		return nil, "", err
	}
	auth := &ledgermodel.Authorization{
		DeviceId: st.DeviceID, AuthorizationId: st.AuthorizationID,
		GrantedMsat: st.GrantedMsat, RemainingMsat: st.RemainingMsat,
		IssuedAt: st.IssuedAt, ExpiresAt: st.ExpiresAt,
	}
	return auth, st.Status, nil
}

// UpdateAuthorization updates an authorization's amounts and status.
func (r *ledgerRepoPebble) UpdateAuthorization(ctx context.Context, tx LedgerTx, authorizationID string, remainingMsat int64, consumedMsat int64, overflowMsat int64, status string) error {
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return err
	}
	st, err := r.loadAuthorization(ctx, pt, authorizationID)
	if err != nil {
		return err
	}
	st.RemainingMsat = remainingMsat
	st.ConsumedMsat = consumedMsat
	st.OverflowMsat = overflowMsat
	st.Status = status
	_ = ctx
	return r.putAuthorization(pt, st)
}

// ConsumeAuthorization atomically consumes from an authorization.
func (r *ledgerRepoPebble) ConsumeAuthorization(ctx context.Context, tx LedgerTx, authorizationID string, debitAmount int64) (newRemaining int64, newConsumed int64, newOverflow int64, newStatus string, err error) {
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return 0, 0, 0, "", err
	}
	st, err := r.loadAuthorization(ctx, pt, authorizationID)
	if err != nil {
		return 0, 0, 0, "", err
	}
	currentRemaining := st.RemainingMsat
	currentConsumed := st.ConsumedMsat
	currentOverflow := st.OverflowMsat
	grantedMsat := st.GrantedMsat

	actualDebit := debitAmount
	if currentRemaining < debitAmount {
		actualDebit = currentRemaining
	}

	newRemaining = currentRemaining - actualDebit
	if newRemaining < 0 {
		newRemaining = 0
	}

	newConsumed = currentConsumed + actualDebit
	if newConsumed > grantedMsat {
		newConsumed = grantedMsat
	}

	overflowDelta := debitAmount - actualDebit
	newOverflow = currentOverflow + overflowDelta

	newStatus = "active"
	if newRemaining <= 0 {
		newStatus = "completed"
	}

	st.RemainingMsat = newRemaining
	st.ConsumedMsat = newConsumed
	st.OverflowMsat = newOverflow
	st.Status = newStatus

	_ = ctx
	if err := r.putAuthorization(pt, st); err != nil {
		return 0, 0, 0, "", err
	}
	if newStatus != "active" {
		if err := r.clearActiveAuthPointerIfMatches(pt, st.DeviceID, authorizationID); err != nil {
			return 0, 0, 0, "", err
		}
	}
	return newRemaining, newConsumed, newOverflow, newStatus, nil
}

// GetExpiredAuthorizations retrieves expired active authorizations (expires_at < expiresBefore).
func (r *ledgerRepoPebble) GetExpiredAuthorizations(ctx context.Context, expiresBefore string) ([]ExpiredAuthorization, error) {
	_ = ctx
	prefix := []byte(keyPrefixAuthRecord)
	var expired []ExpiredAuthorization
	iter, err := r.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		val := iter.Value()
		var st storedAuthorization
		if err := st.unmarshalBinary(val); err != nil {
			continue
		}
		if st.Status == "active" && st.ExpiresAt < expiresBefore {
			expired = append(expired, ExpiredAuthorization{
				AuthorizationID: st.AuthorizationID,
				DeviceID:        st.DeviceID,
				ExpiresAt:       st.ExpiresAt,
			})
		}
	}
	return expired, nil
}

// GetActiveAuthorizationByID retrieves an active authorization's device ID and remaining amount.
func (r *ledgerRepoPebble) GetActiveAuthorizationByID(ctx context.Context, tx LedgerTx, authorizationID string) (deviceID string, remainingMsat int64, err error) {
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return "", 0, err
	}
	st, err := r.loadAuthorization(ctx, pt, authorizationID)
	if err != nil {
		return "", 0, err
	}
	if st.Status != "active" {
		return "", 0, ErrNotFound
	}
	return st.DeviceID, st.RemainingMsat, nil
}

// MarkAuthorizationExpired marks an authorization as expired.
func (r *ledgerRepoPebble) MarkAuthorizationExpired(ctx context.Context, tx LedgerTx, authorizationID string) error {
	pt, err := expectPebbleTx(tx)
	if err != nil {
		return err
	}
	st, err := r.loadAuthorization(ctx, pt, authorizationID)
	if err != nil {
		return err
	}
	st.Status = "expired"
	st.RemainingMsat = 0
	_ = ctx
	if err := r.putAuthorization(pt, st); err != nil {
		return err
	}
	return r.clearActiveAuthPointerIfMatches(pt, st.DeviceID, authorizationID)
}

// ListAuthorizations retrieves authorizations for a device with optional status filter.
func (r *ledgerRepoPebble) ListAuthorizations(ctx context.Context, deviceID string, statusFilter string) ([]AuthorizationResponse, error) {
	_ = ctx
	prefix := []byte(fmt.Sprintf("%s%s/", keyPrefixAuthByDevice, deviceID))
	var resp []AuthorizationResponse
	iter, err := r.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		keyStr := string(iter.Key())
		parts := strings.Split(keyStr, "/")
		if len(parts) < 5 {
			continue
		}
		authID := parts[len(parts)-1]
		val, closer, err := r.db.Get(keyAuthRecord(authID))
		if err != nil {
			continue
		}
		var st storedAuthorization
		if err := st.unmarshalBinary(val); err != nil {
			closer.Close()
			continue
		}
		closer.Close()
		switch statusFilter {
		case "active":
			if st.Status != "active" {
				continue
			}
		case "non-active":
			if st.Status != "completed" && st.Status != "expired" {
				continue
			}
		}
		resp = append(resp, AuthorizationResponse{
			AuthorizationID: st.AuthorizationID,
			DeviceID:        st.DeviceID,
			RequestID:       st.RequestID,
			GrantedMsat:     st.GrantedMsat,
			RemainingMsat:   st.RemainingMsat,
			ConsumedMsat:    st.ConsumedMsat,
			OverflowMsat:    st.OverflowMsat,
			IssuedAt:        st.IssuedAt,
			ExpiresAt:       st.ExpiresAt,
			Status:          st.Status,
			CreatedAt:       st.CreatedAt,
		})
	}
	return resp, nil
}
