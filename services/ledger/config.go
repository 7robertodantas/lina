package main

import (
	"github.com/robertodantas/lnpay/library"
)

type Config struct {
	DBPath        string
	ServiceToken  string
	ListenAddr    string
	GRPCAddr      string
	MaxPageSize   int
	BusyTimeoutMS int
}

func LoadConfig() Config {
	return Config{
		DBPath:        library.GetEnv("DB_PATH", "ledger.db"),
		ServiceToken:  library.GetEnv("SERVICE_TOKEN", "dev-token"),
		ListenAddr:    library.GetEnv("LISTEN_ADDR", ":8080"),
		GRPCAddr:      library.GetEnv("GRPC_ADDR", ":9090"),
		MaxPageSize:   library.IntEnv("MAX_PAGE_SIZE", 200),
		BusyTimeoutMS: library.IntEnv("BUSY_TIMEOUT_MS", 5000),
	}
}

