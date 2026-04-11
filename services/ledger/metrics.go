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
	authorizationsTotal     metric.Int64Counter
	entriesTotal            metric.Int64Counter
	debitLatencySeconds     metric.Float64Histogram
	txCommitSeconds         metric.Float64Histogram
	streamHandlerSeconds    metric.Float64Histogram
	streamMessageAgeSeconds metric.Float64Histogram
	streamAckSeconds        metric.Float64Histogram
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
	meter := provider.Meter("ledger-service")

	// Create counter for authorizations
	authorizationsTotal, err = meter.Int64Counter(
		"ledger_authorizations_total",
		metric.WithDescription("Total number of authorization operations"),
	)
	if err != nil {
		return err
	}

	// Create counter for entries
	entriesTotal, err = meter.Int64Counter(
		"ledger_entries_total",
		metric.WithDescription("Total number of ledger entries"),
	)
	if err != nil {
		return err
	}

	// Create histogram for debit latency with explicit bucket boundaries
	// Optimized for sub-second latencies (typical range: 0.001s - 1s) with higher buckets for outliers
	// Buckets: 0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0, 60.0, 120.0, 300.0, 600.0, +Inf
	buckets := []float64{
		0.001, // 1ms
		0.002, // 2ms
		0.005, // 5ms
		0.01,  // 10ms
		0.02,  // 20ms
		0.05,  // 50ms
		0.1,   // 100ms
		0.2,   // 200ms
		0.5,   // 500ms
		1.0,   // 1s
		2.0,   // 2s
		5.0,   // 5s
		10.0,  // 10s
		30.0,  // 30s
		60.0,  // 1m
		120.0, // 2m
		300.0, // 5m
		600.0, // 10m
		// +Inf is automatically added
	}
	debitLatencySeconds, err = meter.Float64Histogram(
		"ledger_debit_latency_seconds",
		metric.WithDescription("Latency of debit operations from usage in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(buckets...),
	)
	if err != nil {
		return err
	}

	// Create histogram for tx commit latency (seconds)
	txCommitSeconds, err = meter.Float64Histogram(
		"ledger_tx_commit_seconds",
		metric.WithDescription("Ledger transaction commit duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.0005, 0.001, 0.002, 0.005, 0.01,
			0.02, 0.05, 0.1, 0.2, 0.5,
			1.0, 2.0, 5.0, 10.0,
		),
	)
	if err != nil {
		return err
	}

	streamHandlerSeconds, err = meter.Float64Histogram(
		"ledger_stream_handler_seconds",
		metric.WithDescription("End-to-end stream message handling duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.0005, 0.001, 0.002, 0.005, 0.01,
			0.02, 0.05, 0.1, 0.2, 0.5,
			1.0, 2.0, 5.0, 10.0, 30.0, 60.0,
		),
	)
	if err != nil {
		return err
	}

	streamMessageAgeSeconds, err = meter.Float64Histogram(
		"ledger_stream_message_age_seconds",
		metric.WithDescription("Age in seconds of a stream message when processing starts"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.001, 0.01, 0.05, 0.1, 0.2,
			0.5, 1.0, 2.0, 5.0, 10.0,
			30.0, 60.0, 120.0, 300.0, 600.0,
		),
	)
	if err != nil {
		return err
	}

	streamAckSeconds, err = meter.Float64Histogram(
		"ledger_stream_ack_seconds",
		metric.WithDescription("ACK call duration in seconds for stream messages"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.0005, 0.001, 0.002, 0.005, 0.01,
			0.02, 0.05, 0.1, 0.2, 0.5,
			1.0, 2.0, 5.0,
		),
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

// RecordAuthorizationCreated records an authorization created event
func RecordAuthorizationCreated(ctx context.Context) {
	authorizationsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("operation", "created"),
		),
	)
}

// RecordAuthorizationDebited records an authorization debited event
func RecordAuthorizationDebited(ctx context.Context) {
	authorizationsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("operation", "debited"),
		),
	)
}

// RecordAuthorizationExpired records an authorization expired event
func RecordAuthorizationExpired(ctx context.Context) {
	authorizationsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("operation", "expired"),
		),
	)
}

// RecordAuthorizationDebitFailed records a consumption debit that could not be applied (e.g. no active authorization).
func RecordAuthorizationDebitFailed(ctx context.Context) {
	authorizationsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("operation", "debit_failed"),
		),
	)
}

// RecordEntry records a ledger entry with type and source
func RecordEntry(ctx context.Context, entryType, source string) {
	entriesTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("entry_type", entryType),
			attribute.String("source", source),
		),
	)
}

// RecordDebitLatency records the latency for a debit operation from usage
func RecordDebitLatency(ctx context.Context, latencySeconds float64) {
	debitLatencySeconds.Record(ctx, latencySeconds,
		metric.WithAttributes(
			attribute.String("source", "usage"),
		),
	)
}

// RecordTxCommitLatency records transaction commit latency by operation and outcome.
func RecordTxCommitLatency(ctx context.Context, operation string, latencySeconds float64, success bool) {
	outcome := "error"
	if success {
		outcome = "ok"
	}
	txCommitSeconds.Record(ctx, latencySeconds,
		metric.WithAttributes(
			attribute.String("operation", operation),
			attribute.String("outcome", outcome),
		),
	)
}

func streamModeLabel(pendingRetry bool) string {
	if pendingRetry {
		return "retry"
	}
	return "main"
}

// RecordStreamHandlerLatency records end-to-end stream handler latency (includes ACK path).
func RecordStreamHandlerLatency(ctx context.Context, streamName, operation string, latencySeconds float64, success bool, pendingRetry bool) {
	outcome := "error"
	if success {
		outcome = "ok"
	}
	streamHandlerSeconds.Record(ctx, latencySeconds,
		metric.WithAttributes(
			attribute.String("stream", streamName),
			attribute.String("operation", operation),
			attribute.String("mode", streamModeLabel(pendingRetry)),
			attribute.String("outcome", outcome),
		),
	)
}

// RecordStreamMessageAge records queue age when a stream message starts processing.
func RecordStreamMessageAge(ctx context.Context, streamName, operation string, ageSeconds float64, pendingRetry bool) {
	streamMessageAgeSeconds.Record(ctx, ageSeconds,
		metric.WithAttributes(
			attribute.String("stream", streamName),
			attribute.String("operation", operation),
			attribute.String("mode", streamModeLabel(pendingRetry)),
		),
	)
}

// RecordStreamAckLatency records ACK call duration.
func RecordStreamAckLatency(ctx context.Context, streamName, operation string, latencySeconds float64, success bool, pendingRetry bool) {
	outcome := "error"
	if success {
		outcome = "ok"
	}
	streamAckSeconds.Record(ctx, latencySeconds,
		metric.WithAttributes(
			attribute.String("stream", streamName),
			attribute.String("operation", operation),
			attribute.String("mode", streamModeLabel(pendingRetry)),
			attribute.String("outcome", outcome),
		),
	)
}
