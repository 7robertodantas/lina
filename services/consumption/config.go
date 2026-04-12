package main

import (
	"github.com/robertodantas/lina/internal"
)

type Config struct {
	RepositoryType string // pebble (default) or sqlite
	DBPath         string
	BusyTimeoutMS  int // SQLite busy_timeout pragma (ignored for pebble)
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
