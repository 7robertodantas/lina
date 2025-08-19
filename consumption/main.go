package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

/*
   ===========================
   Configuration & Constants
   ===========================
*/

type Config struct {
	DBPath             string
	RegistryBaseURL    string
	ServiceToken       string
	RegistryCacheTTL   time.Duration
	LedgerURL          string
	WorkerBatchSize    int
	WorkerPollInterval time.Duration
	DispatcherEvery    time.Duration
	MaxAttempts        int
}

func loadConfig() Config {
	return Config{
		DBPath:             getenv("DB_PATH", "consumption.db"),
		RegistryBaseURL:    getenv("REGISTRY_URL", "http://localhost:8080"),
		ServiceToken:       getenv("SERVICE_TOKEN", "dev-token"),
		RegistryCacheTTL:   durationEnv("REGISTRY_CACHE_TTL", 2*time.Minute),
		LedgerURL:          getenv("LEDGER_URL", "http://localhost:8080"),
		WorkerBatchSize:    intEnv("WORKER_BATCH_SIZE", 50),
		WorkerPollInterval: durationEnv("WORKER_POLL_INTERVAL", 500*time.Millisecond),
		DispatcherEvery:    durationEnv("DISPATCHER_EVERY", 2*time.Second),
		MaxAttempts:        intEnv("MAX_ATTEMPTS", 10),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
func intEnv(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}
func durationEnv(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

/*
   ===========================
   Database (schema + light queue)
   ===========================
*/

func initDB(path string) *sql.DB {
	// WAL + busy_timeout keeps latency low on edge devices
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout=5000&_pragma=journal_mode(WAL)")
	if err != nil {
		log.Fatalf("db open: %v", err)
	}

	// Base schema (new installs)
	stmts := []string{
		// raw events (idempotent by device_id+seq)
		`CREATE TABLE IF NOT EXISTS raw_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			ts INTEGER NOT NULL,
			quantity REAL,              -- nullable if reporting_mode=counter
			counter REAL,               -- nullable if reporting_mode=delta
			signature TEXT,
			created_at INTEGER NOT NULL,
			UNIQUE(device_id, seq)
		);`,
		// queue as a table
		`CREATE TABLE IF NOT EXISTS queue_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			raw_event_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending', -- pending|processing|done|error
			attempts INTEGER NOT NULL DEFAULT 0,
			worker_id TEXT,
			claimed_at INTEGER,
			last_error TEXT,
			created_at INTEGER NOT NULL,
			FOREIGN KEY(raw_event_id) REFERENCES raw_events(id)
		);`,
		// device state (hot state persisted)
		`CREATE TABLE IF NOT EXISTS device_state (
			device_id TEXT PRIMARY KEY,
			partial_remainder REAL NOT NULL DEFAULT 0,
			current_bucket INTEGER NOT NULL DEFAULT 0,
			sum_in_bucket REAL NOT NULL DEFAULT 0,
			sum_for_threshold REAL NOT NULL DEFAULT 0,
			last_counter REAL,
			last_ts INTEGER DEFAULT 0,
			updated_at INTEGER NOT NULL
		);`,
		// batches ready for ledger
		`CREATE TABLE IF NOT EXISTS consumption_batches (
			id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL,
			window_start INTEGER,
			window_end INTEGER,
			units REAL NOT NULL,
			unit TEXT NOT NULL,
			unit_price_sats REAL NOT NULL,
			total_sats REAL NOT NULL,
			status TEXT NOT NULL, -- pending|posted|settled|failed
			created_at INTEGER NOT NULL
		);`,
		// outbox pattern
		`CREATE TABLE IF NOT EXISTS outbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,   -- BatchReady
			ref_id TEXT NOT NULL, -- batch_id
			payload TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			sent_at INTEGER
		);`,
		`CREATE INDEX IF NOT EXISTS idx_raw_device_ts ON raw_events(device_id, ts);`,
		`CREATE INDEX IF NOT EXISTS idx_queue_status ON queue_events(status, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_outbox_unsent ON outbox(kind, sent_at);`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			log.Fatalf("db schema: %v", err)
		}
	}

	// Migrate legacy DBs: add columns if missing (no-op if already present)
	migrateAddColumnIfMissing(db, "raw_events", "counter", "REAL")
	migrateAddColumnIfMissing(db, "device_state", "last_counter", "REAL")
	migrateAddColumnIfMissing(db, "device_state", "last_ts", "INTEGER DEFAULT 0")

	return db
}

func migrateAddColumnIfMissing(db *sql.DB, table, col, typ string) {
	var found bool
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s);", table))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			_ = rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk)
			if strings.EqualFold(name, col) {
				found = true
				break
			}
		}
	}
	if !found {
		_, _ = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", table, col, typ))
	}
}

/*
   ===========================
   Registry Client (with cache)
   ===========================
*/

type DeviceConfig struct {
	DeviceID        string  `json:"id"`
	Unit            string  `json:"unit"`
	PricePerUnit    float64 `json:"price_per_unit"`
	PublicKey       string  `json:"public_key"`
	AggregationMode string  `json:"aggregation_mode"`

	// Internal-only fields (from secure endpoint)
	SecretKey      string  `json:"secret_key,omitempty"`
	ReportingMode  string  `json:"reporting_mode,omitempty"`  // "delta" (default) or "counter"
	WindowSeconds  int     `json:"window_seconds,omitempty"`  // time-window
	ValueThreshold float64 `json:"value_threshold,omitempty"` // value-threshold
	MeterMax       float64 `json:"meter_max,omitempty"`       // for counter overflow
	MaxDeltaAbs    float64 `json:"max_delta_abs,omitempty"`   // anti-spike
	BillFromFirst  bool    `json:"bill_from_first,omitempty"` // first counter reading bills?
}

type registryClient struct {
	baseURL string
	token   string
	ttl     time.Duration

	cache sync.Map // deviceID -> cachedCfg
}
type cachedCfg struct {
	cfg     DeviceConfig
	expires time.Time
}

func newRegistryClient(base, token string, ttl time.Duration) *registryClient {
	return &registryClient{baseURL: base, token: token, ttl: ttl}
}

func (rc *registryClient) GetConfig(ctx context.Context, deviceID string) (DeviceConfig, error) {
	// Read-through cache
	if v, ok := rc.cache.Load(deviceID); ok {
		c := v.(cachedCfg)
		if time.Now().Before(c.expires) {
			return c.cfg, nil
		}
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", rc.baseURL+"/internal/devices/config?deviceId="+deviceID, nil)
	req.Header.Set("X-Service-Token", rc.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return DeviceConfig{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return DeviceConfig{}, fmt.Errorf("device not found in registry")
	}
	if resp.StatusCode != 200 {
		return DeviceConfig{}, fmt.Errorf("registry error: %s", resp.Status)
	}

	var out DeviceConfig
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return DeviceConfig{}, err
	}
	if out.AggregationMode == "" {
		out.AggregationMode = "per-unit"
	}
	if out.ReportingMode == "" {
		out.ReportingMode = "delta"
	}

	rc.cache.Store(deviceID, cachedCfg{cfg: out, expires: time.Now().Add(rc.ttl)})
	return out, nil
}

/*
   ===========================
   Aggregation (hot state per device)
   ===========================
*/

type deviceState struct {
	PartialRemainder float64
	CurrentBucket    int64
	SumInBucket      float64
	SumForThreshold  float64
	LastCounter      sql.NullFloat64
	LastTS           sql.NullInt64
	UpdatedAt        int64
}

type Aggregator struct {
	db *sql.DB
}

func NewAggregator(db *sql.DB) *Aggregator { return &Aggregator{db: db} }

func (a *Aggregator) loadState(ctx context.Context, deviceID string) (deviceState, error) {
	var st deviceState
	row := a.db.QueryRowContext(ctx, `
		SELECT partial_remainder, current_bucket, sum_in_bucket, sum_for_threshold, last_counter, last_ts, updated_at
		  FROM device_state WHERE device_id = ?`, deviceID)
	switch err := row.Scan(&st.PartialRemainder, &st.CurrentBucket, &st.SumInBucket, &st.SumForThreshold, &st.LastCounter, &st.LastTS, &st.UpdatedAt); err {
	case sql.ErrNoRows:
		return deviceState{UpdatedAt: time.Now().Unix()}, nil
	case nil:
		return st, nil
	default:
		return deviceState{}, err
	}
}

func (a *Aggregator) saveState(ctx context.Context, deviceID string, st deviceState) error {
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO device_state(device_id, partial_remainder, current_bucket, sum_in_bucket, sum_for_threshold, last_counter, last_ts, updated_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(device_id) DO UPDATE SET
			partial_remainder=excluded.partial_remainder,
			current_bucket=excluded.current_bucket,
			sum_in_bucket=excluded.sum_in_bucket,
			sum_for_threshold=excluded.sum_for_threshold,
			last_counter=excluded.last_counter,
			last_ts=excluded.last_ts,
			updated_at=excluded.updated_at
	`, deviceID, st.PartialRemainder, st.CurrentBucket, st.SumInBucket, st.SumForThreshold, st.LastCounter, st.LastTS, time.Now().Unix())
	return err
}

type AggEvent struct {
	DeviceID string
	TS       int64
	Quantity float64 // effective delta quantity to aggregate
}

type Batch struct {
	ID          string
	DeviceID    string
	WindowStart int64
	WindowEnd   int64
	Units       float64
	Unit        string
	UnitPrice   float64
	TotalSats   float64
	CreatedAt   int64
}

func (a *Aggregator) OnEvent(ctx context.Context, cfg DeviceConfig, ev AggEvent) ([]Batch, error) {
	st, err := a.loadState(ctx, ev.DeviceID)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	var out []Batch

	switch cfg.AggregationMode {
	case "per-unit":
		total := st.PartialRemainder + ev.Quantity
		units := math.Floor(total)
		if units >= 1 {
			out = append(out, Batch{
				ID:        uuid.NewString(),
				DeviceID:  ev.DeviceID,
				Units:     units,
				Unit:      cfg.Unit,
				UnitPrice: cfg.PricePerUnit,
				TotalSats: units * cfg.PricePerUnit,
				CreatedAt: now,
			})
			st.PartialRemainder = total - units
		} else {
			st.PartialRemainder = total
		}

	case "time-window":
		win := cfg.WindowSeconds
		if win <= 0 {
			win = 60
		}
		bucket := ev.TS / int64(win)
		if st.CurrentBucket == 0 {
			st.CurrentBucket = bucket
		}
		if bucket != st.CurrentBucket {
			// close previous bucket
			if st.SumInBucket > 0 {
				start := st.CurrentBucket * int64(win)
				end := (st.CurrentBucket+1)*int64(win) - 1
				out = append(out, Batch{
					ID:          uuid.NewString(),
					DeviceID:    ev.DeviceID,
					WindowStart: start,
					WindowEnd:   end,
					Units:       st.SumInBucket,
					Unit:        cfg.Unit,
					UnitPrice:   cfg.PricePerUnit,
					TotalSats:   st.SumInBucket * cfg.PricePerUnit,
					CreatedAt:   now,
				})
			}
			st.CurrentBucket = bucket
			st.SumInBucket = 0
		}
		st.SumInBucket += ev.Quantity

	case "value-threshold":
		thr := cfg.ValueThreshold
		if thr <= 0 {
			thr = 1
		}
		st.SumForThreshold += ev.Quantity
		if st.SumForThreshold >= thr {
			units := st.SumForThreshold
			out = append(out, Batch{
				ID:        uuid.NewString(),
				DeviceID:  ev.DeviceID,
				Units:     units,
				Unit:      cfg.Unit,
				UnitPrice: cfg.PricePerUnit,
				TotalSats: units * cfg.PricePerUnit,
				CreatedAt: now,
			})
			st.SumForThreshold = 0
		}

	default:
		// fallback per-unit
		total := st.PartialRemainder + ev.Quantity
		units := math.Floor(total)
		if units >= 1 {
			out = append(out, Batch{
				ID:        uuid.NewString(),
				DeviceID:  ev.DeviceID,
				Units:     units,
				Unit:      cfg.Unit,
				UnitPrice: cfg.PricePerUnit,
				TotalSats: units * cfg.PricePerUnit,
				CreatedAt: now,
			})
			st.PartialRemainder = total - units
		} else {
			st.PartialRemainder = total
		}
	}

	if err := a.saveState(ctx, ev.DeviceID, st); err != nil {
		return nil, err
	}
	return out, nil
}

/*
   ===========================
   Service (queue claim + processing + dispatch)
   ===========================
*/

type Service struct {
	cfg      Config
	db       *sql.DB
	registry *registryClient
	agg      *Aggregator
	workerID string
}

func NewService(cfg Config, db *sql.DB) *Service {
	return &Service{
		cfg:      cfg,
		db:       db,
		registry: newRegistryClient(cfg.RegistryBaseURL, cfg.ServiceToken, cfg.RegistryCacheTTL),
		agg:      NewAggregator(db),
		workerID: uuid.NewString(),
	}
}

/*** Queue claim ***/
func (s *Service) claimBatch(ctx context.Context, limit int) ([]int64, error) {
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		UPDATE queue_events
		   SET status='processing',
		       worker_id=?,
		       claimed_at=?,
		       attempts=attempts+1
		 WHERE id IN (
		 	   SELECT id FROM queue_events
		 	    WHERE status='pending'
		 	    ORDER BY id
		 	    LIMIT ?
		 )
	`, s.workerID, now, limit)
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(`SELECT id FROM queue_events WHERE status='processing' AND worker_id=? AND claimed_at=?`, s.workerID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

type RawQueueItem struct {
	ID        int64
	DeviceID  string
	Seq       int64
	TS        int64
	Quantity  sql.NullFloat64
	Counter   sql.NullFloat64
	Signature string
	CreatedAt int64
}

func (s *Service) loadQueueItem(ctx context.Context, qid int64) (RawQueueItem, error) {
	var ev RawQueueItem
	row := s.db.QueryRowContext(ctx, `
		SELECT re.id, re.device_id, re.seq, re.ts, re.quantity, re.counter, re.signature, re.created_at
		  FROM queue_events q
		  JOIN raw_events re ON re.id = q.raw_event_id
		 WHERE q.id = ?`, qid)
	switch err := row.Scan(&ev.ID, &ev.DeviceID, &ev.Seq, &ev.TS, &ev.Quantity, &ev.Counter, &ev.Signature, &ev.CreatedAt); err {
	case nil:
		return ev, nil
	default:
		return RawQueueItem{}, err
	}
}

func (s *Service) setQueueDone(ctx context.Context, qid int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE queue_events SET status='done' WHERE id=?`, qid)
	return err
}
func (s *Service) setQueueError(ctx context.Context, qid int64, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE queue_events SET status=CASE WHEN attempts<? THEN 'pending' ELSE 'error' END, last_error=? WHERE id=?`,
		s.cfg.MaxAttempts, truncate(errMsg, 500), qid)
	return err
}

/*** Persistence helpers ***/
func (s *Service) insertBatch(ctx context.Context, b Batch) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO consumption_batches(id, device_id, window_start, window_end, units, unit, unit_price_sats, total_sats, status, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.DeviceID, nullInt(b.WindowStart), nullInt(b.WindowEnd), b.Units, b.Unit, b.UnitPrice, b.TotalSats, "pending", b.CreatedAt)
	return err
}
func (s *Service) enqueueOutbox(ctx context.Context, kind, refID string, payload any) error {
	js, _ := json.Marshal(payload)
	_, err := s.db.ExecContext(ctx, `INSERT INTO outbox(kind, ref_id, payload, created_at) VALUES(?,?,?,?)`,
		kind, refID, string(js), time.Now().Unix())
	return err
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

/*** Worker loop ***/
func (s *Service) workerLoop(ctx context.Context) {
	t := time.NewTicker(s.cfg.WorkerPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ids, err := s.claimBatch(ctx, s.cfg.WorkerBatchSize)
			if err != nil {
				log.Printf("[worker] claim error: %v", err)
				continue
			}
			for _, qid := range ids {
				if err := s.processQueueItem(ctx, qid); err != nil {
					log.Printf("[worker] process error: %v", err)
				}
			}
		}
	}
}

func (s *Service) processQueueItem(ctx context.Context, qid int64) error {
	ev, err := s.loadQueueItem(ctx, qid)
	if err != nil {
		_ = s.setQueueError(ctx, qid, "loadQueueItem: "+err.Error())
		return err
	}

	// Load device config (with secret if available)
	cfg, err := s.registry.GetConfig(ctx, ev.DeviceID)
	if err != nil {
		_ = s.setQueueError(ctx, qid, "registry: "+err.Error())
		return err
	}

	// Optional HMAC verification. Use the field actually sent.
	if cfg.SecretKey != "" {
		ok := verifySignature(cfg.SecretKey, ev.DeviceID, ev.TS, ev.Seq, ev.Quantity, ev.Counter, ev.Signature)
		if !ok {
			_ = s.setQueueError(ctx, qid, "invalid signature")
			return errors.New("invalid signature")
		}
	}

	// Derive effective delta if reporting_mode is counter (or if counter present)
	var effectiveQty float64
	if cfg.ReportingMode == "counter" || ev.Counter.Valid {
		q, derr := s.deriveDeltaFromCounter(ctx, ev.DeviceID, ev.TS, ev.Counter, cfg)
		if derr != nil {
			_ = s.setQueueError(ctx, qid, "deriveDelta: "+derr.Error())
			return derr
		}
		effectiveQty = q
	} else {
		// quantity mode
		if !ev.Quantity.Valid {
			_ = s.setQueueError(ctx, qid, "missing quantity")
			return errors.New("missing quantity")
		}
		effectiveQty = ev.Quantity.Float64
	}

	// No-op event? still mark done to keep idempotence
	if effectiveQty < 0 {
		effectiveQty = 0
	}

	// Aggregate → 0..N batches
	batches, err := s.agg.OnEvent(ctx, cfg, AggEvent{
		DeviceID: ev.DeviceID,
		TS:       ev.TS,
		Quantity: effectiveQty,
	})
	if err != nil {
		_ = s.setQueueError(ctx, qid, "aggregate: "+err.Error())
		return err
	}

	// Persist batches + outbox
	for _, b := range batches {
		if err := s.insertBatch(ctx, b); err != nil {
			_ = s.setQueueError(ctx, qid, "insertBatch: "+err.Error())
			return err
		}
		if err := s.enqueueOutbox(ctx, "BatchReady", b.ID, b); err != nil {
			_ = s.setQueueError(ctx, qid, "enqueueOutbox: "+err.Error())
			return err
		}
	}

	// Done
	if err := s.setQueueDone(ctx, qid); err != nil {
		return err
	}
	return nil
}

/*** Counter → Delta derivation ***/
func (s *Service) deriveDeltaFromCounter(ctx context.Context, deviceID string, ts int64, counter sql.NullFloat64, cfg DeviceConfig) (float64, error) {
	if !counter.Valid {
		// no counter present
		return 0, nil
	}
	// Read current baseline
	var st deviceState
	row := s.db.QueryRowContext(ctx, `
		SELECT partial_remainder, current_bucket, sum_in_bucket, sum_for_threshold, last_counter, last_ts, updated_at
		  FROM device_state WHERE device_id=?`, deviceID)
	switch err := row.Scan(&st.PartialRemainder, &st.CurrentBucket, &st.SumInBucket, &st.SumForThreshold, &st.LastCounter, &st.LastTS, &st.UpdatedAt); err {
	case sql.ErrNoRows:
		// first reading
		delta := 0.0
		if cfg.BillFromFirst {
			delta = counter.Float64
		}
		_, _ = s.db.ExecContext(ctx, `
		  INSERT INTO device_state(device_id, partial_remainder, current_bucket, sum_in_bucket, sum_for_threshold, last_counter, last_ts, updated_at)
		  VALUES(?,?,?,?,?,?,?,?)`,
			deviceID, st.PartialRemainder, st.CurrentBucket, st.SumInBucket, st.SumForThreshold, counter.Float64, ts, time.Now().Unix())
		return delta, nil
	case nil:
		// ok
	default:
		return 0, err
	}

	// Out-of-order: ignore older timestamps
	if st.LastTS.Valid && ts <= st.LastTS.Int64 {
		return 0, nil
	}

	var delta float64
	if !st.LastCounter.Valid {
		delta = 0
		if cfg.BillFromFirst {
			delta = counter.Float64
		}
	} else {
		delta = counter.Float64 - st.LastCounter.Float64
		if delta < 0 {
			// overflow or reset
			if cfg.MeterMax > 0 {
				delta = counter.Float64 + (cfg.MeterMax - st.LastCounter.Float64)
			} else {
				// unknown max: treat as reset (start from new counter)
				delta = counter.Float64
			}
		}
	}

	// Anti-spike: clamp if configured
	if cfg.MaxDeltaAbs > 0 && delta > cfg.MaxDeltaAbs {
		delta = cfg.MaxDeltaAbs
	}
	if delta < 0 {
		delta = 0
	}

	// Update baseline
	_, _ = s.db.ExecContext(ctx, `
	  INSERT INTO device_state(device_id, partial_remainder, current_bucket, sum_in_bucket, sum_for_threshold, last_counter, last_ts, updated_at)
	  VALUES(?,?,?,?,?,?,?,?)
	  ON CONFLICT(device_id) DO UPDATE SET
	    last_counter=excluded.last_counter,
	    last_ts=excluded.last_ts,
	    updated_at=excluded.updated_at
	`, deviceID, st.PartialRemainder, st.CurrentBucket, st.SumInBucket, st.SumForThreshold, counter.Float64, ts, time.Now().Unix())

	return delta, nil
}

/*** Dispatcher (Outbox → Ledger) ***/
func (s *Service) dispatcherLoop(ctx context.Context) {
	t := time.NewTicker(s.cfg.DispatcherEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.dispatchOnce(ctx, 50)
		}
	}
}

func (s *Service) dispatchOnce(ctx context.Context, limit int) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, ref_id, payload FROM outbox WHERE sent_at IS NULL ORDER BY id LIMIT ?`, limit)
	if err != nil {
		log.Printf("[dispatch] query: %v", err)
		return
	}
	defer rows.Close()

	type item struct {
		ID      int64
		Kind    string
		RefID   string
		Payload string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Kind, &it.RefID, &it.Payload); err == nil {
			items = append(items, it)
		}
	}
	for _, it := range items {
		if err := s.sendToLedger(ctx, it.Kind, it.RefID, it.Payload); err != nil {
			log.Printf("[dispatch] sendToLedger: %v", err)
			continue
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE outbox SET sent_at=? WHERE id=?`, time.Now().Unix(), it.ID)
		_, _ = s.db.ExecContext(ctx, `UPDATE consumption_batches SET status='posted' WHERE id=?`, it.RefID)
	}
}

func (s *Service) sendToLedger(ctx context.Context, kind, refID, payload string) error {
	if kind != "BatchReady" {
		log.Printf("[sendToLedger] unknown kind: %s, refID: %s, payload: %s", kind, refID, payload)
		return fmt.Errorf("unknown kind: %s", kind)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", s.cfg.LedgerURL+"/debit", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", refID)

	log.Printf("[sendToLedger] POST %s/debit kind=%s refID=%s payload=%s", s.cfg.LedgerURL, kind, refID, payload)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[sendToLedger] error: %v", err)
		return err
	}
	defer resp.Body.Close()
	log.Printf("[sendToLedger] response status: %s", resp.Status)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ledger status: %s", resp.Status)
	}
	return nil
}

/*
   ===========================
   HTTP Handlers
   ===========================
*/

type postConsumptionIn struct {
	DeviceID  string   `json:"device_id" binding:"required"`
	TS        int64    `json:"ts" binding:"required"`
	Seq       int64    `json:"seq" binding:"required"`
	Quantity  *float64 `json:"quantity,omitempty"` // mutually exclusive with Counter
	Counter   *float64 `json:"counter,omitempty"`  // mutually exclusive with Quantity
	Signature string   `json:"signature"`
}

func (s *Service) postConsumptions(c *gin.Context) {
	var in postConsumptionIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	// exactly one of quantity or counter must be present
	if (in.Quantity == nil && in.Counter == nil) || (in.Quantity != nil && in.Counter != nil) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "must send exactly one of quantity or counter"})
		return
	}
	now := time.Now().Unix()

	var qtyVal any
	var cntVal any
	if in.Quantity != nil {
		qtyVal = *in.Quantity
	} else {
		qtyVal = nil
	}
	if in.Counter != nil {
		cntVal = *in.Counter
	} else {
		cntVal = nil
	}

	// Insert raw_event (idempotent by device_id,seq)
	res, err := s.db.Exec(`
		INSERT INTO raw_events(device_id, seq, ts, quantity, counter, signature, created_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(device_id, seq) DO NOTHING
	`, in.DeviceID, in.Seq, in.TS, qtyVal, cntVal, in.Signature, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db insert raw_events"})
		return
	}

	var rawID int64
	affected, _ := res.RowsAffected()
	if affected == 0 {
		// already exists → load id
		row := s.db.QueryRow(`SELECT id FROM raw_events WHERE device_id=? AND seq=?`, in.DeviceID, in.Seq)
		if err := row.Scan(&rawID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db select raw_event"})
			return
		}
	} else {
		row := s.db.QueryRow(`SELECT last_insert_rowid()`)
		if err := row.Scan(&rawID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db last_insert_rowid"})
			return
		}
	}

	// Enqueue
	_, err = s.db.Exec(`INSERT INTO queue_events(raw_event_id, status, created_at) VALUES(?,?,?)`,
		rawID, "pending", now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db enqueue"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"status": "queued", "raw_event_id": rawID})
}

func (s *Service) health(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
}

func nullInt64ToNative(v sql.NullInt64) any {
	if v.Valid {
		return v.Int64
	}
	return nil
}

func nullFloat64ToNative(v sql.NullFloat64) any {
	if v.Valid {
		return v.Float64
	}
	return nil
}

func nullStringToNative(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}

// Internal support endpoints
func (s *Service) getDeviceState(c *gin.Context) {
	deviceID := c.Query("device_id")
	if deviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing device_id"})
		return
	}
	var st deviceState
	row := s.db.QueryRow(`SELECT partial_remainder, current_bucket, sum_in_bucket, sum_for_threshold, last_counter, last_ts, updated_at FROM device_state WHERE device_id=?`, deviceID)
	err := row.Scan(&st.PartialRemainder, &st.CurrentBucket, &st.SumInBucket, &st.SumForThreshold, &st.LastCounter, &st.LastTS, &st.UpdatedAt)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"partial_remainder": st.PartialRemainder,
		"current_bucket":    st.CurrentBucket,
		"sum_in_bucket":     st.SumInBucket,
		"sum_for_threshold": st.SumForThreshold,
		"last_counter":      nullFloat64ToNative(st.LastCounter),
		"last_ts":           nullInt64ToNative(st.LastTS),
		"updated_at":        st.UpdatedAt,
	})
}

func (s *Service) getQueue(c *gin.Context) {
	deviceID := c.Query("device_id")
	rows, err := s.db.Query(`SELECT q.id, q.status, q.attempts, q.worker_id, q.claimed_at, q.last_error, q.created_at, re.seq, re.ts, re.quantity, re.counter FROM queue_events q JOIN raw_events re ON re.id = q.raw_event_id WHERE re.device_id=? ORDER BY q.id DESC LIMIT 100`, deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, attempts, claimedAt, createdAt, seq, ts int64
		var status string
		var workerID, lastError sql.NullString
		var quantity, counter sql.NullFloat64
		err := rows.Scan(&id, &status, &attempts, &workerID, &claimedAt, &lastError, &createdAt, &seq, &ts, &quantity, &counter)
		if err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id":         id,
			"status":     status,
			"attempts":   attempts,
			"worker_id":  nullStringToNative(workerID),
			"claimed_at": claimedAt,
			"last_error": nullStringToNative(lastError),
			"created_at": createdAt,
			"seq":        seq,
			"ts":         ts,
			"quantity":   nullFloat64ToNative(quantity),
			"counter":    nullFloat64ToNative(counter),
		})
	}
	c.JSON(http.StatusOK, out)
}

func (s *Service) getBatches(c *gin.Context) {
	deviceID := c.Query("device_id")
	rows, err := s.db.Query(`SELECT id, device_id, window_start, window_end, units, unit, unit_price_sats, total_sats, status, created_at FROM consumption_batches WHERE device_id=? ORDER BY created_at DESC LIMIT 100`, deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, deviceID string
		var windowStart, windowEnd sql.NullInt64
		var createdAt int64
		var units, unitPrice, totalSats float64
		var unit, status string
		err := rows.Scan(&id, &deviceID, &windowStart, &windowEnd, &units, &unit, &unitPrice, &totalSats, &status, &createdAt)
		if err != nil {
			log.Printf("[getBatches] scan error: %v", err)
			continue
		}
		out = append(out, map[string]any{
			"id":              id,
			"device_id":       deviceID,
			"window_start":    nullInt64ToNative(windowStart),
			"window_end":      nullInt64ToNative(windowEnd),
			"units":           units,
			"unit":            unit,
			"unit_price_sats": unitPrice,
			"total_sats":      totalSats,
			"status":          status,
			"created_at":      createdAt,
		})
	}
	c.JSON(http.StatusOK, out)
}

// Debug: dump all queue_events for all devices
func (s *Service) getQueueAll(c *gin.Context) {
	rows, err := s.db.Query(`SELECT q.id, q.status, q.attempts, q.worker_id, q.claimed_at, q.last_error, q.created_at, re.device_id, re.seq, re.ts, re.quantity, re.counter FROM queue_events q JOIN raw_events re ON re.id = q.raw_event_id ORDER BY q.id DESC LIMIT 200`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, attempts, claimedAt, createdAt, seq, ts int64
		var status, deviceID string
		var workerID, lastError sql.NullString
		var quantity, counter sql.NullFloat64
		err := rows.Scan(&id, &status, &attempts, &workerID, &claimedAt, &lastError, &createdAt, &deviceID, &seq, &ts, &quantity, &counter)
		if err != nil {
			log.Printf("[getQueueAll] scan error: %v", err)
			continue
		}
		log.Printf("[getQueueAll] row: id=%d status=%s attempts=%d worker_id=%s claimed_at=%d last_error=%s created_at=%d device_id=%s seq=%d ts=%d quantity=%v counter=%v",
			id, status, attempts, workerID.String, claimedAt, lastError.String, createdAt, deviceID, seq, ts, quantity, counter)
		out = append(out, map[string]any{
			"id":         id,
			"status":     status,
			"attempts":   attempts,
			"worker_id":  nullStringToNative(workerID),
			"claimed_at": claimedAt,
			"last_error": nullStringToNative(lastError),
			"created_at": createdAt,
			"device_id":  deviceID,
			"seq":        seq,
			"ts":         ts,
			"quantity":   nullFloat64ToNative(quantity),
			"counter":    nullFloat64ToNative(counter),
		})
	}
	c.JSON(http.StatusOK, out)
}

/*
   ===========================
   Signature Verification
   ===========================
*/

func verifySignature(secret, deviceID string, ts int64, seq int64, qty sql.NullFloat64, cnt sql.NullFloat64, signature string) bool {
	var valStr string
	switch {
	case cnt.Valid:
		valStr = fmt.Sprintf("%.6f", cnt.Float64)
	case qty.Valid:
		valStr = fmt.Sprintf("%.6f", qty.Float64)
	default:
		return false
	}
	msg := fmt.Sprintf("%d|%s|%d|%s", ts, deviceID, seq, valStr)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(msg))
	expected := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

/*
   ===========================
   main
   ===========================
*/

func main() {
	cfg := loadConfig()
	db := initDB(cfg.DBPath)
	defer db.Close()

	svc := NewService(cfg, db)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Background loops
	go svc.workerLoop(ctx)
	go svc.dispatcherLoop(ctx)

	// HTTP
	r := gin.Default()
	r.POST("/consumptions", svc.postConsumptions)
	r.GET("/health", svc.health)

	// Internal support endpoints
	r.GET("/internal/device-state", svc.getDeviceState)
	r.GET("/internal/queue", svc.getQueue)
	r.GET("/internal/batches", svc.getBatches)
	r.GET("/internal/queue-all", svc.getQueueAll)

	log.Printf("Consumption Service on :8080 (DB=%s)", cfg.DBPath)
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
