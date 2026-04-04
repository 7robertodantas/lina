package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/robertodantas/lina/internal"
)

var logger = internal.NewLogger("device")

func main() {
	ctx := context.Background()

	logger.Info(ctx, "Starting device service")

	cfg := LoadConfig()

	// Initialize metrics (must be done before OpenTelemetry tracer to avoid conflicts)
	if err := initMetrics(); err != nil {
		logger.Warnf(ctx, "Failed to initialize metrics: %v. Continuing without metrics.", err)
	} else {
		logger.Info(ctx, "Metrics initialized successfully")
	}

	// Initialize OpenTelemetry
	tracerShutdown, err := internal.InitTracer(internal.TracerConfig{
		ServiceName:          cfg.OTELServiceName,
		ExporterOTLPEndpoint: cfg.OTELExporterOTLPEndpoint,
	})
	if err != nil {
		logger.Warnf(ctx, "Failed to initialize OpenTelemetry: %v. Continuing without tracing.", err)
	} else {
		logger.Infof(ctx, "OpenTelemetry initialized with OTLP exporter at %s", cfg.OTELExporterOTLPEndpoint)
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tracerShutdown(shutdownCtx); err != nil {
				logger.Errorf(shutdownCtx, "Error shutting down tracer: %v", err)
			}
		}()
	}

	serviceCtx, serviceCancel := context.WithCancel(ctx)
	defer serviceCancel()

	// Initialize device repository
	repo, err := NewDeviceRepository(ctx, cfg.DBPath, cfg.BusyTimeoutMS)
	if err != nil {
		logger.Fatal(ctx, "Failed to initialize device repository", err)
	}
	defer repo.Close()
	logger.Info(ctx, "Device repository initialized")

	// Bind the NanoMQ HTTP auth listener before connecting to MQTT so the broker's first
	// auth_req cannot race an unopened socket.
	authLn, err := net.Listen("tcp", cfg.MQTTAuthAddr)
	if err != nil {
		logger.Fatal(ctx, "Failed to listen for MQTT auth HTTP", err)
	}
	authServer := NewMQTTAuthServer(repo, cfg)
	go func() {
		if err := authServer.Serve(authLn); err != nil && err != http.ErrServerClosed {
			logger.Fatalf(ctx, "MQTT auth server error: %v", err)
		}
	}()

	// Connect to MQTT broker
	mqttClient, err := NewMQTTClient(ctx, cfg)
	if err != nil {
		logger.Fatal(ctx, "Failed to create MQTT client", err)
	}
	defer mqttClient.Disconnect()

	// Connect to Redis
	streamInterface, err := NewEastWestStreamInterface(ctx)
	if err != nil {
		logger.Fatal(ctx, "Failed to create Redis stream client", err)
	}
	defer streamInterface.Close()

	// Connect to ledger service via gRPC
	ledgerClient, err := NewLedgerClient(ctx, cfg)
	if err != nil {
		logger.Fatal(ctx, "Failed to create ledger gRPC client", err)
	}
	defer ledgerClient.Close()

	// Connect to lightning service via gRPC
	lightningClient, err := NewLightningClient(ctx, cfg)
	if err != nil {
		logger.Fatal(ctx, "Failed to create lightning gRPC client", err)
	}
	defer lightningClient.Close()

	// Initialize and start northbound REST API
	logger.Info(ctx, "Initializing northbound REST API")
	northbound := NewNorthboundInterface(repo, mqttClient, cfg)

	// On startup, republish config for all devices so configs are retained in MQTT
	if err := northbound.RepublishAllDeviceConfigs(ctx); err != nil {
		logger.Warnf(ctx, "Failed to republish device configs on startup: %v", err)
	}

	// Start northbound server in a goroutine
	go func() {
		if err := northbound.Start(ctx, cfg.APIAddr); err != nil && err != http.ErrServerClosed {
			logger.Fatalf(ctx, "Failed to start northbound API server: %v", err)
		}
	}()

	// Initialize and start southbound interface
	logger.Info(ctx, "Initializing southbound interface")
	invoiceTimeout := time.Duration(cfg.LightningRPCTimeoutSeconds) * time.Second
	southbound := NewSouthboundInterface(mqttClient, streamInterface, ledgerClient, lightningClient, repo, invoiceTimeout)
	if err := southbound.Start(ctx); err != nil {
		logger.Fatal(ctx, "Failed to start southbound interface", err)
	}

	// Create southbound publisher for east-west stream events
	southboundPublisher := NewSouthboundPublisher(mqttClient)

	// Start ledger balance subscriber to fan-out balance updates via MQTT
	streamInterface.StartLedgerBalanceSubscriber(serviceCtx, southboundPublisher)

	// Start lightning invoice event subscriber to fan-out invoice updates via MQTT
	streamInterface.StartLightningInvoiceSubscriber(serviceCtx, southboundPublisher)

	logger.Info(ctx, "Device service is running. Press Ctrl+C to stop")
	logger.Infof(ctx, "Northbound REST API listening on %s", cfg.APIAddr)

	metricsAddr := internal.GetEnv("METRICS_ADDR", ":9464")
	go func() {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", GetMetricsHandler())
		metricsServer := &http.Server{
			Addr:    metricsAddr,
			Handler: metricsMux,
		}
		logger.Infof(ctx, "Metrics server listening on %s/metrics", metricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf(ctx, "Failed to start metrics server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	logger.Info(ctx, "Shutting down device service")
	serviceCancel()

	// Gracefully shut down HTTP servers (northbound API, then NanoMQ auth callback).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := northbound.Stop(shutdownCtx); err != nil {
		logger.Errorf(shutdownCtx, "Error shutting down northbound server: %v", err)
	}
	if err := authServer.Stop(shutdownCtx); err != nil {
		logger.Errorf(shutdownCtx, "Error shutting down MQTT auth server: %v", err)
	}

	logger.Info(ctx, "Device service stopped")
}
