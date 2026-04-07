package internal

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// MQTTConnectionSpec describes broker connection parameters. Zero durations and unset
// booleans are replaced with defaults matching the device-service southbound client.
type MQTTConnectionSpec struct {
	ClientID string
	Username string
	Password string
	UseTLS   bool
	Broker   string
	Port     int
	Protocol string // e.g. "tcp", "tls", "ssl"

	ConnectTimeout       time.Duration
	KeepAlive            time.Duration
	ConnectRetryInterval time.Duration
	PingTimeout          time.Duration

	CleanSession        bool
	AutoReconnect       bool
	ConnectRetry        bool
	DisableConnectRetry bool // if true, overrides default ConnectRetry=true
}

func (s *MQTTConnectionSpec) connectTimeout() time.Duration {
	if s.ConnectTimeout > 0 {
		return s.ConnectTimeout
	}
	return 30 * time.Second
}

func (s *MQTTConnectionSpec) keepAlive() time.Duration {
	if s.KeepAlive > 0 {
		return s.KeepAlive
	}
	return 60 * time.Second
}

func (s *MQTTConnectionSpec) connectRetryInterval() time.Duration {
	if s.ConnectRetryInterval > 0 {
		return s.ConnectRetryInterval
	}
	return 5 * time.Second
}

func (s *MQTTConnectionSpec) pingTimeout() time.Duration {
	if s.PingTimeout > 0 {
		return s.PingTimeout
	}
	return 10 * time.Second
}

func (s *MQTTConnectionSpec) cleanSession() bool {
	return true
}

func (s *MQTTConnectionSpec) autoReconnect() bool {
	return true
}

func (s *MQTTConnectionSpec) connectRetry() bool {
	if s.DisableConnectRetry {
		return false
	}
	return true
}

// MQTTTLSParams holds TLS material for MQTT over TLS. BrokerHost is used when
// ServerName is empty (SNI and hostname verification).
type MQTTTLSParams struct {
	BrokerHost string

	SkipVerify bool
	ServerName string

	CACertPath string

	RequireEdgeCert bool
	EdgeCertPath    string
	EdgeKeyPath     string
}

// CreateMQTTTLSConfig builds a tls.Config for MQTT. It applies NanoMQ compatibility.
func CreateMQTTTLSConfig(p *MQTTTLSParams) (*tls.Config, error) {
	if p == nil {
		return nil, fmt.Errorf("TLS params are nil")
	}

	serverName := p.ServerName
	if serverName == "" {
		serverName = p.BrokerHost
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: p.SkipVerify,
		ServerName:         serverName,
	}
	ApplyNanomqMQTTTLSCompat(tlsConfig)

	caCert, err := os.ReadFile(p.CACertPath)
	if err != nil {
		return nil, fmt.Errorf("read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("parse CA certificate")
	}
	tlsConfig.RootCAs = caCertPool

	if p.RequireEdgeCert && p.EdgeCertPath != "" && p.EdgeKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(p.EdgeCertPath, p.EdgeKeyPath)
		if err != nil {
			return nil, fmt.Errorf("load edge certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// MQTTSessionHooks are optional Paho connection callbacks.
type MQTTSessionHooks struct {
	OnConnectionLost func(client mqtt.Client, err error)
	OnConnect        func(client mqtt.Client)
}

// MQTTConnectConfig is everything needed to dial the broker once.
type MQTTConnectConfig struct {
	Connection MQTTConnectionSpec
	TLS        *MQTTTLSParams // required when Connection.UseTLS
	Hooks      *MQTTSessionHooks
}

// MQTTTracing enables OpenTelemetry spans around publish, subscribe, and receive.
// When Enabled is false, spans are not created regardless of tracer fields.
type MQTTTracing struct {
	Enabled       bool
	PublishTracer trace.Tracer
	ReceiveTracer trace.Tracer
}

// MQTTReceiveHooks are optional callbacks after a message is received or processed
// (e.g. Prometheus counters). DeviceID is derived via ExtractDeviceIDFromDevicesTopic.
type MQTTReceiveHooks struct {
	OnReceived  func(ctx context.Context, topic, deviceID string)
	OnProcessed func(ctx context.Context, topic, deviceID string)
	OnFailed    func(ctx context.Context, topic, deviceID string)
}

// MQTTClientBehavior configures optional tracing, metrics hooks, timeouts, and logging hooks.
type MQTTClientBehavior struct {
	Tracing MQTTTracing
	Receive *MQTTReceiveHooks

	// PublishAckTimeout is how long to wait for QoS 1/2 acknowledgment. Zero defaults to 30s.
	PublishAckTimeout time.Duration

	OnPublishSuccess   func(ctx context.Context, topic string)
	OnSubscribeSuccess func(ctx context.Context, topic string, qos byte)
	OnDisconnect       func()
}

// MQTTMessageHandlerWithContext handles an incoming message with context and reports errors.
type MQTTMessageHandlerWithContext func(ctx context.Context, client mqtt.Client, msg mqtt.Message) error

// MQTTClient wraps a connected Paho client with publish/subscribe helpers.
type MQTTClient struct {
	client mqtt.Client
	broker string
	port   int

	tracing MQTTTracing
	receive *MQTTReceiveHooks

	publishAckTimeout time.Duration

	onPublishSuccess   func(ctx context.Context, topic string)
	onSubscribeSuccess func(ctx context.Context, topic string, qos byte)
	onDisconnect       func()
}

// DialMQTT connects to the broker and returns the raw Paho client (no tracing, no publish helpers).
// Prefer ConnectMQTT when you need Publish/Subscribe with optional OpenTelemetry.
func DialMQTT(dial MQTTConnectConfig) (mqtt.Client, error) {
	c, _, _, err := connectPahoClient(dial)
	return c, err
}

// ConnectMQTT establishes a single MQTT session and returns a wrapped client.
func ConnectMQTT(dial MQTTConnectConfig, behavior *MQTTClientBehavior) (*MQTTClient, error) {
	raw, broker, port, err := connectPahoClient(dial)
	if err != nil {
		return nil, err
	}

	b := MQTTClientBehavior{}
	if behavior != nil {
		b = *behavior
	}
	pubWait := b.PublishAckTimeout
	if pubWait <= 0 {
		pubWait = 30 * time.Second
	}

	return &MQTTClient{
		client:             raw,
		broker:             broker,
		port:               port,
		tracing:            b.Tracing,
		receive:            b.Receive,
		publishAckTimeout:  pubWait,
		onPublishSuccess:   b.OnPublishSuccess,
		onSubscribeSuccess: b.OnSubscribeSuccess,
		onDisconnect:       b.OnDisconnect,
	}, nil
}

func connectPahoClient(dial MQTTConnectConfig) (mqtt.Client, string, int, error) {
	spec := dial.Connection
	opts, err := buildMQTTClientOptions(&spec, dial.TLS, dial.Hooks)
	if err != nil {
		return nil, "", 0, err
	}

	client := mqtt.NewClient(opts)
	token := client.Connect()
	timeout := spec.connectTimeout()
	connected := token.WaitTimeout(timeout)
	if !connected {
		if token.Error() != nil {
			return nil, "", 0, fmt.Errorf("connection timeout after %v: %w", timeout, token.Error())
		}
		return nil, "", 0, fmt.Errorf("connection timeout after %v - broker may not be accepting connections or certificate validation failed", timeout)
	}
	if token.Error() != nil {
		return nil, "", 0, fmt.Errorf("connect to MQTT broker: %w", token.Error())
	}

	return client, spec.Broker, spec.Port, nil
}

func buildMQTTClientOptions(spec *MQTTConnectionSpec, tlsParams *MQTTTLSParams, hooks *MQTTSessionHooks) (*mqtt.ClientOptions, error) {
	brokerURL := fmt.Sprintf("%s://%s:%d", spec.Protocol, spec.Broker, spec.Port)

	mqttOpts := mqtt.NewClientOptions()
	mqttOpts.AddBroker(brokerURL)
	mqttOpts.SetClientID(spec.ClientID)
	mqttOpts.SetCleanSession(spec.cleanSession())
	mqttOpts.SetAutoReconnect(spec.autoReconnect())
	if spec.connectRetry() {
		mqttOpts.SetConnectRetry(true)
		mqttOpts.SetConnectRetryInterval(spec.connectRetryInterval())
	} else {
		mqttOpts.SetConnectRetry(false)
	}
	mqttOpts.SetKeepAlive(spec.keepAlive())
	mqttOpts.SetPingTimeout(spec.pingTimeout())

	if hooks != nil {
		if hooks.OnConnectionLost != nil {
			mqttOpts.SetConnectionLostHandler(hooks.OnConnectionLost)
		}
		if hooks.OnConnect != nil {
			mqttOpts.SetOnConnectHandler(hooks.OnConnect)
		}
	}

	if spec.Username != "" {
		mqttOpts.SetUsername(spec.Username)
		if spec.Password != "" {
			mqttOpts.SetPassword(spec.Password)
		}
	}

	if spec.UseTLS {
		if tlsParams == nil {
			return nil, fmt.Errorf("UseTLS is true but TLS params are nil")
		}
		tlsConfig, err := CreateMQTTTLSConfig(tlsParams)
		if err != nil {
			return nil, fmt.Errorf("TLS config: %w", err)
		}
		mqttOpts.SetTLSConfig(tlsConfig)
	}

	return mqttOpts, nil
}

// ExtractDeviceIDFromDevicesTopic parses /devices/{id}/... topics.
func ExtractDeviceIDFromDevicesTopic(topic string) string {
	parts := strings.Split(strings.TrimPrefix(topic, "/"), "/")
	if len(parts) >= 2 && parts[0] == "devices" {
		return parts[1]
	}
	return ""
}

// Publish sends a message and waits for broker acknowledgment (QoS 1/2) up to PublishAckTimeout.
func (m *MQTTClient) Publish(ctx context.Context, topic string, qos byte, retained bool, payload []byte) error {
	var span trace.Span
	if m.tracing.Enabled && m.tracing.PublishTracer != nil {
		spanName := fmt.Sprintf("[mqtt] %s publish", topic)
		var s trace.Span
		ctx, s = m.tracing.PublishTracer.Start(ctx, spanName,
			trace.WithAttributes(
				attribute.String("mqtt.topic", topic),
				attribute.Int("mqtt.qos", int(qos)),
				attribute.Bool("mqtt.retained", retained),
				attribute.Int("mqtt.payload.size", len(payload)),
				attribute.String("mqtt.operation", "PUBLISH"),
			),
		)
		span = s
	}
	if span != nil {
		defer span.End()
	}

	if !m.client.IsConnected() {
		err := fmt.Errorf("MQTT client is not connected")
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "client not connected")
		}
		return err
	}

	token := m.client.Publish(topic, qos, retained, payload)
	if !token.WaitTimeout(m.publishAckTimeout) {
		err := fmt.Errorf("timeout waiting for publish acknowledgment")
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "publish timeout")
		}
		return err
	}
	if token.Error() != nil {
		if span != nil {
			span.RecordError(token.Error())
			span.SetStatus(codes.Error, token.Error().Error())
		}
		return fmt.Errorf("publish message: %w", token.Error())
	}
	if qos >= 1 && !m.client.IsConnected() {
		err := fmt.Errorf("client disconnected after publish - broker may have denied the publish")
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "client disconnected after publish")
		}
		return err
	}

	if m.onPublishSuccess != nil {
		m.onPublishSuccess(ctx, topic)
	}
	if span != nil {
		span.SetStatus(codes.Ok, "success")
	}
	return nil
}

func (m *MQTTClient) wrapHandlerWithTracing(handler MQTTMessageHandlerWithContext) mqtt.MessageHandler {
	return func(client mqtt.Client, msg mqtt.Message) {
		topic := msg.Topic()
		deviceID := ExtractDeviceIDFromDevicesTopic(topic)

		var span trace.Span
		ctx := context.Background()
		if m.tracing.Enabled && m.tracing.ReceiveTracer != nil {
			var s trace.Span
			ctx, s = m.tracing.ReceiveTracer.Start(ctx, fmt.Sprintf("[mqtt] %s received", topic),
				trace.WithAttributes(
					attribute.String("mqtt.topic", topic),
					attribute.String("mqtt.device_id", deviceID),
					attribute.Int("mqtt.payload.size", len(msg.Payload())),
					attribute.String("mqtt.operation", "RECEIVE"),
				),
			)
			span = s
		}

		if m.receive != nil && m.receive.OnReceived != nil {
			m.receive.OnReceived(ctx, topic, deviceID)
		}

		go func() {
			if span != nil {
				defer span.End()
			}
			if err := handler(ctx, client, msg); err != nil {
				if span != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				}
				if m.receive != nil && m.receive.OnFailed != nil {
					m.receive.OnFailed(ctx, topic, deviceID)
				}
				return
			}
			if m.receive != nil && m.receive.OnProcessed != nil {
				m.receive.OnProcessed(ctx, topic, deviceID)
			}
			if span != nil {
				span.SetStatus(codes.Ok, "processed")
			}
		}()
	}
}

// Subscribe registers a context-aware handler (runs in a goroutine; does not block Paho).
func (m *MQTTClient) Subscribe(ctx context.Context, topic string, qos byte, handler MQTTMessageHandlerWithContext) error {
	var span trace.Span
	if m.tracing.Enabled && m.tracing.PublishTracer != nil {
		spanName := fmt.Sprintf("[mqtt] %s subscribe", topic)
		var s trace.Span
		ctx, s = m.tracing.PublishTracer.Start(ctx, spanName,
			trace.WithAttributes(
				attribute.String("mqtt.topic", topic),
				attribute.Int("mqtt.qos", int(qos)),
				attribute.String("mqtt.operation", "SUBSCRIBE"),
			),
		)
		span = s
	}
	if span != nil {
		defer span.End()
	}

	wrapped := m.wrapHandlerWithTracing(handler)
	token := m.client.Subscribe(topic, qos, wrapped)
	if token.Wait() && token.Error() != nil {
		if span != nil {
			span.RecordError(token.Error())
			span.SetStatus(codes.Error, token.Error().Error())
		}
		return fmt.Errorf("subscribe to topic: %w", token.Error())
	}

	if m.onSubscribeSuccess != nil {
		m.onSubscribeSuccess(ctx, topic, qos)
	}
	if span != nil {
		span.SetStatus(codes.Ok, "success")
	}
	return nil
}

// Disconnect ends the MQTT session (250ms quiesce, matching prior device-service behavior).
func (m *MQTTClient) Disconnect() {
	m.client.Disconnect(250)
	if m.onDisconnect != nil {
		m.onDisconnect()
	}
}

// GetClient returns the underlying Paho client for advanced use (e.g. custom subscribe patterns).
func (m *MQTTClient) GetClient() mqtt.Client {
	return m.client
}
