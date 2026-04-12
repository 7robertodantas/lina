package main

import (
	"context"
	"fmt"

	ledgerpb "github.com/robertodantas/lina/proto/gen/interfaces/ledger"
	ledgermodel "github.com/robertodantas/lina/proto/gen/model/ledger"
	"google.golang.org/grpc"
)

// LedgerClient wraps the gRPC client for the ledger service
type LedgerClient struct {
	client ledgerpb.LedgerServiceClient
	conn   *grpc.ClientConn
}

// NewLedgerClient creates a new gRPC client connection to the ledger service
func NewLedgerClient(ctx context.Context, cfg Config) (*LedgerClient, error) {
	host := cfg.LedgerGRPCHost
	port := cfg.LedgerGRPCPort

	addr := fmt.Sprintf("%s:%d", host, port)
	logger.Infof(ctx, "Connecting to ledger gRPC service at %s via eastwest gRPC", addr)

	dialOpts, err := eastWestGRPCDialOptions(cfg, host)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %w", err)
	}

	client := ledgerpb.NewLedgerServiceClient(conn)

	logger.Info(ctx, "Connected to ledger service")

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
func (c *LedgerClient) CreateOrGetAuthorization(ctx context.Context, deviceID string, requestID string, requestMsat int64, reason string) (*ledgermodel.CreateAuthorizationResponse, error) {
	req := &ledgermodel.CreateAuthorizationRequest{
		DeviceId:    deviceID,
		RequestId:   requestID,
		RequestMsat: requestMsat,
		Reason:      reason,
	}

	resp, err := c.client.CreateOrGetAuthorization(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create or get authorization: %w", err)
	}

	return resp, nil
}
