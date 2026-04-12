package main

import (
	"context"
	"fmt"

	lightningpb "github.com/robertodantas/lina/proto/gen/interfaces/lightning"
	lightningmodel "github.com/robertodantas/lina/proto/gen/model/lightning"
	"google.golang.org/grpc"
)

// LightningClient wraps the gRPC client for the lightning service
type LightningClient struct {
	client lightningpb.LightningServiceClient
	conn   *grpc.ClientConn
}

// NewLightningClient creates a new gRPC client connection to the lightning service
func NewLightningClient(ctx context.Context, cfg Config) (*LightningClient, error) {
	host := cfg.LightningGRPCHost
	port := cfg.LightningGRPCPort

	addr := fmt.Sprintf("%s:%d", host, port)
	logger.Infof(ctx, "Connecting to lightning gRPC service at %s via eastwest gRPC", addr)

	dialOpts, err := eastWestGRPCDialOptions(cfg, host)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create lightning gRPC client: %w", err)
	}

	client := lightningpb.NewLightningServiceClient(conn)

	logger.Info(ctx, "Connected to lightning service")

	return &LightningClient{
		client: client,
		conn:   conn,
	}, nil
}

// Close closes the gRPC connection
func (c *LightningClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// CreateInvoice requests a new invoice from the lightning service
func (c *LightningClient) CreateInvoice(ctx context.Context, deviceID string, amountMsat int64, reason string) (*lightningmodel.CreateInvoiceResponse, error) {
	req := &lightningmodel.CreateInvoiceRequest{
		DeviceId:   deviceID,
		AmountMsat: amountMsat,
		Reason:     reason,
	}

	resp, err := c.client.CreateInvoice(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create invoice: %w", err)
	}

	return resp, nil
}
