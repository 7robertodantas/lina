package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	ledgerpb "github.com/robertodantas/lnpay/proto/gen/interfaces/ledger"
	ledgermodel "github.com/robertodantas/lnpay/proto/gen/model/ledger"
)

// LedgerClient wraps the gRPC client for the ledger service
type LedgerClient struct {
	client ledgerpb.LedgerServiceClient
	conn   *grpc.ClientConn
}

// simplifyMethodName extracts service and method name from full gRPC method path
// Example: /iot.payperuse.edge.interfaces.sync.ledger.LedgerService/CreateOrGetAuthorization
// Returns: LedgerService/CreateOrGetAuthorization
func simplifyMethodName(method string) string {
	// Remove leading slash
	method = strings.TrimPrefix(method, "/")

	// Split by / to separate service path from method name
	parts := strings.Split(method, "/")
	if len(parts) != 2 {
		// If format is unexpected, return as-is
		return method
	}

	servicePath := parts[0]
	methodName := parts[1]

	// Split service path by dots and take the last part (service name)
	serviceParts := strings.Split(servicePath, ".")
	if len(serviceParts) == 0 {
		return method
	}

	serviceName := serviceParts[len(serviceParts)-1]

	return fmt.Sprintf("%s/%s", serviceName, methodName)
}

// loggingUnaryInterceptor logs gRPC requests and responses
func loggingUnaryInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	start := time.Now()
	simpleMethod := simplifyMethodName(method)

	// Log request
	log.Printf("[gRPC] Calling %s with request: %+v", simpleMethod, req)

	// Invoke the actual RPC
	err := invoker(ctx, method, req, reply, cc, opts...)

	// Calculate duration
	duration := time.Since(start)

	// Log response or error
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			log.Printf("[gRPC] %s failed: code=%s, message=%s, duration=%v", simpleMethod, st.Code(), st.Message(), duration)
		} else {
			log.Printf("[gRPC] %s failed: error=%v, duration=%v", simpleMethod, err, duration)
		}
	} else {
		log.Printf("[gRPC] %s succeeded: response=%+v, duration=%v", simpleMethod, reply, duration)
	}

	return err
}

// NewLedgerClient creates a new gRPC client connection to the ledger service
func NewLedgerClient() (*LedgerClient, error) {
	host := getEnv("LEDGER_GRPC_HOST", "ledger")
	port := getEnvInt("LEDGER_GRPC_PORT", 9090)

	addr := fmt.Sprintf("%s:%d", host, port)
	log.Printf("Connecting to ledger gRPC service at %s...", addr)

	// Configure keepalive for long-lived connections
	// Time: 30s is a reasonable interval to avoid "too_many_pings" errors
	keepaliveParams := keepalive.ClientParameters{
		Time:                30 * time.Second,
		Timeout:             10 * time.Second,
		PermitWithoutStream: true,
	}

	// Create gRPC connection (using insecure for now, can be upgraded to TLS later)
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepaliveParams),
		grpc.WithUnaryInterceptor(loggingUnaryInterceptor),
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
