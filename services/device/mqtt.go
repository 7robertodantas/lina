package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTClient wraps the MQTT client and connection logic
type MQTTClient struct {
	client mqtt.Client
	broker string
	port   int
}

// MQTTConnectionOptions holds options for MQTT connection
type MQTTConnectionOptions struct {
	ClientID  string
	Username  string
	Password  string
	UseTLS    bool
	Broker    string
	Port      int
	Protocol  string
	Timeout   time.Duration
	KeepAlive time.Duration
}

// createTLSConfig creates a TLS configuration from config
func createTLSConfig(cfg Config) (*tls.Config, error) {
	// Check if we should skip certificate verification (for testing only)
	skipVerify := cfg.MQTTTLSSkipVerify

	broker := cfg.MQTTBroker
	// Allow custom server name for certificate validation (useful when CN doesn't match hostname)
	serverName := cfg.MQTTTLSServerName
	if serverName == "" {
		serverName = broker
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: skipVerify,
		MinVersion:         tls.VersionTLS12,
		ServerName:         serverName, // Set server name for SNI and hostname verification
	}

	if serverName != broker {
		logger.InfoWithFields("Using custom TLS server name on southbound mqtt", map[string]interface{}{
			"server_name": serverName,
			"broker":      broker,
		})
	}

	if skipVerify {
		logger.Warn("TLS certificate verification is disabled on southbound mqtt (for testing only)")
	}

	// Load CA certificate
	caCertPath := cfg.MQTTTLSCACert
	logger.Infof("Loading CA certificate from %s on southbound mqtt", caCertPath)
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}
	tlsConfig.RootCAs = caCertPool
	logger.Info("CA certificate loaded successfully on southbound mqtt")

	// Load edge node certificate and key if provided and required
	if cfg.MQTTTLSRequireEdgeCert && cfg.MQTTTLSEdgeCert != "" && cfg.MQTTTLSEdgeKey != "" {
		logger.InfoWithFields("Loading edge node certificate on southbound mqtt", map[string]interface{}{
			"cert_path": cfg.MQTTTLSEdgeCert,
			"key_path":  cfg.MQTTTLSEdgeKey,
		})
		cert, err := tls.LoadX509KeyPair(cfg.MQTTTLSEdgeCert, cfg.MQTTTLSEdgeKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load edge node certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		logger.Info("Edge node certificate loaded for client authentication on southbound mqtt")
	} else {
		logger.Info("No edge node certificate required on southbound mqtt - using CA-only server verification")
	}

	return tlsConfig, nil
}

// buildMQTTOptions creates MQTT client options from connection options
func buildMQTTOptions(opts *MQTTConnectionOptions, cfg Config) (*mqtt.ClientOptions, error) {
	brokerURL := fmt.Sprintf("%s://%s:%d", opts.Protocol, opts.Broker, opts.Port)

	mqttOpts := mqtt.NewClientOptions()
	mqttOpts.AddBroker(brokerURL)
	mqttOpts.SetClientID(opts.ClientID)
	mqttOpts.SetCleanSession(true)
	mqttOpts.SetAutoReconnect(true)
	mqttOpts.SetConnectRetry(true)
	mqttOpts.SetConnectRetryInterval(5 * time.Second)
	mqttOpts.SetKeepAlive(opts.KeepAlive)
	mqttOpts.SetPingTimeout(10 * time.Second)
	mqttOpts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		logger.Error("MQTT connection lost on southbound mqtt", err)
	})
	mqttOpts.SetOnConnectHandler(func(client mqtt.Client) {
		logger.Info("MQTT OnConnect handler called on southbound mqtt")
	})

	// Set username/password if provided
	if opts.Username != "" {
		mqttOpts.SetUsername(opts.Username)
		if opts.Password != "" {
			mqttOpts.SetPassword(opts.Password)
		}
	}

	// Configure TLS if enabled
	if opts.UseTLS {
		logger.Info("Configuring TLS for MQTT connection on southbound mqtt")
		tlsConfig, err := createTLSConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
		mqttOpts.SetTLSConfig(tlsConfig)
		logger.Info("TLS configuration created successfully on southbound mqtt")
	}

	return mqttOpts, nil
}

// ConnectMQTT connects to MQTT broker with the given options and returns the client
func ConnectMQTT(opts *MQTTConnectionOptions, cfg Config) (mqtt.Client, error) {
	mqttOpts, err := buildMQTTOptions(opts, cfg)
	if err != nil {
		return nil, err
	}

	brokerURL := fmt.Sprintf("%s://%s:%d", opts.Protocol, opts.Broker, opts.Port)
	logger.Infof("Attempting to connect to MQTT broker at %s on southbound mqtt", brokerURL)

	client := mqtt.NewClient(mqttOpts)
	token := client.Connect()

	// Wait for connection with timeout
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	connected := token.WaitTimeout(timeout)
	if !connected {
		if token.Error() != nil {
			errMsg := token.Error().Error()
			logger.Errorf("MQTT connection error (timeout) on southbound mqtt: %s", errMsg)
			return nil, fmt.Errorf("connection timeout after %v: %w", timeout, token.Error())
		}
		return nil, fmt.Errorf("connection timeout after %v - broker may not be accepting connections or certificate validation failed", timeout)
	}

	if token.Error() != nil {
		errMsg := token.Error().Error()
		logger.Errorf("MQTT connection error details on southbound mqtt: %s", errMsg)
		return nil, fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	logger.Infof("Connected to MQTT broker at %s on southbound mqtt", brokerURL)
	return client, nil
}

// NewMQTTClient creates a new MQTT client with TLS support using config
func NewMQTTClient(cfg Config) (*MQTTClient, error) {
	broker := cfg.MQTTBroker
	useTLS := cfg.MQTTUseTLS

	var port int
	var protocol string
	if useTLS {
		port = cfg.MQTTTLSPort
		protocol = cfg.MQTTTLSProtocol
	} else {
		port = cfg.MQTTPort
		protocol = "tcp"
	}

	clientID := cfg.MQTTClientID
	username := cfg.MQTTUsername
	password := cfg.MQTTPassword

	opts := &MQTTConnectionOptions{
		ClientID:  clientID,
		Username:  username,
		Password:  password,
		UseTLS:    useTLS,
		Broker:    broker,
		Port:      port,
		Protocol:  protocol,
		Timeout:   30 * time.Second,
		KeepAlive: 60 * time.Second,
	}

	client, err := ConnectMQTT(opts, cfg)
	if err != nil {
		return nil, err
	}

	return &MQTTClient{
		client: client,
		broker: broker,
		port:   port,
	}, nil
}

// Publish publishes a message to the specified topic
func (m *MQTTClient) Publish(topic string, qos byte, retained bool, payload []byte) error {
	// Check if client is connected before attempting to publish
	if !m.client.IsConnected() {
		return fmt.Errorf("MQTT client is not connected")
	}

	token := m.client.Publish(topic, qos, retained, payload)

	// Wait for publish to complete with timeout (important for QoS 1/2 to get PUBACK/PUBREC)
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("timeout waiting for publish acknowledgment")
	}

	// Check for errors after waiting
	if token.Error() != nil {
		return fmt.Errorf("failed to publish message: %w", token.Error())
	}

	// For QoS 1, verify client is still connected (broker might disconnect on denial)
	if qos >= 1 && !m.client.IsConnected() {
		return fmt.Errorf("client disconnected after publish - broker may have denied the publish")
	}

	logger.InfoWithFields("Published message on southbound mqtt", map[string]interface{}{
		"topic": topic,
	})
	return nil
}

// Subscribe subscribes to a topic with a message handler
func (m *MQTTClient) Subscribe(topic string, qos byte, handler mqtt.MessageHandler) error {
	token := m.client.Subscribe(topic, qos, handler)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to topic: %w", token.Error())
	}
	logger.InfoWithFields("Subscribed to topic on southbound mqtt", map[string]interface{}{
		"topic": topic,
		"qos":   qos,
	})
	return nil
}

// Disconnect disconnects from the MQTT broker
func (m *MQTTClient) Disconnect() {
	m.client.Disconnect(250)
	logger.Info("Disconnected from MQTT broker on southbound mqtt")
}

// GetClient returns the underlying MQTT client
func (m *MQTTClient) GetClient() mqtt.Client {
	return m.client
}
