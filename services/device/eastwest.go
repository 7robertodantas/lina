package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	ledgerpb "github.com/robertodantas/lnpay/proto/gen/interfaces/ledger"
	ledgermodel "github.com/robertodantas/lnpay/proto/gen/model/ledger"
)

// LedgerClient wraps the gRPC client for the ledger service
type LedgerClient struct {
	client ledgerpb.LedgerServiceClient
	conn   *grpc.ClientConn
}

// NewLedgerClient creates a new gRPC client connection to the ledger service
func NewLedgerClient() (*LedgerClient, error) {
	host := getEnv("LEDGER_GRPC_HOST", "ledger")
	port := getEnvInt("LEDGER_GRPC_PORT", 9090)

	addr := fmt.Sprintf("%s:%d", host, port)
	log.Printf("Connecting to ledger gRPC service at %s...", addr)

	// Configure keepalive for long-lived connections
	keepaliveParams := keepalive.ClientParameters{
		Time:                10 * time.Second,
		Timeout:             3 * time.Second,
		PermitWithoutStream: true,
	}

	// Create gRPC connection (using insecure for now, can be upgraded to TLS later)
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepaliveParams),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %w", err)
	}

	client := ledgerpb.NewLedgerServiceClient(conn)

	log.Printf("Connected to ledger gRPC service at %s", addr)

	return &LedgerClient{
		client: client,
		conn:   conn,
	}, nil
}

// Close closes the gRPC connection
func (c *LedgerClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// CreateOrGetAuthorization creates a new authorization or returns the active one for the device
func (c *LedgerClient) CreateOrGetAuthorization(ctx context.Context, deviceID string, requestMsat int64, reason string) (*ledgermodel.CreateAuthorizationResponse, error) {
	req := &ledgermodel.CreateAuthorizationRequest{
		DeviceId:    deviceID,
		RequestMsat: requestMsat,
		Reason:      reason,
	}

	resp, err := c.client.CreateOrGetAuthorization(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create or get authorization: %w", err)
	}

	return resp, nil
}
