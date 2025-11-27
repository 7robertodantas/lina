package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/robertodantas/lnpay/internal"
	lightningpb "github.com/robertodantas/lnpay/proto/gen/interfaces/lightning"
	"google.golang.org/grpc"
)

func main() {
	// Load configuration
	cfg := LoadConfig()

	// Connect to Redis stream
	log.Println("Connecting to Redis...")
	streamClient, err := internal.NewStreamClientFromEnv()
	if err != nil {
		log.Fatalf("Failed to create Redis stream client: %v", err)
	}
	defer streamClient.Close()
	log.Println("Redis stream client connected successfully")

	// Log configuration (masked for security)
	log.Printf("Configuration loaded: LND_HOST=%s, Network=%s, TLS_CERT=%s..., MACAROON=%s...", cfg.LNDHost, cfg.Network, cfg.LNDTLSCertHex[:min(20, len(cfg.LNDTLSCertHex))], cfg.LNDMacaroonHex[:min(20, len(cfg.LNDMacaroonHex))])

	// Create LND client
	lndClient, err := NewLNDClient(*cfg)
	if err != nil {
		log.Fatalf("Failed to create LND client: %v", err)
	}
	defer lndClient.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get and display node info
	info, err := lndClient.GetInfo(ctx)
	if err != nil {
		log.Fatalf("Failed to get node info: %v", err)
	}

	log.Printf("Node Info: Alias=%s, Identity=%s, Version=%s, BlockHeight=%d, SyncedToChain=%v, ActiveChannels=%d", info.Alias, info.IdentityPubkey, info.Version, info.BlockHeight, info.SyncedToChain, info.NumActiveChannels)

	// Create LND event stream
	lndEventStream := NewLNDEventStream(lndClient)
	if err := lndEventStream.Start(ctx); err != nil {
		log.Fatalf("Failed to start LND event stream: %v", err)
	}

	// Create stream publisher (publishes to Redis)
	streamPublisher := NewStreamPublisher(streamClient, lndEventStream)
	if err := streamPublisher.Start(ctx); err != nil {
		log.Fatalf("Failed to start stream publisher: %v", err)
	}

	// Create northbound REST interface
	log.Println("Initializing northbound REST API...")
	northbound := NewNorthboundInterface(lndClient, cfg)
	go func() {
		log.Printf("Lightning northbound REST listening on %s", cfg.ListenAddr)
		if err := northbound.Start(cfg.ListenAddr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start northbound server: %v", err)
		}
	}()

	// Start gRPC server in a goroutine
	go func() {
		lis, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			log.Fatalf("failed to listen on %s: %v", cfg.GRPCAddr, err)
		}

		grpcServer := grpc.NewServer()
		eastWestServer := NewEastWestServer(lndClient, streamPublisher)
		lightningpb.RegisterLightningServiceServer(grpcServer, eastWestServer)

		log.Printf("gRPC server listening on %s", cfg.GRPCAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve gRPC: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down lightning service...")
	cancel() // Cancel context to stop consumers

	// Gracefully shutdown northbound server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := northbound.Stop(shutdownCtx); err != nil {
		log.Printf("Error shutting down northbound server: %v", err)
	}

	log.Println("Lightning service stopped")
}
