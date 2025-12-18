package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

var (
	// Metrics instruments
	mqttMessagesReceivedTotal  metric.Int64Counter
	mqttMessagesProcessedTotal metric.Int64Counter
	mqttMessagesFailedTotal    metric.Int64Counter
)

// initMetrics initializes Prometheus metrics using OpenTelemetry
func initMetrics() error {
	// Create Prometheus exporter
	exporter, err := prometheus.New()
	if err != nil {
		return err
	}

	// Create meter provider with Prometheus exporter
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
	)

	// Set as global meter provider
	otel.SetMeterProvider(provider)

	// Get meter
	meter := provider.Meter("device-service")

	// Create counters for MQTT messages
	mqttMessagesReceivedTotal, err = meter.Int64Counter(
		"device_mqtt_messages_received_total",
		metric.WithDescription("Total number of MQTT messages received by the device service"),
	)
	if err != nil {
		return err
	}

	mqttMessagesProcessedTotal, err = meter.Int64Counter(
		"device_mqtt_messages_processed_total",
		metric.WithDescription("Total number of MQTT messages successfully processed by the device service"),
	)
	if err != nil {
		return err
	}

	mqttMessagesFailedTotal, err = meter.Int64Counter(
		"device_mqtt_messages_failed_total",
		metric.WithDescription("Total number of MQTT messages that failed processing in the device service"),
	)
	if err != nil {
		return err
	}

	return nil
}

// GetMetricsHandler returns the HTTP handler for the /metrics endpoint
// The OTel prometheus exporter automatically collects metrics from the meter provider
// and exposes them via the default Prometheus registry
func GetMetricsHandler() http.Handler {
	// The exporter automatically collects metrics from the meter provider
	// We just need to expose them via HTTP using the default Prometheus handler
	// The exporter's internal mechanism will handle metric collection
	return promhttp.Handler()
}

// mqttMetricAttributes normalizes the topic by replacing the concrete device ID
// with a "{deviceId}" placeholder (when provided), and uses only the normalized
// topic as the metric label to avoid high-cardinality metrics.
func mqttMetricAttributes(topic, deviceID string) []attribute.KeyValue {
	normalizedTopic := topic
	if deviceID != "" {
		// Replace all occurrences of the concrete device ID in the topic with a placeholder.
		normalizedTopic = strings.ReplaceAll(topic, deviceID, "{deviceId}")
	}

	return []attribute.KeyValue{
		attribute.String("topic", normalizedTopic),
	}
}

// RecordMQTTMessageReceived records that an MQTT message was received
func RecordMQTTMessageReceived(ctx context.Context, topic, deviceID string) {
	if mqttMessagesReceivedTotal == nil {
		return
	}
	mqttMessagesReceivedTotal.Add(ctx, 1,
		metric.WithAttributes(mqttMetricAttributes(topic, deviceID)...),
	)
}

// RecordMQTTMessageProcessed records that an MQTT message was successfully processed
func RecordMQTTMessageProcessed(ctx context.Context, topic, deviceID string) {
	if mqttMessagesProcessedTotal == nil {
		return
	}
	mqttMessagesProcessedTotal.Add(ctx, 1,
		metric.WithAttributes(mqttMetricAttributes(topic, deviceID)...),
	)
}

// RecordMQTTMessageFailed records that processing an MQTT message failed
func RecordMQTTMessageFailed(ctx context.Context, topic, deviceID string) {
	if mqttMessagesFailedTotal == nil {
		return
	}
	mqttMessagesFailedTotal.Add(ctx, 1,
		metric.WithAttributes(mqttMetricAttributes(topic, deviceID)...),
	)
}
