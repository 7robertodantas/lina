package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ledgerpb "github.com/robertodantas/lnpay/proto/gen/interfaces/ledger"
	ledgermodel "github.com/robertodantas/lnpay/proto/gen/model/ledger"
)

// EastWestServer implements the LedgerService gRPC server
type EastWestServer struct {
	ledgerpb.UnimplementedLedgerServiceServer
	svc *Service
}

// NewEastWestServer creates a new east-west gRPC server
func NewEastWestServer(svc *Service) *EastWestServer {
	return &EastWestServer{svc: svc}
}

// CreateOrGetAuthorization creates a new authorization or returns the active one for the device
func (s *EastWestServer) CreateOrGetAuthorization(ctx context.Context, req *ledgermodel.CreateAuthorizationRequest) (*ledgermodel.CreateAuthorizationResponse, error) {
	if req.DeviceId == "" {
		return nil, status.Errorf(codes.InvalidArgument, "device_id is required")
	}
	if req.RequestMsat <= 0 {
		return nil, status.Errorf(codes.InvalidArgument, "request_msat must be > 0")
	}

	// Convert msat to sats (1 sat = 1000 msat)
	requestSats := req.RequestMsat / 1000
	if requestSats == 0 {
		requestSats = 1 // minimum 1 sat
	}

	tx, err := s.svc.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Ensure balance row exists
	if err := s.svc.ensureBalanceRow(ctx, tx, req.DeviceId); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to ensure balance: %v", err)
	}

	// Get current balance
	balanceSats, err := s.svc.getBalance(ctx, tx, req.DeviceId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get balance: %v", err)
	}

	// Check for active authorization
	activeAuth, err := s.getActiveAuthorization(ctx, tx, req.DeviceId)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.Internal, "failed to check authorization: %v", err)
	}

	// If active authorization exists, return it
	if activeAuth != nil {
		// Convert sats to msat for response
		availableMsat := balanceSats * 1000
		return &ledgermodel.CreateAuthorizationResponse{
			Status:        ledgermodel.AuthorizationStatus_AUTHORIZATION_STATUS_ACTIVE,
			Authorization: activeAuth,
			AvailableMsat: availableMsat,
			Reason:        "Active authorization found",
		}, nil
	}

	// Check if we have sufficient balance
	balanceMsat := balanceSats * 1000
	if balanceMsat < req.RequestMsat {
		if err := tx.Commit(); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to commit: %v", err)
		}
		return &ledgermodel.CreateAuthorizationResponse{
			Status:        ledgermodel.AuthorizationStatus_AUTHORIZATION_STATUS_REJECTED,
			Authorization: nil,
			AvailableMsat: balanceMsat,
			Reason:        fmt.Sprintf("insufficient balance: have %d msat, need %d msat", balanceMsat, req.RequestMsat),
		}, nil
	}

	// Create new authorization
	authID := fmt.Sprintf("auth_%d_%s", time.Now().UnixNano(), req.DeviceId)
	now := time.Now()
	issuedAt := now.Format(time.RFC3339)
	expiresAt := now.Add(24 * time.Hour).Format(time.RFC3339) // 24 hour expiry

	// Insert authorization
	_, err = tx.ExecContext(ctx, `
		INSERT INTO authorizations(
			authorization_id, device_id, granted_msat, remaining_msat,
			issued_at, expires_at, status, created_at
		) VALUES(?,?,?,?,?,?,?,?)`,
		authID, req.DeviceId, req.RequestMsat, req.RequestMsat,
		issuedAt, expiresAt, "active", time.Now().Unix(),
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create authorization: %v", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to commit: %v", err)
	}

	// Build response
	auth := &ledgermodel.Authorization{
		DeviceId:        req.DeviceId,
		AuthorizationId: authID,
		GrantedMsat:     req.RequestMsat,
		RemainingMsat:   req.RequestMsat,
		IssuedAt:        issuedAt,
		ExpiresAt:       expiresAt,
	}

	return &ledgermodel.CreateAuthorizationResponse{
		Status:        ledgermodel.AuthorizationStatus_AUTHORIZATION_STATUS_GRANTED,
		Authorization: auth,
		AvailableMsat: balanceMsat,
		Reason:        req.Reason,
	}, nil
}

// getActiveAuthorization retrieves an active authorization for a device
func (s *EastWestServer) getActiveAuthorization(ctx context.Context, tx *sql.Tx, deviceID string) (*ledgermodel.Authorization, error) {
	var authID, issuedAt, expiresAt string
	var grantedMsat, remainingMsat int64
	var status string

	row := tx.QueryRowContext(ctx, `
		SELECT authorization_id, granted_msat, remaining_msat, issued_at, expires_at, status
		FROM authorizations
		WHERE device_id = ? AND status = 'active' AND expires_at > datetime('now')
		ORDER BY created_at DESC
		LIMIT 1`,
		deviceID,
	)

	err := row.Scan(&authID, &grantedMsat, &remainingMsat, &issuedAt, &expiresAt, &status)
	if err != nil {
		return nil, err
	}

	return &ledgermodel.Authorization{
		DeviceId:        deviceID,
		AuthorizationId: authID,
		GrantedMsat:     grantedMsat,
		RemainingMsat:   remainingMsat,
		IssuedAt:        issuedAt,
		ExpiresAt:       expiresAt,
	}, nil
}
