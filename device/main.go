package main

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	_ "modernc.org/sqlite"

	// Import the generated proto package
	// Note: The path matches the go_package in the proto file + the gen/ output directory
	devicepb "github.com/robertodantas/lnpay/proto/gen/gen/iot/payperuse/edge/model/device"
)

// Device represents a registered IoT device
type Device struct {
	ID              string  `json:"id"`
	PublicKey       string  `json:"public_key"`
	Unit            string  `json:"unit"`           // e.g., "sheet", "m3"
	PricePerUnit    float64 `json:"price_per_unit"` // cost in sats per unit
	SecretKey       string  `json:"secret_key"`
	AggregationMode string  `json:"aggregation_mode"` // e.g., "per-unit", "time-window", "value-threshold"
}

// RegistryService manages the registered devices
type RegistryService struct {
	db *sql.DB
}

// NewRegistryService creates and initializes the SQLite database
func NewRegistryService(dbPath string) *RegistryService {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("failed to connect to SQLite: %v", err)
	}

	// Create the devices table with aggregation_mode support
	createTable := `
	CREATE TABLE IF NOT EXISTS devices (
		id TEXT PRIMARY KEY,
		public_key TEXT,
		unit TEXT,
		price_per_unit REAL,
		secret_key TEXT,
		aggregation_mode TEXT DEFAULT 'per-unit'
	);`

	if _, err := db.Exec(createTable); err != nil {
		log.Fatalf("failed to create table: %v", err)
	}

	return &RegistryService{db: db}
}

// EmitUsageRecord creates and serializes a DeviceUsageReportedEvent
// This demonstrates how to use the generated proto files to emit events
func EmitUsageRecord(deviceID, reportID, unit string, measure float64, strategy devicepb.UsageReportingStrategy) ([]byte, error) {
	// Create a UsageRecord
	usageRecord := &devicepb.UsageRecord{
		DeviceId: deviceID,
		ReportId: reportID,
		Strategy: strategy,
		Measure:  measure,
		Unit:     unit,
		// ISO-8601 timestamp
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Create the DeviceUsageReportedEvent
	event := &devicepb.DeviceUsageReportedEvent{
		Usage: usageRecord,
	}

	// Option 1: Serialize to protobuf binary format (recommended for gRPC/streaming)
	protoBytes, err := proto.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event: %w", err)
	}

	// Option 2: Serialize to JSON (useful for HTTP APIs, logging, etc.)
	jsonBytes, err := protojson.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event to JSON: %w", err)
	}

	// You can also create a DeviceEvent envelope if needed
	deviceEvent := &devicepb.DeviceEvent{
		Type: devicepb.DeviceEventType_DEVICE_EVENT_TYPE_USAGE_REPORTED,
		Payload: &devicepb.DeviceEvent_UsageReported{
			UsageReported: event,
		},
	}

	// Serialize the envelope
	envelopeBytes, err := proto.Marshal(deviceEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal envelope: %w", err)
	}

	// For demonstration, return JSON (you can return protoBytes or envelopeBytes)
	_ = protoBytes
	_ = envelopeBytes

	return jsonBytes, nil
}

// Example usage function
func ExampleEmitUsageRecord() {
	// Example: Emit a usage record with DELTA strategy
	jsonData, err := EmitUsageRecord(
		"device-123",
		"report-456", // unique report ID for idempotency
		"kWh",        // unit
		2.5,          // measure (amount consumed)
		devicepb.UsageReportingStrategy_USAGE_STRATEGY_DELTA,
	)
	if err != nil {
		log.Printf("Error emitting usage record: %v", err)
		return
	}

	// Print the JSON for demonstration
	var prettyJSON map[string]interface{}
	if err := json.Unmarshal(jsonData, &prettyJSON); err == nil {
		prettyBytes, _ := json.MarshalIndent(prettyJSON, "", "  ")
		fmt.Println("Emitted usage record event:")
		fmt.Println(string(prettyBytes))
	}
}

// MQTTClient wraps the MQTT client and connection logic
type MQTTClient struct {
	client mqtt.Client
	broker string
	port   int
}

// NewMQTTClient creates a new MQTT client with TLS support
func NewMQTTClient() (*MQTTClient, error) {
	broker := getEnv("MQTT_BROKER", "mosquitto")
	useTLS := getEnvBool("MQTT_USE_TLS", true) // Default to true for secure connections

	var port int
	var protocol string
	if useTLS {
		port = getEnvInt("MQTT_TLS_PORT", 8883)
		protocol = getEnv("MQTT_TLS_PROTOCOL", "tls")
	} else {
		port = getEnvInt("MQTT_PORT", 1883)
		protocol = "tcp"
	}

	brokerURL := fmt.Sprintf("%s://%s:%d", protocol, broker, port)
	clientID := getEnv("MQTT_CLIENT_ID", "device-service")

	log.Printf("Configuring MQTT client: broker=%s, protocol=%s, port=%d, useTLS=%v", broker, protocol, port, useTLS)

	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID(clientID)
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("MQTT connection lost: %v", err)
	})
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Println("MQTT OnConnect handler called")
	})

	// Set username/password if provided
	if username := getEnv("MQTT_USERNAME", ""); username != "" {
		opts.SetUsername(username)
		if password := getEnv("MQTT_PASSWORD", ""); password != "" {
			opts.SetPassword(password)
		}
	}

	// Configure TLS if enabled
	if useTLS {
		log.Println("Configuring TLS for MQTT connection...")
		tlsConfig, err := createTLSConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
		opts.SetTLSConfig(tlsConfig)
		log.Println("TLS configuration created successfully")
	}

	// Note: Protocol version defaults to MQTT 3.1.1 (version 4), which is what Mosquitto uses
	// No need to set it explicitly unless you need MQTT 3.1 (version 3) or MQTT 5.0 (when supported)

	client := mqtt.NewClient(opts)

	// Connect to the broker with timeout
	log.Printf("Attempting to connect to MQTT broker at %s...", brokerURL)
	token := client.Connect()

	// Wait for connection with timeout
	connected := token.WaitTimeout(30 * time.Second)
	if !connected {
		// Check for error even if timeout occurred
		if token.Error() != nil {
			errMsg := token.Error().Error()
			log.Printf("MQTT connection error (timeout): %s", errMsg)
			return nil, fmt.Errorf("connection timeout after 30 seconds: %w", token.Error())
		}
		return nil, fmt.Errorf("connection timeout after 30 seconds - broker may not be accepting TLS connections or certificate validation failed")
	}

	if token.Error() != nil {
		errMsg := token.Error().Error()
		log.Printf("MQTT connection error details: %s", errMsg)
		return nil, fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	log.Printf("Connected to MQTT broker at %s", brokerURL)

	return &MQTTClient{
		client: client,
		broker: broker,
		port:   port,
	}, nil
}

// createTLSConfig creates a TLS configuration from environment variables
func createTLSConfig() (*tls.Config, error) {
	// Check if we should skip certificate verification (for testing only)
	skipVerify := getEnvBool("MQTT_TLS_SKIP_VERIFY", false)

	broker := getEnv("MQTT_BROKER", "mosquitto")
	// Allow custom server name for certificate validation (useful when CN doesn't match hostname)
	serverName := getEnv("MQTT_TLS_SERVER_NAME", broker)

	tlsConfig := &tls.Config{
		InsecureSkipVerify: skipVerify,
		MinVersion:         tls.VersionTLS12,
		ServerName:         serverName, // Set server name for SNI and hostname verification
	}

	if serverName != broker {
		log.Printf("Using custom TLS server name: %s (broker hostname: %s)", serverName, broker)
	}

	if skipVerify {
		log.Println("WARNING: TLS certificate verification is disabled (for testing only)")
	}

	// Load CA certificate
	caCertPath := getEnv("MQTT_TLS_CA_CERT", "/certs/ca.crt")
	log.Printf("Loading CA certificate from: %s", caCertPath)
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}
	tlsConfig.RootCAs = caCertPool
	log.Println("CA certificate loaded successfully")

	// Note: When VerifyPeerCertificate is set, it overrides default verification
	// The RootCAs we set above is still used for chain building, but we do our own verification

	// Load edge node certificate and key if provided and required
	// Only send client certificate if explicitly required (require_certificate true in mosquitto.conf)
	// Since mosquitto.conf has require_certificate false, we don't need to send client cert
	requireEdgeCert := getEnvBool("MQTT_TLS_REQUIRE_EDGE_CERT", false)
	edgeCertPath := getEnv("MQTT_TLS_EDGE_CERT", "")
	edgeKeyPath := getEnv("MQTT_TLS_EDGE_KEY", "")

	if requireEdgeCert && edgeCertPath != "" && edgeKeyPath != "" {
		log.Printf("Loading edge node certificate from: %s and key from: %s", edgeCertPath, edgeKeyPath)
		cert, err := tls.LoadX509KeyPair(edgeCertPath, edgeKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load edge node certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		log.Println("Edge node certificate loaded for client authentication")
	} else {
		log.Println("No edge node certificate required - using CA-only server verification")
	}

	return tlsConfig, nil
}

// Publish publishes a message to the specified topic
func (m *MQTTClient) Publish(topic string, qos byte, retained bool, payload []byte) error {
	token := m.client.Publish(topic, qos, retained, payload)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to publish message: %w", token.Error())
	}
	log.Printf("Published message to topic: %s", topic)
	return nil
}

// Subscribe subscribes to a topic with a message handler
func (m *MQTTClient) Subscribe(topic string, qos byte, handler mqtt.MessageHandler) error {
	token := m.client.Subscribe(topic, qos, handler)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to topic: %w", token.Error())
	}
	log.Printf("Subscribed to topic: %s", topic)
	return nil
}

// Disconnect disconnects from the MQTT broker
func (m *MQTTClient) Disconnect() {
	m.client.Disconnect(250)
	log.Println("Disconnected from MQTT broker")
}

// Helper functions for environment variables
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func main() {
	log.Println("Starting device service...")

	_ = NewRegistryService("devices.db")
	log.Println("Registry service initialized")

	// Connect to MQTT broker
	log.Println("Connecting to MQTT broker...")
	mqttClient, err := NewMQTTClient()
	if err != nil {
		log.Fatalf("Failed to create MQTT client: %v", err)
	}
	defer mqttClient.Disconnect()
	log.Println("MQTT client connected successfully")

	// Example: Emit a usage record event
	jsonData, err := EmitUsageRecord(
		"device-123",
		"report-456",
		"kWh",
		2.5,
		devicepb.UsageReportingStrategy_USAGE_STRATEGY_DELTA,
	)
	if err != nil {
		log.Printf("Error emitting usage record: %v", err)
		return
	}

	// Publish usage record to MQTT
	deviceID := "device-123"
	topic := fmt.Sprintf("devices/%s/usage", deviceID)
	if err := mqttClient.Publish(topic, 1, false, jsonData); err != nil {
		log.Printf("Error publishing to MQTT: %v", err)
		return
	}

	log.Println("Usage record published successfully")
}
