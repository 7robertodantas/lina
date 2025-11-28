package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/robertodantas/lnpay/internal"
)

var logger = internal.NewLogger("consumption")

func main() {
	logger.Info("Starting consumption service")

	cfg := LoadConfig()
	repository, err := NewConsumptionRepository(cfg.DBPath, cfg.BusyTimeoutMS)
	if err != nil {
		logger.Fatal("Failed to create consumption repository", err)
	}
	defer repository.Close()

	// Connect to Redis stream
	logger.Info("Connecting to Redis")
	streamClient, err := internal.NewStreamClientFromEnv()
	if err != nil {
		logger.Fatal("Failed to create Redis stream client", err)
	}
	defer streamClient.Close()
	logger.Info("Redis stream client connected successfully")

	// Create stream handler
	streamHandler := NewStreamHandler(streamClient, cfg, repository)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start device event consumer (consumes from event.device stream)
	go func() {
		if err := streamHandler.StartDeviceConsumer(ctx); err != nil && err != context.Canceled {
			logger.WithStream("event.device", "consume").
				Error("Device consumer error", err)
		}
	}()

	// Start outbox publisher (publishes to event.consumption stream)
	go func() {
		if err := streamHandler.StartOutboxPublisher(ctx); err != nil && err != context.Canceled {
			logger.WithStream("event.consumption", "produce").
				Error("Outbox publisher error", err)
		}
	}()

	// Start outbox cleanup (removes old published records after retention period)
	go func() {
		if err := streamHandler.StartOutboxCleanup(ctx); err != nil && err != context.Canceled {
			logger.Error("Outbox cleanup error", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down consumption service")
	cancel() // Cancel context to stop consumers
	logger.Info("Consumption service stopped")
}
