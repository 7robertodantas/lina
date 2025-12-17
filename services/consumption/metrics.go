package main

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

var (
	// Metrics instruments
	consumptionEventsTotal metric.Int64Counter
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
	meter := provider.Meter("consumption-service")

	// Create counter for consumption events
	consumptionEventsTotal, err = meter.Int64Counter(
		"consumption_events_total",
		metric.WithDescription("Total number of consumption events published to Redis"),
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

// RecordConsumptionEventPublished records a consumption event published to Redis
func RecordConsumptionEventPublished(ctx context.Context, eventType string) {
	if consumptionEventsTotal == nil {
		return
	}

	attrs := []attribute.KeyValue{}
	if eventType != "" {
		attrs = append(attrs, attribute.String("event_type", eventType))
	}

	consumptionEventsTotal.Add(ctx, 1,
		metric.WithAttributes(attrs...),
	)
}


