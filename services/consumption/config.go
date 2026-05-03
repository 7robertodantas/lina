package main

import (
	"github.com/robertodantas/lina/internal"
)

type Config struct {
	RepositoryType string // pebble (default), sqlite, or postgres
	DBPath         string
	BusyTimeoutMS  int    // SQLite busy_timeout pragma (ignored for pebble/postgres)
	PostgresDSN    string // Postgres connection string (used when RepositoryType=postgres)
	PGMaxOpenConns int    // Postgres connection pool size; > 1 enables concurrent writers
	ServiceToken   string
	ListenAddr     string
	GRPCAddr       string
	MaxPageSize    int

	// Redis streams: REDIS_STREAM_CONSUMER_NAME; STREAM_PARALLELISM / STREAM_READ_COUNT (or map from CONSUMPTION_STREAM_* in compose/ansible).
	StreamConsumerName string
	ConsumeParallelism int
	StreamReadCount    int

	// OpenTelemetry / Jaeger
	OTELExporterOTLPEndpoint string
	OTELServiceName          string
}

func LoadConfig() Config {
	return Config{
		RepositoryType: internal.GetEnv("REPOSITORY_TYPE", "pebble"),
		DBPath:         internal.GetEnv("DB_PATH", "consumption-pebble"),
		BusyTimeoutMS:  internal.IntEnv("BUSY_TIMEOUT_MS", 5000),
		PostgresDSN:    internal.GetEnv("PG_DSN", "postgres://ledger:ledger@localhost:5432/ledger?sslmode=disable"),
		PGMaxOpenConns: internal.IntEnv("PG_MAX_OPEN_CONNS", 10),
		ServiceToken:   internal.GetEnv("SERVICE_TOKEN", "dev-token"),
		ListenAddr:     internal.GetEnv("LISTEN_ADDR", ":8080"),
		GRPCAddr:       internal.GetEnv("GRPC_ADDR", ":9090"),
		MaxPageSize:    internal.IntEnv("MAX_PAGE_SIZE", 200),

		StreamConsumerName: internal.GetEnv("REDIS_STREAM_CONSUMER_NAME", "consumption-service"),
		ConsumeParallelism: internal.StreamParallelismFromEnv("CONSUMPTION_STREAM_PARALLELISM", 4),
		StreamReadCount:      internal.StreamReadCountFromEnv("CONSUMPTION_STREAM_READ_COUNT", 100),

		// OpenTelemetry / Jaeger
		OTELExporterOTLPEndpoint: internal.GetEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		OTELServiceName:          internal.GetEnv("OTEL_SERVICE_NAME", "consumption-service"),
	}
}
