package main

import (
	"github.com/robertodantas/lina/internal"
)

type Config struct {
	DBPath       string
	ServiceToken string
	ListenAddr   string
	GRPCAddr     string
	MaxPageSize  int

	// Redis stream: consumer name (empty = auto ledger-{hostname}-{pid}). Parallelism = max concurrent handlers per batch.
	StreamConsumerName string
	ConsumeParallelism int

	// OpenTelemetry / Jaeger
	OTELExporterOTLPEndpoint string
	OTELServiceName          string
}

func LoadConfig() Config {
	return Config{
		DBPath:       internal.GetEnv("DB_PATH", "ledger-pebble"),
		ServiceToken: internal.GetEnv("SERVICE_TOKEN", "dev-token"),
		ListenAddr:   internal.GetEnv("LISTEN_ADDR", ":8080"),
		GRPCAddr:     internal.GetEnv("GRPC_ADDR", ":9090"),
		MaxPageSize:  internal.IntEnv("MAX_PAGE_SIZE", 200),

		StreamConsumerName: internal.GetEnv("REDIS_STREAM_CONSUMER_NAME", "ledger-service"),
		ConsumeParallelism: internal.IntEnv("LEDGER_STREAM_PARALLELISM", 8),

		// OpenTelemetry / Jaeger
		OTELExporterOTLPEndpoint: internal.GetEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		OTELServiceName:          internal.GetEnv("OTEL_SERVICE_NAME", "ledger-service"),
	}
}
