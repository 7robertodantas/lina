package main

import (
	"context"
	"fmt"
	"time"

	lightningservicepb "github.com/robertodantas/lnpay/proto/gen/interfaces/lightning"
	lightningmodel "github.com/robertodantas/lnpay/proto/gen/model/lightning"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultInvoiceExpirySeconds = 3600

// EastWestServer implements the LightningService gRPC server using the shared proto models.
type EastWestServer struct {
	lightningservicepb.UnimplementedLightningServiceServer
	lndClient       *LNDClient
	streamPublisher *StreamPublisher
}

func NewEastWestServer(lndClient *LNDClient, streamPublisher *StreamPublisher) *EastWestServer {
	return &EastWestServer{
		lndClient:       lndClient,
		streamPublisher: streamPublisher,
	}
}

func (s *EastWestServer) CreateInvoice(ctx context.Context, req *lightningmodel.CreateInvoiceRequest) (*lightningmodel.CreateInvoiceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.DeviceId == "" {
		return nil, status.Error(codes.InvalidArgument, "device_id is required")
	}
	if req.AmountMsat <= 0 {
		return nil, status.Error(codes.InvalidArgument, "amount_msat must be greater than 0")
	}

	logger.WithDeviceID(req.DeviceId).
		InfoWithFields(ctx, "CreateInvoice request received via eastwest gRPC", map[string]interface{}{
			"amount_msat": req.AmountMsat,
			"reason":      req.Reason,
		})

	memo := encodeInvoiceMetadata(req.DeviceId, req.Reason)
	invoiceResp, err := s.lndClient.CreateInvoice(ctx, req.AmountMsat, memo, defaultInvoiceExpirySeconds)
	if err != nil {
		logger.WithDeviceID(req.DeviceId).
			Error(ctx, "CreateInvoice failed via eastwest gRPC", err)
		return nil, status.Errorf(codes.Internal, "failed to create invoice: %v", err)
	}

	invoiceID := fmt.Sprintf("%x", invoiceResp.RHash)
	expiresAt := time.Now().UTC().Add(time.Duration(defaultInvoiceExpirySeconds) * time.Second).Format(time.RFC3339)

	logger.WithDeviceID(req.DeviceId).
		DebugWithFields(ctx, "Invoice response received via eastwest gRPC", map[string]interface{}{
			"invoice_id":      invoiceID,
			"payment_request": invoiceResp.PaymentRequest,
			"payment_hash":    fmt.Sprintf("%x", invoiceResp.RHash),
		})

	invoice := &lightningmodel.Invoice{
		InvoiceId:  invoiceID,
		DeviceId:   req.DeviceId,
		Bolt11:     invoiceResp.PaymentRequest,
		AmountMsat: req.AmountMsat,
		Status:     lightningmodel.InvoiceStatus_INVOICE_STATUS_CREATED,
		ExpiresAt:  expiresAt,
	}

	if s.streamPublisher != nil {
		if err := s.streamPublisher.PublishInvoiceCreated(ctx, invoice); err != nil {
			logger.WithDeviceID(req.DeviceId).
				WithStream("event.lightning", "produce").
				Error(ctx, "Failed to publish invoice created event via eastwest gRPC", err)
		}
	}

	logger.WithDeviceID(req.DeviceId).
		InfoWithFields(ctx, "Invoice created successfully via eastwest gRPC", map[string]interface{}{
			"invoice_id":  invoiceID,
			"amount_msat": req.AmountMsat,
			"expires_at":  expiresAt,
		})

	return &lightningmodel.CreateInvoiceResponse{
		Invoice: invoice,
	}, nil
}
