package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	ledgermodel "github.com/robertodantas/lnpay/proto/gen/model/ledger"
	lightningmodel "github.com/robertodantas/lnpay/proto/gen/model/lightning"
	mqttpb "github.com/robertodantas/lnpay/proto/gen/model/mqtt"
)

// SouthboundInterface handles MQTT subscriptions for device messages
type SouthboundInterface struct {
	mqttClient      *MQTTClient
	streamClient    *StreamClient
	ledgerClient    *LedgerClient
	lightningClient *LightningClient
	repo            *DeviceRepository
	invoiceTimeout  time.Duration
}

// NewSouthboundInterface creates a new southbound interface
func NewSouthboundInterface(mqttClient *MQTTClient, streamClient *StreamClient, ledgerClient *LedgerClient, lightningClient *LightningClient, repo *DeviceRepository, invoiceTimeout time.Duration) *SouthboundInterface {
	if invoiceTimeout <= 0 {
		invoiceTimeout = 30 * time.Second
	}

	return &SouthboundInterface{
		mqttClient:      mqttClient,
		streamClient:    streamClient,
		ledgerClient:    ledgerClient,
		lightningClient: lightningClient,
		repo:            repo,
		invoiceTimeout:  invoiceTimeout,
	}
}

// Start initializes all MQTT subscriptions for the southbound interface
func (sb *SouthboundInterface) Start(ctx context.Context) error {
	// Subscribe to heartbeat topic: /devices/#/heartbeat
	if err := sb.mqttClient.Subscribe(ctx, "/devices/+/heartbeat", 1, sb.handleHeartbeat); err != nil {
		return fmt.Errorf("failed to subscribe to heartbeat topic: %w", err)
	}

	// Subscribe to usage topic: /devices/#/usage
	if err := sb.mqttClient.Subscribe(ctx, "/devices/+/usage", 1, sb.handleUsage); err != nil {
		return fmt.Errorf("failed to subscribe to usage topic: %w", err)
	}

	// Subscribe to authorization request topic: /devices/#/request/authorize
	if err := sb.mqttClient.Subscribe(ctx, "/devices/+/request/authorize", 1, sb.handleAuthorizationRequest); err != nil {
		return fmt.Errorf("failed to subscribe to authorization request topic: %w", err)
	}

	// Subscribe to invoice request topic: /devices/#/request/invoice
	if err := sb.mqttClient.Subscribe(ctx, "/devices/+/request/invoice", 1, sb.handleInvoiceRequest); err != nil {
		return fmt.Errorf("failed to subscribe to invoice request topic: %w", err)
	}

	logger.Info(ctx, "Southbound interface started - all subscriptions active on southbound mqtt")
	return nil
}

// extractDeviceID extracts the device ID from an MQTT topic path
// Topics are in format: /devices/{deviceId}/...
func extractDeviceID(topic string) string {
	parts := strings.Split(strings.TrimPrefix(topic, "/"), "/")
	if len(parts) >= 2 && parts[0] == "devices" {
		return parts[1]
	}
	return ""
}

// handleHeartbeat processes heartbeat messages from devices
func (sb *SouthboundInterface) handleHeartbeat(ctx context.Context, client mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	deviceID := extractDeviceID(topic)

	var payload mqttpb.HeartbeatPayload
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(msg.Payload(), &payload); err != nil {
		logger.WithDeviceID(deviceID).
			Error(ctx, "Error parsing heartbeat payload on southbound mqtt", err)
		return
	}

	logger.WithDeviceID(payload.GetDeviceId()).
		InfoWithFields(ctx, "Heartbeat received on southbound mqtt", map[string]interface{}{
			"status":    payload.GetStatus().String(),
			"timestamp": payload.GetTimestamp(),
		})
}

// handleUsage processes usage messages from devices
// This handler runs in a goroutine to avoid blocking the MQTT message handler
func (sb *SouthboundInterface) handleUsage(ctx context.Context, client mqtt.Client, msg mqtt.Message) {
	// Copy payload since we'll be processing in a goroutine and the original may be reused
	payload := make([]byte, len(msg.Payload()))
	copy(payload, msg.Payload())
	topic := msg.Topic()
	deviceID := extractDeviceID(topic)

	// Process in goroutine to avoid blocking MQTT message handler
	go func() {
		// Create new context for this goroutine (parent context may be cancelled)
		processCtx := context.Background()

		var usagePayload mqttpb.UsagePayload
		opts := protojson.UnmarshalOptions{DiscardUnknown: true}
		if err := opts.Unmarshal(payload, &usagePayload); err != nil {
			logger.WithDeviceID(deviceID).
				Error(processCtx, "Error parsing usage payload on southbound mqtt", err)
			return
		}

		logger.WithDeviceID(usagePayload.GetDeviceId()).
			InfoWithFields(processCtx, "Usage received on southbound mqtt", map[string]interface{}{
				"report_id": usagePayload.GetReportId(),
				"strategy":  usagePayload.GetStrategy().String(),
				"measure":   usagePayload.GetMeasure(),
				"unit":      usagePayload.GetUnit(),
				"timestamp": usagePayload.GetTimestamp(),
			})

		// Publish DeviceUsageReportedEvent to Redis stream (with price_per_unit from device config)
		if err := sb.streamClient.PublishDeviceUsageReportedEvent(processCtx, &usagePayload, sb.repo); err != nil {
			logger.WithDeviceID(usagePayload.GetDeviceId()).
				WithStream("event.device", "produce").
				Error(processCtx, "Error publishing usage event to Redis stream on southbound mqtt", err)
			return
		}
	}()
}

// mapLedgerStatusToMQTTStatus maps ledger AuthorizationStatus to MQTT AuthorizationStatus
func mapLedgerStatusToMQTTStatus(status ledgermodel.AuthorizationStatus) mqttpb.AuthorizationStatus {
	// Both enums have the same values, so we can convert directly
	return mqttpb.AuthorizationStatus(status)
}

// mapLightningStatusToMQTTStatus maps lightning InvoiceStatus to MQTT InvoiceStatus
func mapLightningStatusToMQTTStatus(status lightningmodel.InvoiceStatus) mqttpb.InvoiceStatus {
	return mqttpb.InvoiceStatus(status)
}

// handleAuthorizationRequest processes authorization requests from devices
// This handler runs in a goroutine to avoid blocking the MQTT message handler
func (sb *SouthboundInterface) handleAuthorizationRequest(ctx context.Context, client mqtt.Client, msg mqtt.Message) {
	// Copy payload since we'll be processing in a goroutine and the original may be reused
	payload := make([]byte, len(msg.Payload()))
	copy(payload, msg.Payload())
	topic := msg.Topic()
	deviceID := extractDeviceID(topic)

	// Process in goroutine to avoid blocking MQTT message handler
	go func() {
		// Create new context for this goroutine (parent context may be cancelled)
		processCtx := context.Background()

		// Log that we received a message on this topic
		logger.WithDeviceID(deviceID).
			InfoWithFields(processCtx, "Received message on authorization request topic on southbound mqtt", map[string]interface{}{
				"topic":        topic,
				"payload_size": len(payload),
				"payload":      string(payload),
			})

		var request mqttpb.AuthorizationRequestPayload
		opts := protojson.UnmarshalOptions{DiscardUnknown: true}
		if err := opts.Unmarshal(payload, &request); err != nil {
			logger.WithDeviceID(deviceID).
				WithFields(map[string]interface{}{
					"payload": string(payload),
				}).
				Error(processCtx, "Error parsing authorization request payload on southbound mqtt", err)
			return
		}

		logger.WithDeviceID(request.GetDeviceId()).
			InfoWithFields(processCtx, "Authorization request received on southbound mqtt", map[string]interface{}{
				"request_id":   request.GetRequestId(),
				"request_msat": request.GetRequestMsat(),
				"reason":       request.GetReason(),
				"timestamp":    request.GetTimestamp(),
			})

		// Call ledger service via gRPC
		grpcCtx, cancel := context.WithTimeout(processCtx, sb.invoiceTimeout)
		defer cancel()

		ledgerResp, err := sb.ledgerClient.CreateOrGetAuthorization(
			grpcCtx,
			request.GetDeviceId(),
			request.GetRequestId(),
			request.GetRequestMsat(),
			request.GetReason(),
		)
		if err != nil {
			logger.WithDeviceID(deviceID).
				Error(processCtx, "Error calling ledger service on southbound mqtt", err)

			// Send error response to device
			responseTopic := fmt.Sprintf("/devices/%s/response/authorize", deviceID)
			response := &mqttpb.AuthorizationResponsePayload{
				DeviceId:  request.GetDeviceId(),
				RequestId: request.GetRequestId(),
				Status:    mqttpb.AuthorizationStatus_AUTHORIZATION_STATUS_REJECTED,
				Reason:    fmt.Sprintf("Failed to process authorization: %v", err),
			}
			marshalOpts := protojson.MarshalOptions{UseProtoNames: true}
			responseJSON, marshalErr := marshalOpts.Marshal(response)
			if marshalErr != nil {
				logger.WithDeviceID(deviceID).
					Error(processCtx, "Error marshaling authorization error response on southbound mqtt", marshalErr)
				return
			}

			if err := sb.mqttClient.Publish(processCtx, responseTopic, 1, false, responseJSON); err != nil {
				logger.WithDeviceID(deviceID).
					Error(processCtx, "Error publishing error response to device on southbound mqtt", err)
			}
			// Publish STOP control command when authorization is rejected due to error
			if err := sb.publishControlCommand(processCtx, deviceID, mqttpb.ControlCommand_CONTROL_COMMAND_STOP, fmt.Sprintf("Failed to process authorization: %v", err)); err != nil {
				logger.WithDeviceID(deviceID).
					Error(processCtx, "Error publishing STOP control command after authorization error on southbound mqtt", err)
			}
			return
		}

		// Map ledger response to MQTT response payload
		response := &mqttpb.AuthorizationResponsePayload{
			DeviceId:  request.GetDeviceId(),
			RequestId: request.GetRequestId(),
			Status:    mapLedgerStatusToMQTTStatus(ledgerResp.GetStatus()),
			Reason:    ledgerResp.GetReason(),
		}

		// If authorization was granted or is active, include authorization details
		if ledgerResp.GetAuthorization() != nil {
			auth := ledgerResp.GetAuthorization()
			response.AuthorizationId = auth.GetAuthorizationId()
			response.GrantedMsat = auth.GetGrantedMsat()
			response.RemainingMsat = auth.GetRemainingMsat()
			response.IssuedAt = auth.GetIssuedAt()
			response.ExpiresAt = auth.GetExpiresAt()
		}

		// Include available balance if provided
		if ledgerResp.GetAvailableMsat() > 0 {
			response.AvailableMsat = ledgerResp.GetAvailableMsat()
		}

		// Serialize response to JSON with short enum names
		marshalOpts := protojson.MarshalOptions{UseProtoNames: true}
		responseJSON, err := marshalOpts.Marshal(response)
		if err != nil {
			logger.WithDeviceID(deviceID).
				Error(processCtx, "Error marshaling authorization response on southbound mqtt", err)
			return
		}

		// Publish response to /devices/{deviceId}/response/authorize
		responseTopic := fmt.Sprintf("/devices/%s/response/authorize", deviceID)
		if err := sb.mqttClient.Publish(processCtx, responseTopic, 1, false, responseJSON); err != nil {
			logger.WithDeviceID(deviceID).
				Error(processCtx, "Error publishing authorization response to device on southbound mqtt", err)
			return
		}

		logger.WithDeviceID(deviceID).
			InfoWithFields(processCtx, "Authorization response published on southbound mqtt", map[string]interface{}{
				"topic":  responseTopic,
				"status": response.Status.String(),
			})

		// If authorization was granted or is active, publish RESUME control command
		if response.Status == mqttpb.AuthorizationStatus_AUTHORIZATION_STATUS_GRANTED ||
			response.Status == mqttpb.AuthorizationStatus_AUTHORIZATION_STATUS_ACTIVE {
			reason := response.Reason
			if reason == "" {
				reason = "AUTHORIZATION_GRANTED"
			}
			if err := sb.publishControlCommand(processCtx, deviceID, mqttpb.ControlCommand_CONTROL_COMMAND_RESUME, reason); err != nil {
				logger.WithDeviceID(deviceID).
					Error(processCtx, "Error publishing RESUME control command after authorization grant on southbound mqtt", err)
			}
		}

		// If authorization was rejected, publish STOP control command to halt the device
		if response.Status == mqttpb.AuthorizationStatus_AUTHORIZATION_STATUS_REJECTED {
			reason := response.Reason
			if reason == "" {
				reason = "AUTHORIZATION_REJECTED"
			}
			if err := sb.publishControlCommand(processCtx, deviceID, mqttpb.ControlCommand_CONTROL_COMMAND_STOP, reason); err != nil {
				logger.WithDeviceID(deviceID).
					Error(processCtx, "Error publishing STOP control command after authorization rejection on southbound mqtt", err)
			}
		}
	}()
}

// publishControlCommand publishes a control command to the device
func (sb *SouthboundInterface) publishControlCommand(ctx context.Context, deviceID string, command mqttpb.ControlCommand, reason string) error {
	if deviceID == "" {
		return fmt.Errorf("device ID is required")
	}

	payload := &mqttpb.ControlPayload{
		Command: command,
		Reason:  reason,
	}

	marshalOpts := protojson.MarshalOptions{UseProtoNames: true}
	msgBytes, err := marshalOpts.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal control payload: %w", err)
	}

	topic := fmt.Sprintf("/devices/%s/control", deviceID)
	if err := sb.mqttClient.Publish(ctx, topic, 1, false, msgBytes); err != nil {
		return fmt.Errorf("failed to publish control command to MQTT: %w", err)
	}

	logger.WithDeviceID(deviceID).
		InfoWithFields(ctx, "Published control command on southbound mqtt", map[string]interface{}{
			"command": command.String(),
			"reason":  reason,
		})
	return nil
}

// handleInvoiceRequest processes invoice requests from devices
// This handler runs in a goroutine to avoid blocking the MQTT message handler
func (sb *SouthboundInterface) handleInvoiceRequest(ctx context.Context, client mqtt.Client, msg mqtt.Message) {
	// Copy payload since we'll be processing in a goroutine and the original may be reused
	payload := make([]byte, len(msg.Payload()))
	copy(payload, msg.Payload())
	topic := msg.Topic()
	deviceID := extractDeviceID(topic)

	// Process in goroutine to avoid blocking MQTT message handler
	go func() {
		// Create new context for this goroutine (parent context may be cancelled)
		processCtx := context.Background()

		var request mqttpb.InvoiceRequestPayload
		opts := protojson.UnmarshalOptions{DiscardUnknown: true}
		if err := opts.Unmarshal(payload, &request); err != nil {
			logger.WithDeviceID(deviceID).
				Error(processCtx, "Error parsing invoice request payload on southbound mqtt", err)
			return
		}

		logger.WithDeviceID(request.GetDeviceId()).
			InfoWithFields(processCtx, "Invoice request received on southbound mqtt", map[string]interface{}{
				"request_id":  request.GetRequestId(),
				"amount_msat": request.GetAmountMsat(),
				"reason":      request.GetReason(),
				"timestamp":   request.GetTimestamp(),
			})

		if sb.lightningClient == nil {
			logger.WithDeviceID(deviceID).
				Warn(processCtx, "Lightning client not initialized on southbound mqtt; cannot process invoice request")
			return
		}

		grpcCtx, cancel := context.WithTimeout(processCtx, sb.invoiceTimeout)
		defer cancel()

		lightningResp, err := sb.lightningClient.CreateInvoice(
			grpcCtx,
			request.GetDeviceId(),
			request.GetAmountMsat(),
			request.GetReason(),
		)
		if err != nil {
			logger.WithDeviceID(deviceID).
				Error(processCtx, "Error calling lightning service on southbound mqtt", err)

			// Send error response
			responseTopic := fmt.Sprintf("/devices/%s/response/invoice", deviceID)
			response := &mqttpb.InvoiceResponsePayload{
				DeviceId:  request.GetDeviceId(),
				RequestId: request.GetRequestId(),
				Status:    mqttpb.InvoiceStatus_INVOICE_STATUS_FAILED,
			}
			marshalOpts := protojson.MarshalOptions{UseProtoNames: true}
			responseJSON, marshalErr := marshalOpts.Marshal(response)
			if marshalErr != nil {
				logger.WithDeviceID(deviceID).
					Error(processCtx, "Error marshaling invoice error response on southbound mqtt", marshalErr)
				return
			}

			if err := sb.mqttClient.Publish(processCtx, responseTopic, 1, false, responseJSON); err != nil {
				logger.WithDeviceID(deviceID).
					Error(processCtx, "Error publishing invoice error response to device on southbound mqtt", err)
			}
			return
		}

		invoice := lightningResp.GetInvoice()
		if invoice == nil {
			logger.WithDeviceID(deviceID).
				Warn(processCtx, "Lightning service returned empty invoice on southbound mqtt")
			return
		}

		response := &mqttpb.InvoiceResponsePayload{
			DeviceId:   request.GetDeviceId(),
			RequestId:  request.GetRequestId(),
			Status:     mapLightningStatusToMQTTStatus(invoice.GetStatus()),
			InvoiceId:  invoice.GetInvoiceId(),
			Bolt11:     invoice.GetBolt11(),
			AmountMsat: invoice.GetAmountMsat(),
			ExpiresAt:  invoice.GetExpiresAt(),
		}

		// Serialize response to JSON with short enum names
		marshalOpts := protojson.MarshalOptions{UseProtoNames: true}
		responseJSON, err := marshalOpts.Marshal(response)
		if err != nil {
			logger.WithDeviceID(deviceID).
				Error(processCtx, "Error marshaling invoice response on southbound mqtt", err)
			return
		}

		// Publish response to /devices/{deviceId}/response/invoice
		responseTopic := fmt.Sprintf("/devices/%s/response/invoice", deviceID)
		if err := sb.mqttClient.Publish(processCtx, responseTopic, 1, false, responseJSON); err != nil {
			logger.WithDeviceID(deviceID).
				Error(processCtx, "Error publishing invoice response to device on southbound mqtt", err)
			return
		}

		logger.WithDeviceID(deviceID).
			InfoWithFields(processCtx, "Invoice response published on southbound mqtt", map[string]interface{}{
				"topic":      responseTopic,
				"invoice_id": response.InvoiceId,
				"status":     response.Status.String(),
			})
	}()
}
