package main

import (
	"context"
	"os"
	"time"

	"github.com/robertodantas/lina/internal"
)

// Redis stream client uses REDIS_HOST / REDIS_PORT / etc. (internal.NewStreamClientFromEnv). This service only publishes to streams, not XREADGROUP.
type Config struct {
	LNDHost          string
	LNDTLSCertHex    string
	LNDTLSServerName string
	LNDMacaroonHex   string
	Network          string
	ListenAddr       string
	GRPCAddr         string
	GRPCUseTLS     bool
	GRPCTLSCACert  string
	GRPCServerCert string
	GRPCServerKey  string
	ServiceToken     string
	RedisAddr        string
	RedisPassword    string

	// OpenTelemetry / Jaeger
	OTELExporterOTLPEndpoint string
	OTELServiceName          string

	// LightningEphemeralRetention is how long entries may remain in event.lightning.ephemeral
	// (created/expired) before XTRIM MINID drops them. Set via LIGHTNING_EPHEMERAL_RETENTION (Go duration, e.g. 1m).
	LightningEphemeralRetention time.Duration
}

func LoadConfig() *Config {
	cfg := &Config{
		LNDHost:          internal.GetEnv("LND_HOST", ""),
		LNDTLSCertHex:    internal.GetEnv("LND_TLS_CERT_HEX", ""),
		LNDTLSServerName: internal.GetEnv("LND_TLS_SERVER_NAME", "localhost"),
		LNDMacaroonHex:   internal.GetEnv("LND_MACAROON_HEX", ""),
		Network:          internal.GetEnv("NETWORK", "testnet"),
		GRPCAddr:       internal.GetEnv("GRPC_ADDR", ":9090"),
		GRPCUseTLS:     internal.BoolEnv("GRPC_USE_TLS", false),
		GRPCTLSCACert:  internal.GetEnv("GRPC_TLS_CA_CERT", "/certs/ca.crt"),
		GRPCServerCert: internal.GetEnv("GRPC_TLS_SERVER_CERT", "/certs/server.crt"),
		GRPCServerKey:  internal.GetEnv("GRPC_TLS_SERVER_KEY", "/certs/server.key"),
		ListenAddr:     internal.GetEnv("LISTEN_ADDR", ":8080"),
		ServiceToken:     internal.GetEnv("SERVICE_TOKEN", "dev-token"),
		RedisAddr:        internal.GetEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:    internal.GetEnv("REDIS_PASSWORD", ""),

		// OpenTelemetry / Jaeger
		OTELExporterOTLPEndpoint: internal.GetEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		OTELServiceName:          internal.GetEnv("OTEL_SERVICE_NAME", "lightning-service"),

		LightningEphemeralRetention: time.Minute,
	}

	if s := os.Getenv("LIGHTNING_EPHEMERAL_RETENTION"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			logger.Warnf(context.Background(), "Invalid LIGHTNING_EPHEMERAL_RETENTION %q, using 1m: %v", s, err)
		} else {
			cfg.LightningEphemeralRetention = d
		}
	}

	ctx := context.Background()

	// Validate configuration
	if cfg.LNDHost == "" {
		logger.Fatal(ctx, "LND_HOST environment variable required", nil)
	}
	if cfg.LNDTLSCertHex == "" {
		logger.Fatal(ctx, "LND_TLS_CERT_HEX environment variable required", nil)
	}
	if cfg.LNDMacaroonHex == "" {
		logger.Fatal(ctx, "LND_MACAROON_HEX environment variable required", nil)
	}

	return cfg
}
