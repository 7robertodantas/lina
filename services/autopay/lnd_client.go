package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	internalpkg "github.com/robertodantas/lnpay/internal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
)

type LNDClient struct {
	conn         *grpc.ClientConn
	client       lnrpc.LightningClient
	routerClient routerrpc.RouterClient
}

// LNDConfig holds LND connection configuration
type LNDConfig struct {
	Host          string
	TLSCertHex    string
	TLSServerName string
	MacaroonHex   string
}

// NewLNDClient creates a new LND client from configuration
func NewLNDClient(ctx context.Context, lndCfg LNDConfig, clientName string) (*LNDClient, error) {
	// Decode hex TLS certificate
	tlsCertBytes, err := hex.DecodeString(lndCfg.TLSCertHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode TLS cert: %w", err)
	}

	// Create certificate pool
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(tlsCertBytes) {
		return nil, fmt.Errorf("failed to append TLS certificate")
	}

	// Create TLS credentials
	tlsConfig := &tls.Config{
		RootCAs:    certPool,
		ServerName: lndCfg.TLSServerName,
	}
	creds := credentials.NewTLS(tlsConfig)

	// Decode hex macaroon
	macaroonBytes, err := hex.DecodeString(lndCfg.MacaroonHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode macaroon: %w", err)
	}

	// Create macaroon credential
	mac := &macaroonCredential{macaroon: macaroonBytes}

	// Dial LND
	logger.Infof(ctx, "Dialing LND host %s (%s)", lndCfg.Host, clientName)
	conn, err := grpc.NewClient(
		lndCfg.Host,
		grpc.WithTransportCredentials(creds),
		grpc.WithPerRPCCredentials(mac),
		grpc.WithUnaryInterceptor(internalpkg.LoggingUnaryClientInterceptor("autopay")),
	)
	if err != nil {
		logger.Errorf(ctx, "Failed to dial LND host %s (%s): %v", lndCfg.Host, clientName, err)
		return nil, fmt.Errorf("failed to dial LND: %w", err)
	}

	// Manually wait for the channel to reach READY
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn.Connect()
	state := conn.GetState()
	for {
		if state == connectivity.Ready {
			break
		}
		if ok := conn.WaitForStateChange(connectCtx, state); !ok {
			conn.Close()
			return nil, fmt.Errorf("timed out waiting for LND connection (last state: %s)", state)
		}
		state = conn.GetState()
	}
	logger.Infof(connectCtx, "Successfully connected to LND host %s (%s)", lndCfg.Host, clientName)

	// Create clients
	client := lnrpc.NewLightningClient(conn)
	routerClient := routerrpc.NewRouterClient(conn)

	return &LNDClient{
		conn:         conn,
		client:       client,
		routerClient: routerClient,
	}, nil
}

// macaroonCredential implements grpc.PerRPCCredentials
type macaroonCredential struct {
	macaroon []byte
}

func (m *macaroonCredential) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		"macaroon": hex.EncodeToString(m.macaroon),
	}, nil
}

func (m *macaroonCredential) RequireTransportSecurity() bool {
	return true
}

// SubscribeInvoices creates a subscription stream for invoice updates
func (c *LNDClient) SubscribeInvoices(ctx context.Context, addIndex, settleIndex uint64) (lnrpc.Lightning_SubscribeInvoicesClient, error) {
	logger.InfoWithFields(ctx, "Subscribing to LND invoices stream", map[string]interface{}{
		"add_index":    addIndex,
		"settle_index": settleIndex,
	})
	return c.client.SubscribeInvoices(ctx, &lnrpc.InvoiceSubscription{
		AddIndex:    addIndex,
		SettleIndex: settleIndex,
	})
}

// PayInvoice pays an invoice using the bolt11 payment request
func (c *LNDClient) PayInvoice(ctx context.Context, paymentRequest string) (*lnrpc.SendResponse, error) {
	logger.InfoWithFields(ctx, "Paying invoice", map[string]interface{}{
		"payment_request": paymentRequest[:min(50, len(paymentRequest))] + "...",
	})

	req := &routerrpc.SendPaymentRequest{
		PaymentRequest: paymentRequest,
		TimeoutSeconds: 60,
		FeeLimitMsat:   1000000, // 1 sat fee limit for regtest
	}

	stream, err := c.routerClient.SendPaymentV2(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment stream: %w", err)
	}

	// Wait for payment response - stream sends multiple status updates
	// We wait for SUCCEEDED or FAILED status
	for {
		response, err := stream.Recv()
		if err != nil {
			return nil, fmt.Errorf("failed to receive payment response: %w", err)
		}

		status := response.GetStatus()

		// Check if payment is complete (succeeded or failed)
		// Payment status: UNKNOWN, IN_FLIGHT, SUCCEEDED, FAILED
		if status == lnrpc.Payment_IN_FLIGHT {
			// Payment in progress, continue waiting
			continue
		}

		// Payment completed (either succeeded or failed)
		paymentHash, _ := hex.DecodeString(response.GetPaymentHash())
		result := &lnrpc.SendResponse{
			PaymentHash: paymentHash,
		}

		if status == lnrpc.Payment_SUCCEEDED {
			paymentPreimage, _ := hex.DecodeString(response.GetPaymentPreimage())
			result.PaymentPreimage = paymentPreimage
		} else if status == lnrpc.Payment_FAILED {
			// Payment failed
			result.PaymentError = response.GetFailureReason().String()
		}

		return result, nil
	}
}

// Close closes the connection
func (c *LNDClient) Close() error {
	if c.conn != nil {
		logger.Info(context.Background(), "Closing LND gRPC connection")
		return c.conn.Close()
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
