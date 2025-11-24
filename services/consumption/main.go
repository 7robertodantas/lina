package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/robertodantas/lnpay/library"
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

func main() {
	cfg := loadConfig()
	repository, err := NewConsumptionRepository(cfg.DBPath, cfg.BusyTimeoutMS)
	if err != nil {
		log.Fatalf("Failed to create consumption repository: %v", err)
	}
	defer repository.Close()

	// Connect to Redis stream
	log.Println("Connecting to Redis...")
	streamClient, err := library.NewStreamClientFromEnv()
	if err != nil {
		log.Fatalf("Failed to create Redis stream client: %v", err)
	}
	defer streamClient.Close()
	log.Println("Redis stream client connected successfully")

	// Create stream handler
	streamHandler := NewStreamHandler(streamClient, cfg, repository)

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
