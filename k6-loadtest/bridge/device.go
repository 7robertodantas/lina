package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	mqttmodel "github.com/robertodantas/lnpay/services/proto/gen/model/mqtt"
	"google.golang.org/protobuf/encoding/protojson"
)

// DeviceContext manages the lifecycle and state of a device
type DeviceContext struct {
	mu sync.RWMutex

	// Device identity
	DeviceID string
	Secret   string
	Client   mqtt.Client

	// State
	AvailableMsat                 int64
	HasActiveAuthorization        bool
	AuthorizationID               string
	AuthorizationExpiresAt        *time.Time
	PendingAuthorization          bool
	CurrentAuthorizationRequestID string // Store request_id for idempotent retries
	AuthorizationRetryCount       int    // Track retry attempts with same request_id
	PendingInvoice                bool
	Initialized                   bool
	ReportingEnabled              bool // Controls whether usage reports should be sent
	InitComplete                  chan bool

	// Configuration
	AuthorizeRequestMsat int64
	InvoiceAmountMsat    int64

	// Timestamps
	LastInvoiceRequest       time.Time
	LastAuthorizationRequest time.Time
}

// NewDeviceContext creates a new device context
func NewDeviceContext(deviceID, secret string, client mqtt.Client) *DeviceContext {
	return &DeviceContext{
		DeviceID:                 deviceID,
		Secret:                   secret,
		Client:                   client,
		AuthorizeRequestMsat:     10000,  // Default 10k msat
		InvoiceAmountMsat:        250000, // Default 250k msat
		InitComplete:             make(chan bool, 1),
		ReportingEnabled:         true,                              // Start with reporting enabled
		LastInvoiceRequest:       time.Now().Add(-10 * time.Minute), // Allow immediate request
		LastAuthorizationRequest: time.Now().Add(-10 * time.Minute),
	}
}

// Proto type aliases (same as smartmeter/core)
type BalanceMessage = mqttmodel.BalancePayload
type AuthorizeResponse = mqttmodel.AuthorizationResponsePayload
type InvoiceResponse = mqttmodel.InvoiceResponsePayload
type InvoiceEvent = mqttmodel.InvoiceEventPayload
type ControlMessage = mqttmodel.ControlPayload

// Proto marshal/unmarshal options (same as smartmeter/core)
var (
	protoMarshalOpts   = protojson.MarshalOptions{UseProtoNames: true}
	protoUnmarshalOpts = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// SubscribeToTopics subscribes to all necessary topics and sets up handlers
func (d *DeviceContext) SubscribeToTopics() error {
	deviceID := d.DeviceID
	topics := map[string]byte{
		fmt.Sprintf("/devices/%s/balance", deviceID):            1,
		fmt.Sprintf("/devices/%s/control", deviceID):            1,
		fmt.Sprintf("/devices/%s/response/authorize", deviceID): 1,
		fmt.Sprintf("/devices/%s/response/invoice", deviceID):   1,
		fmt.Sprintf("/devices/%s/events/invoice", deviceID):     1,
	}

	// Set up message handlers
	messageHandler := func(client mqtt.Client, msg mqtt.Message) {
		topic := msg.Topic()
		payload := msg.Payload()

		switch topic {
		case fmt.Sprintf("/devices/%s/balance", deviceID):
			d.handleBalanceMessage(payload)
		case fmt.Sprintf("/devices/%s/control", deviceID):
			d.handleControlMessage(payload)
		case fmt.Sprintf("/devices/%s/response/authorize", deviceID):
			d.handleAuthorizeResponse(payload)
		case fmt.Sprintf("/devices/%s/response/invoice", deviceID):
			d.handleInvoiceResponse(payload)
		case fmt.Sprintf("/devices/%s/events/invoice", deviceID):
			d.handleInvoiceEvent(payload)
		}
	}

	// Subscribe with handlers
	for topic, qos := range topics {
		token := d.Client.Subscribe(topic, qos, messageHandler)
		if !token.WaitTimeout(5 * time.Second) {
			return fmt.Errorf("timeout subscribing to %s", topic)
		}
		if token.Error() != nil {
			return fmt.Errorf("error subscribing to %s: %v", topic, token.Error())
		}
		log.Printf("[%s] Subscribed to %s", deviceID, topic)
	}

	// Small delay to ensure subscriptions are ready
	time.Sleep(100 * time.Millisecond)
	return nil
}

// Initialize ensures the device has funds and authorization before returning
func (d *DeviceContext) Initialize() error {
	log.Printf("[%s] Starting initialization", d.DeviceID)

	// Wait briefly for initial balance message (if any) - reduced from 2s to 500ms
	time.Sleep(100 * time.Millisecond)

	// Step 1: Check if we need invoice and request it if needed
	d.mu.RLock()
	needsInvoice := d.AvailableMsat < d.InvoiceAmountMsat
	currentBalance := d.AvailableMsat
	d.mu.RUnlock()

	// Request invoice and authorization in parallel for speed
	// Authorization will be rejected if no funds, but will succeed once invoice settles
	invoiceRequested := false
	if needsInvoice {
		log.Printf("[%s] Requesting invoice (current balance: %d msat)", d.DeviceID, currentBalance)
		if err := d.RequestInvoice("INITIALIZATION"); err != nil {
			return fmt.Errorf("failed to request invoice: %v", err)
		}
		invoiceRequested = true
	}

	// Request authorization immediately (in parallel with invoice)
	// If no funds, it will be rejected, but invoice handler will retry
	log.Printf("[%s] Requesting authorization", d.DeviceID)
	_ = d.RequestAuthorization("INITIALIZATION") // Ignore errors, will retry if needed

	// Wait for both invoice settlement (if requested) and authorization
	// Use a combined timeout and check both conditions
	// Increased timeout to handle MQTT publish delays
	timeout := time.After(90 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	lastRetryTime := time.Now()
	retryInterval := 2 * time.Second // Retry authorization every 2 seconds if we have balance but no auth

	for {
		select {
		case <-timeout:
			d.mu.RLock()
			hasAuth := d.HasActiveAuthorization
			hasBalance := d.AvailableMsat >= d.AuthorizeRequestMsat
			pendingInvoice := d.PendingInvoice
			d.mu.RUnlock()

			if !hasAuth {
				if !hasBalance && pendingInvoice {
					return fmt.Errorf("timeout waiting for invoice settlement and authorization")
				} else if !hasBalance {
					return fmt.Errorf("timeout waiting for authorization (insufficient balance)")
				} else {
					return fmt.Errorf("timeout waiting for authorization (have balance: %d msat)", d.AvailableMsat)
				}
			}
			// Has auth, proceed
			log.Printf("[%s] Authorization received (timeout check)", d.DeviceID)
			d.mu.Lock()
			d.Initialized = true
			d.mu.Unlock()
			log.Printf("[%s] Initialization complete", d.DeviceID)
			return nil

		case <-ticker.C:
			d.mu.RLock()
			hasAuth := d.HasActiveAuthorization
			hasBalance := d.AvailableMsat >= d.AuthorizeRequestMsat
			pendingInvoice := d.PendingInvoice
			d.mu.RUnlock()

			// If we requested invoice, wait for it to settle
			if invoiceRequested && pendingInvoice && !hasBalance {
				continue // Still waiting for invoice
			}

			// Check if we have authorization
			if hasAuth {
				log.Printf("[%s] Authorization received", d.DeviceID)
				d.mu.Lock()
				d.Initialized = true
				d.mu.Unlock()
				log.Printf("[%s] Initialization complete", d.DeviceID)
				return nil
			}

			// If we have balance but no auth, keep requesting authorization
			// Backend is idempotent, so multiple requests are safe
			if hasBalance && time.Since(lastRetryTime) >= retryInterval {
				log.Printf("[%s] Have balance (%d msat) but no authorization, requesting authorization", d.DeviceID, d.AvailableMsat)
				if err := d.RequestAuthorization("INITIALIZATION_RETRY"); err != nil {
					log.Printf("[%s] Authorization request failed: %v", d.DeviceID, err)
				}
				lastRetryTime = time.Now()
			}

			// Continue waiting
		}
	}
}

// RequestInvoice sends an invoice request
func (d *DeviceContext) RequestInvoice(reason string) error {
	d.mu.Lock()
	// Rate limiting: don't request too frequently
	if time.Since(d.LastInvoiceRequest) < 5*time.Second {
		d.mu.Unlock()
		return fmt.Errorf("invoice request rate limited")
	}
	d.PendingInvoice = true
	d.LastInvoiceRequest = time.Now()
	d.mu.Unlock()

	requestID := generateRequestID()
	payload := map[string]interface{}{
		"device_id":   d.DeviceID,
		"request_id":  requestID,
		"amount_msat": d.InvoiceAmountMsat,
		"reason":      reason,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	payloadJSON, _ := json.Marshal(payload)
	topic := fmt.Sprintf("/devices/%s/request/invoice", d.DeviceID)

	token := d.Client.Publish(topic, 1, false, payloadJSON)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("timeout publishing invoice request")
	}
	if token.Error() != nil {
		return token.Error()
	}

	log.Printf("[%s] Invoice request sent: %s", d.DeviceID, requestID)
	return nil
}

// RequestAuthorization sends an authorization request
// Backend is idempotent, so multiple requests with same request_id are safe
// After 3 retries with the same request_id, generate a new one
func (d *DeviceContext) RequestAuthorization(reason string) error {
	d.mu.Lock()
	// Check if we should generate a new request_id (after 3 retries)
	requestID := d.CurrentAuthorizationRequestID
	if requestID == "" {
		// First request - generate new request_id
		requestID = generateRequestID()
		d.CurrentAuthorizationRequestID = requestID
		d.AuthorizationRetryCount = 0
	} else if d.AuthorizationRetryCount >= 3 {
		// After 3 retries, generate new request_id
		log.Printf("[%s] Generating new request_id after 3 retries (old: %s)", d.DeviceID, requestID)
		requestID = generateRequestID()
		d.CurrentAuthorizationRequestID = requestID
		d.AuthorizationRetryCount = 0
	} else {
		// Increment retry count when reusing existing request_id
		d.AuthorizationRetryCount++
	}
	d.PendingAuthorization = true
	d.LastAuthorizationRequest = time.Now()
	d.mu.Unlock()

	payload := map[string]interface{}{
		"device_id":    d.DeviceID,
		"request_id":   requestID,
		"request_msat": d.AuthorizeRequestMsat,
		"reason":       reason,
		"timestamp":    time.Now().Format(time.RFC3339),
	}

	payloadJSON, _ := json.Marshal(payload)
	topic := fmt.Sprintf("/devices/%s/request/authorize", d.DeviceID)

	token := d.Client.Publish(topic, 1, false, payloadJSON)
	if !token.WaitTimeout(5 * time.Second) {
		d.mu.Lock()
		d.PendingAuthorization = false
		d.mu.Unlock()
		return fmt.Errorf("timeout publishing authorization request")
	}
	if token.Error() != nil {
		d.mu.Lock()
		d.PendingAuthorization = false
		d.mu.Unlock()
		return token.Error()
	}

	log.Printf("[%s] Authorization request sent: %s", d.DeviceID, requestID)
	return nil
}

// EnsureAuthorizationActive checks and maintains active authorization
func (d *DeviceContext) EnsureAuthorizationActive() {
	d.mu.RLock()
	hasAuth := d.HasActiveAuthorization
	expiresAt := d.AuthorizationExpiresAt
	pending := d.PendingAuthorization
	d.mu.RUnlock()

	// Check if authorization is expired
	if expiresAt != nil && time.Now().After(*expiresAt) {
		d.mu.Lock()
		d.HasActiveAuthorization = false
		d.mu.Unlock()
		hasAuth = false
	}

	// Request authorization if needed
	if !hasAuth && !pending {
		if expiresAt != nil && time.Now().After(*expiresAt) {
			log.Printf("[%s] Authorization expired, requesting new one", d.DeviceID)
		} else {
			log.Printf("[%s] No active authorization, requesting one", d.DeviceID)
		}
		if err := d.RequestAuthorization("MAINTAIN_ACTIVE"); err != nil {
			log.Printf("[%s] Failed to request authorization: %v", d.DeviceID, err)
		}
	} else if !hasAuth && pending {
		// Log when we skip requesting because one is already pending
		if expiresAt != nil && time.Now().After(*expiresAt) {
			log.Printf("[%s] Authorization expired, but request already pending", d.DeviceID)
		}
	}
}

// Message handlers
func (d *DeviceContext) handleBalanceMessage(payload []byte) {
	var msg BalanceMessage
	if err := protoUnmarshalOpts.Unmarshal(payload, &msg); err != nil {
		log.Printf("[%s] Failed to parse balance message: %v", d.DeviceID, err)
		return
	}

	d.mu.Lock()
	d.AvailableMsat = msg.AvailableMsat
	shouldRequestAuth := msg.AvailableMsat > 0 && !d.PendingAuthorization && !d.HasActiveAuthorization
	d.mu.Unlock()

	log.Printf("[%s] Balance updated: %d msat available", d.DeviceID, msg.AvailableMsat)

	if shouldRequestAuth {
		log.Printf("[%s] Funds available, requesting authorization", d.DeviceID)
		d.RequestAuthorization("FUNDS_AVAILABLE")
	}
}

func (d *DeviceContext) handleAuthorizeResponse(payload []byte) {
	var msg AuthorizeResponse
	if err := protoUnmarshalOpts.Unmarshal(payload, &msg); err != nil {
		log.Printf("[%s] Failed to parse authorization response: %v", d.DeviceID, err)
		return
	}

	d.mu.Lock()
	d.PendingAuthorization = false
	d.CurrentAuthorizationRequestID = "" // Clear request_id after response
	d.AuthorizationRetryCount = 0        // Reset retry count after response
	d.mu.Unlock()

	log.Printf("[%s] Authorization response: %s", d.DeviceID, msg.Status.String())

	switch msg.Status {
	case mqttmodel.AuthorizationStatus_AUTHORIZATION_STATUS_GRANTED,
		mqttmodel.AuthorizationStatus_AUTHORIZATION_STATUS_ACTIVE:
		expiresAt, _ := time.Parse(time.RFC3339, msg.ExpiresAt)
		d.mu.Lock()
		d.HasActiveAuthorization = true
		d.AuthorizationID = msg.AuthorizationId
		d.AuthorizationExpiresAt = &expiresAt
		d.mu.Unlock()

		// Signal initialization complete if waiting
		select {
		case d.InitComplete <- true:
		default:
		}

		log.Printf("[%s] Authorization active: %s (expires: %s)", d.DeviceID, msg.AuthorizationId, msg.ExpiresAt)

	case mqttmodel.AuthorizationStatus_AUTHORIZATION_STATUS_REJECTED:
		d.mu.Lock()
		d.HasActiveAuthorization = false
		d.mu.Unlock()

		log.Printf("[%s] Authorization rejected: %s", d.DeviceID, msg.Reason)

		// If rejected due to insufficient funds, request invoice
		if msg.AvailableMsat < d.AuthorizeRequestMsat {
			log.Printf("[%s] Insufficient funds, requesting invoice", d.DeviceID)
			d.RequestInvoice("AUTHORIZATION_REJECTED_NEED_FUNDS")
		}
	}
}

func (d *DeviceContext) handleInvoiceResponse(payload []byte) {
	var msg InvoiceResponse
	if err := protoUnmarshalOpts.Unmarshal(payload, &msg); err != nil {
		log.Printf("[%s] Failed to parse invoice response: %v", d.DeviceID, err)
		return
	}

	log.Printf("[%s] Invoice response: %s", d.DeviceID, msg.Status.String())

	switch msg.Status {
	case mqttmodel.InvoiceStatus_INVOICE_STATUS_CREATED:
		log.Printf("[%s] Invoice created: %s (waiting for payment)", d.DeviceID, msg.InvoiceId)
		// Keep pendingInvoice true until settled

	case mqttmodel.InvoiceStatus_INVOICE_STATUS_SETTLED:
		d.mu.Lock()
		d.PendingInvoice = false
		d.mu.Unlock()
		log.Printf("[%s] Invoice settled: %s", d.DeviceID, msg.InvoiceId)
		// After settlement, request authorization if we don't have one and none is pending
		d.mu.RLock()
		hasAuth := d.HasActiveAuthorization
		pendingAuth := d.PendingAuthorization
		d.mu.RUnlock()
		if !hasAuth && !pendingAuth {
			d.RequestAuthorization("INVOICE_SETTLED")
		}

	case mqttmodel.InvoiceStatus_INVOICE_STATUS_EXPIRED,
		mqttmodel.InvoiceStatus_INVOICE_STATUS_FAILED:
		d.mu.Lock()
		d.PendingInvoice = false
		d.mu.Unlock()
		log.Printf("[%s] Invoice %s: %s", d.DeviceID, msg.Status.String(), msg.InvoiceId)
	}
}

func (d *DeviceContext) handleInvoiceEvent(payload []byte) {
	var event InvoiceEvent
	if err := protoUnmarshalOpts.Unmarshal(payload, &event); err != nil {
		log.Printf("[%s] Failed to parse invoice event: %v", d.DeviceID, err)
		return
	}

	log.Printf("[%s] Invoice event: %s for invoice %s", d.DeviceID, event.Status.String(), event.InvoiceId)

	switch event.Status {
	case mqttmodel.InvoiceStatus_INVOICE_STATUS_SETTLED:
		d.mu.Lock()
		d.PendingInvoice = false
		d.mu.Unlock()
		log.Printf("[%s] Invoice settled via event: %s (%d msats received)", d.DeviceID, event.InvoiceId, event.AmountReceivedMsat)
		// After settlement, request authorization if we don't have one and none is pending
		d.mu.RLock()
		hasAuth := d.HasActiveAuthorization
		pendingAuth := d.PendingAuthorization
		d.mu.RUnlock()
		if !hasAuth && !pendingAuth {
			d.RequestAuthorization("INVOICE_SETTLED")
		}

	case mqttmodel.InvoiceStatus_INVOICE_STATUS_EXPIRED:
		d.mu.Lock()
		d.PendingInvoice = false
		d.mu.Unlock()
		log.Printf("[%s] Invoice expired via event: %s", d.DeviceID, event.InvoiceId)

	default:
		log.Printf("[%s] Unhandled invoice event status: %s for invoice %s", d.DeviceID, event.Status.String(), event.InvoiceId)
	}
}

func (d *DeviceContext) handleControlMessage(payload []byte) {
	var msg ControlMessage
	if err := protoUnmarshalOpts.Unmarshal(payload, &msg); err != nil {
		log.Printf("[%s] Failed to parse control message: %v", d.DeviceID, err)
		return
	}

	log.Printf("[%s] Control command: %s", d.DeviceID, msg.Command.String())

	switch msg.Command {
	case mqttmodel.ControlCommand_CONTROL_COMMAND_STOP,
		mqttmodel.ControlCommand_CONTROL_COMMAND_PAUSE:
		d.mu.Lock()
		d.ReportingEnabled = false
		d.mu.Unlock()
		log.Printf("[%s] Received STOP/PAUSE command - reporting disabled", d.DeviceID)

	case mqttmodel.ControlCommand_CONTROL_COMMAND_RESUME:
		d.mu.Lock()
		d.ReportingEnabled = true
		d.mu.Unlock()
		// Ensure authorization is active when resuming
		d.EnsureAuthorizationActive()
		log.Printf("[%s] Received RESUME command - reporting enabled", d.DeviceID)

	case mqttmodel.ControlCommand_CONTROL_COMMAND_AUTHORIZATION:
		reason := msg.Reason
		if reason == "" {
			reason = "AUTHORIZATION_REQUIRED"
		}
		log.Printf("[%s] Received AUTHORIZATION command (reason: %s)", d.DeviceID, reason)
		// Request new authorization
		if err := d.RequestAuthorization(reason); err != nil {
			log.Printf("[%s] Failed to request authorization: %v", d.DeviceID, err)
		}

	default:
		log.Printf("[%s] Unknown control command: %s", d.DeviceID, msg.Command.String())
	}
}

// Helper functions (removed - using polling in Initialize instead)

func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}
