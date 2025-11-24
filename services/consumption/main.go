package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/robertodantas/lnpay/library"
	_ "modernc.org/sqlite"
)

/*
   =========================================
   Config & bootstrap
   =========================================
*/

type Config struct {
	DBPath        string
	ServiceToken  string
	ListenAddr    string
	GRPCAddr      string
	MaxPageSize   int
	BusyTimeoutMS int
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

func loadConfig() Config {
	return Config{
		DBPath:        getenv("DB_PATH", "consumption.db"),
		ServiceToken:  getenv("SERVICE_TOKEN", "dev-token"),
		ListenAddr:    getenv("LISTEN_ADDR", ":8080"),
		GRPCAddr:      getenv("GRPC_ADDR", ":9090"),
		MaxPageSize:   intEnv("MAX_PAGE_SIZE", 200),
		BusyTimeoutMS: intEnv("BUSY_TIMEOUT_MS", 5000),
	}
}

/*
   =========================================
   SQLite init (WAL + schema)
   =========================================
*/

func initDB(cfg Config) *sql.DB {
	// WAL + busy_timeout for concurrent writers on edge devices.
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", cfg.DBPath, cfg.BusyTimeoutMS)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}

	stmts := []string{
		// Consumption records table - stores processed usage records per device_id with idempotency
		// This is the source of truth for business data
		`CREATE TABLE IF NOT EXISTS consumption_records (
			report_id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL,
			authorization_id TEXT,
			debit_msat INTEGER NOT NULL,
			measure REAL NOT NULL,
			price_per_unit_msat INTEGER NOT NULL,
			unit TEXT NOT NULL,
			timestamp TEXT NOT NULL,
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
		`CREATE INDEX IF NOT EXISTS idx_authorization_id ON consumption_records (authorization_id)`,
		// Index for consumption_outbox
		`CREATE INDEX IF NOT EXISTS idx_published_created ON consumption_outbox (published, created_at)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			log.Fatalf("schema: %v", err)
		}
	}
	return db
}

type Service struct {
	cfg Config
	db  *sql.DB
}

func NewService(cfg Config, db *sql.DB) *Service {
	return &Service{cfg: cfg, db: db}
}

func main() {
	cfg := loadConfig()
	db := initDB(cfg)
	defer db.Close()

	svc := NewService(cfg, db)

	// Connect to Redis stream
	log.Println("Connecting to Redis...")
	streamClient, err := library.NewStreamClientFromEnv()
	if err != nil {
		log.Fatalf("Failed to create Redis stream client: %v", err)
	}
	defer streamClient.Close()
	log.Println("Redis stream client connected successfully")

	// Create stream handler
	streamHandler := NewStreamHandler(streamClient, svc)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start device event consumer (consumes from event.device stream)
	go func() {
		if err := streamHandler.StartDeviceConsumer(ctx); err != nil && err != context.Canceled {
			log.Printf("Device consumer error: %v", err)
		}
	}()

	// Start outbox publisher (publishes to event.consumption stream)
	go func() {
		if err := streamHandler.StartOutboxPublisher(ctx); err != nil && err != context.Canceled {
			log.Printf("Outbox publisher error: %v", err)
		}
	}()

	// Start outbox cleanup (removes old published records after retention period)
	go func() {
		if err := streamHandler.StartOutboxCleanup(ctx); err != nil && err != context.Canceled {
			log.Printf("Outbox cleanup error: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down consumption service...")
	cancel() // Cancel context to stop consumers
	log.Println("Consumption service stopped")
}
