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

// ErrNotFound is returned when a looked-up row or key does not exist (replaces sql.ErrNoRows).
var ErrNotFound = errors.New("ledger: not found")

// Key prefixes — device_id, authorization_id, entry_id, request_id must not contain '/'.
const (
	keyPrefixBalance          = "balance/"
	keyPrefixLedgerEntry      = "ledger/entry/"
	keyPrefixLedgerByDevice   = "ledger/by_device/"
	keyPrefixIdem             = "idem/"
	keyPrefixAuthRecord       = "auth/record/"
	keyPrefixAuthByRequest    = "auth/by_request/"
	keyPrefixAuthByDevice     = "auth/by_device/"
)

// LedgerRepository manages Pebble storage for the ledger.
type LedgerRepository struct {
	db *pebble.DB
}

// LedgerTxOptions controls BeginTx behavior (read-only vs read-write batch).
type LedgerTxOptions struct {
	ReadOnly bool
}

// LedgerTx is a read-write Pebble batch or a read-only view over the DB.
type LedgerTx struct {
	db       *pebble.DB
	batch    *pebble.Batch
	readOnly bool
}

// Commit applies the batch (sync). No-op for read-only transactions.
func (t *LedgerTx) Commit() error {
	if t.readOnly || t.batch == nil {
		return nil
	}
	err := t.batch.Commit(&pebble.WriteOptions{Sync: true})
	_ = t.batch.Close()
	t.batch = nil
	return err
}

// Rollback discards the batch.
func (t *LedgerTx) Rollback() error {
	if t.batch != nil {
		_ = t.batch.Close()
		t.batch = nil
	}
	return nil
}

// NewLedgerRepository opens a Pebble store at storePath (directory).
func NewLedgerRepository(storePath string) (*LedgerRepository, error) {
	db, err := pebble.Open(storePath, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("open pebble store: %w", err)
	}
	return &LedgerRepository{db: db}, nil
}

func (r *LedgerRepository) getRaw(tx *LedgerTx, key []byte) ([]byte, io.Closer, error) {
	if tx != nil && tx.batch != nil {
		return tx.batch.Get(key)
	}
	return r.db.Get(key)
}

func (r *LedgerRepository) setRaw(tx *LedgerTx, key, val []byte) error {
	if tx == nil || tx.batch == nil {
		return errors.New("ledger: write requires a write transaction")
	}
	return tx.batch.Set(key, val, nil)
}

// BeginTx starts a read-only session or a write batch.
func (r *LedgerRepository) BeginTx(ctx context.Context, opts *LedgerTxOptions) (*LedgerTx, error) {
	_ = ctx
	if opts != nil && opts.ReadOnly {
		return &LedgerTx{db: r.db, readOnly: true}, nil
	}
	return &LedgerTx{db: r.db, batch: r.db.NewIndexedBatch()}, nil
}

// Close closes the Pebble store.
func (r *LedgerRepository) Close() error {
	return r.db.Close()
}

func now() int64 { return time.Now().Unix() }

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
func (r *LedgerRepository) EnsureBalanceRow(ctx context.Context, tx *LedgerTx, deviceID string) error {
	_ = ctx
	k := keyBalance(deviceID)
	_, closer, err := r.getRaw(tx, k)
	if err == nil {
		closer.Close()
		return nil
	}
	if !errors.Is(err, pebble.ErrNotFound) {
		return err
	}
	b := storedBalance{BalanceMsat: 0, UpdatedAt: now()}
	payload, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return r.setRaw(tx, k, payload)
}

// GetBalance retrieves the balance for a device.
func (r *LedgerRepository) GetBalance(ctx context.Context, tx *LedgerTx, deviceID string) (int64, error) {
	_ = ctx
	k := keyBalance(deviceID)
	val, closer, err := r.getRaw(tx, k)
	if errors.Is(err, pebble.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer closer.Close()
	var b storedBalance
	if err := json.Unmarshal(val, &b); err != nil {
		return 0, err
	}
	return b.BalanceMsat, nil
}

// UpdateBalance adds or subtracts from a device's balance.
func (r *LedgerRepository) UpdateBalance(ctx context.Context, tx *LedgerTx, deviceID string, amountMsat int64) error {
	_ = ctx
	k := keyBalance(deviceID)
	val, closer, err := r.getRaw(tx, k)
	if errors.Is(err, pebble.ErrNotFound) {
		b := storedBalance{BalanceMsat: amountMsat, UpdatedAt: now()}
		payload, e := json.Marshal(b)
		if e != nil {
			return e
		}
		return r.setRaw(tx, k, payload)
	}
	if err != nil {
		return err
	}
	defer closer.Close()
	var b storedBalance
	if err := json.Unmarshal(val, &b); err != nil {
		return err
	}
	b.BalanceMsat += amountMsat
	b.UpdatedAt = now()
	payload, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return r.setRaw(tx, k, payload)
}

/*
   =========================================
   Ledger entry operations
   =========================================
*/

// CreateLedgerEntry creates a new ledger entry.
func (r *LedgerRepository) CreateLedgerEntry(ctx context.Context, tx *LedgerTx, entry EntryResponse) error {
	_ = ctx
	st := storedLedgerEntry{
		EntryID: entry.EntryID, DeviceID: entry.DeviceID, EntryType: entry.EntryType,
		AmountMsat: entry.AmountMsat, BalanceAfter: entry.BalanceAfter, Reason: entry.Reason,
		CorrelationID: entry.CorrelationID, CreatedAt: entry.CreatedAt,
	}
	payload, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := r.setRaw(tx, keyLedgerEntry(entry.EntryID), payload); err != nil {
		return err
	}
	idx := keyLedgerByDevice(entry.DeviceID, entry.CreatedAt, entry.EntryID)
	return r.setRaw(tx, idx, []byte{1})
}

// ListLedgerEntries retrieves ledger entries for a device with pagination (newest first).
func (r *LedgerRepository) ListLedgerEntries(ctx context.Context, deviceID string, cursorCreated int64, cursorID string, limit int) ([]EntryResponse, error) {
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
		if err := json.Unmarshal(val, &st); err != nil {
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
func (r *LedgerRepository) ApplyCredit(ctx context.Context, tx *LedgerTx, in CreditRequest) (EntryResponse, error) {
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
func (r *LedgerRepository) ApplyDebit(ctx context.Context, tx *LedgerTx, in DebitRequest) (EntryResponse, error) {
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
func (r *LedgerRepository) GetCachedIdem(ctx context.Context, key string) (kind string, resp []byte, ok bool, err error) {
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
	if err := json.Unmarshal(val, &st); err != nil {
		return "", nil, false, err
	}
	return st.Kind, []byte(st.ResponseJSON), true, nil
}

// SaveIdem saves an idempotency response.
func (r *LedgerRepository) SaveIdem(ctx context.Context, tx *LedgerTx, key, kind, reqHash string, response any) error {
	_ = ctx
	js, _ := json.Marshal(response)
	st := storedIdem{
		Kind: kind, RequestHash: reqHash, ResponseJSON: string(js), CreatedAt: now(),
	}
	payload, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return r.setRaw(tx, keyIdem(key), payload)
}

/*
   =========================================
   Authorization operations
   =========================================
*/

func (r *LedgerRepository) loadAuthorization(ctx context.Context, tx *LedgerTx, authorizationID string) (*storedAuthorization, error) {
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
	if err := json.Unmarshal(val, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (r *LedgerRepository) putAuthorization(tx *LedgerTx, st *storedAuthorization) error {
	payload, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return r.setRaw(tx, keyAuthRecord(st.AuthorizationID), payload)
}

// CreateAuthorization creates a new authorization.
func (r *LedgerRepository) CreateAuthorization(ctx context.Context, tx *LedgerTx, authID, deviceID, requestID string, grantedMsat int64, issuedAt, expiresAt string) error {
	_ = ctx
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
	if err := r.putAuthorization(tx, st); err != nil {
		return err
	}
	if err := r.setRaw(tx, keyAuthByRequest(requestID), []byte(authID)); err != nil {
		return err
	}
	idx := keyAuthByDevice(deviceID, createdAt, authID)
	return r.setRaw(tx, idx, []byte{1})
}

// GetAuthorizationByRequestID retrieves an authorization by request_id (latest).
func (r *LedgerRepository) GetAuthorizationByRequestID(ctx context.Context, tx *LedgerTx, requestID string) (*ledgermodel.Authorization, string, error) {
	val, closer, err := r.getRaw(tx, keyAuthByRequest(requestID))
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	defer closer.Close()
	authID := string(val)
	st, err := r.loadAuthorization(ctx, tx, authID)
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
func (r *LedgerRepository) GetActiveAuthorization(ctx context.Context, tx *LedgerTx, deviceID string, expiresAfter string) (string, int64, int64, int64, string, string, error) {
	st, err := r.findActiveAuthorization(ctx, tx, deviceID, expiresAfter)
	if err != nil {
		return "", 0, 0, 0, "", "", err
	}
	return st.AuthorizationID, st.RemainingMsat, st.GrantedMsat, st.OverflowMsat, st.ExpiresAt, st.Status, nil
}

func (r *LedgerRepository) findActiveAuthorization(ctx context.Context, tx *LedgerTx, deviceID string, expiresAfter string) (*storedAuthorization, error) {
	_ = ctx
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
			return st, nil
		}
	}
	return nil, ErrNotFound
}

func (r *LedgerRepository) iterForTx(tx *LedgerTx, prefix []byte) (*pebble.Iterator, error) {
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
func (r *LedgerRepository) GetActiveAuthorizationForDevice(ctx context.Context, tx *LedgerTx, deviceID string) (*ledgermodel.Authorization, string, error) {
	nowStr := time.Now().Format(time.RFC3339)
	st, err := r.findActiveAuthorization(ctx, tx, deviceID, nowStr)
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
func (r *LedgerRepository) UpdateAuthorization(ctx context.Context, tx *LedgerTx, authorizationID string, remainingMsat int64, consumedMsat int64, overflowMsat int64, status string) error {
	st, err := r.loadAuthorization(ctx, tx, authorizationID)
	if err != nil {
		return err
	}
	st.RemainingMsat = remainingMsat
	st.ConsumedMsat = consumedMsat
	st.OverflowMsat = overflowMsat
	st.Status = status
	_ = ctx
	return r.putAuthorization(tx, st)
}

// ConsumeAuthorization atomically consumes from an authorization.
func (r *LedgerRepository) ConsumeAuthorization(ctx context.Context, tx *LedgerTx, authorizationID string, debitAmount int64) (newRemaining int64, newConsumed int64, newOverflow int64, newStatus string, err error) {
	st, err := r.loadAuthorization(ctx, tx, authorizationID)
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
	if err := r.putAuthorization(tx, st); err != nil {
		return 0, 0, 0, "", err
	}
	return newRemaining, newConsumed, newOverflow, newStatus, nil
}

// GetExpiredAuthorizations retrieves expired active authorizations (expires_at < expiresBefore).
func (r *LedgerRepository) GetExpiredAuthorizations(ctx context.Context, expiresBefore string) ([]ExpiredAuthorization, error) {
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
		if err := json.Unmarshal(val, &st); err != nil {
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
func (r *LedgerRepository) GetActiveAuthorizationByID(ctx context.Context, tx *LedgerTx, authorizationID string) (deviceID string, remainingMsat int64, err error) {
	st, err := r.loadAuthorization(ctx, tx, authorizationID)
	if err != nil {
		return "", 0, err
	}
	if st.Status != "active" {
		return "", 0, ErrNotFound
	}
	return st.DeviceID, st.RemainingMsat, nil
}

// MarkAuthorizationExpired marks an authorization as expired.
func (r *LedgerRepository) MarkAuthorizationExpired(ctx context.Context, tx *LedgerTx, authorizationID string) error {
	st, err := r.loadAuthorization(ctx, tx, authorizationID)
	if err != nil {
		return err
	}
	st.Status = "expired"
	st.RemainingMsat = 0
	_ = ctx
	return r.putAuthorization(tx, st)
}

// ListAuthorizations retrieves authorizations for a device with optional status filter.
func (r *LedgerRepository) ListAuthorizations(ctx context.Context, deviceID string, statusFilter string) ([]AuthorizationResponse, error) {
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
		if err := json.Unmarshal(val, &st); err != nil {
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

// ExpiredAuthorization represents an expired authorization.
type ExpiredAuthorization struct {
	AuthorizationID string
	DeviceID        string
	ExpiresAt       string
}
