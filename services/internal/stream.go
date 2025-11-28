package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	streamTracer = otel.Tracer("redis.stream")
)

// StreamClient wraps the Redis client for stream operations
type StreamClient struct {
	client *redis.Client
	ctx    context.Context
}

// StreamConfig holds configuration for creating a StreamClient
type StreamConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

// NewStreamClient creates a new Redis stream client with the provided configuration
func NewStreamClient(ctx context.Context, config StreamConfig) (*StreamClient, error) {
	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)
	// Note: This is in the internal package, so we can't use the logger here
	// as it would create a circular dependency. We'll use a simple log for now.
	// In production, you might want to pass a logger interface.
	_ = addr
	_ = config.DB

	opts := &redis.Options{
		Addr:     addr,
		Password: config.Password,
		DB:       config.DB,
	}

	client := redis.NewClient(opts)

	// Add OpenTelemetry metrics instrumentation
	// Note: Tracing is handled by our custom span wrappers (XReadGroupWithSpan, XAddWithSpan, etc.)
	// so we don't need InstrumentTracing which would create duplicate generic spans
	if err := redisotel.InstrumentMetrics(client); err != nil {
		return nil, fmt.Errorf("failed to instrument Redis with OpenTelemetry metrics: %w", err)
	}

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &StreamClient{
		client: client,
		ctx:    ctx,
	}, nil
}

// NewStreamClientFromEnv creates a new Redis stream client reading configuration from environment variables
func NewStreamClientFromEnv(ctx context.Context) (*StreamClient, error) {
	host := getEnv("REDIS_HOST", "redis")
	port := getEnv("REDIS_PORT", "6379")
	password := getEnv("REDIS_PASSWORD", "")
	dbStr := getEnv("REDIS_DB", "0")

	db, err := strconv.Atoi(dbStr)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_DB value: %w", err)
	}

	config := StreamConfig{
		Host:     host,
		Port:     port,
		Password: password,
		DB:       db,
	}

	return NewStreamClient(ctx, config)
}

// Client returns the underlying Redis client (useful for advanced operations)
func (sc *StreamClient) Client() *redis.Client {
	return sc.client
}

// Context returns the context used by the stream client
func (sc *StreamClient) Context() context.Context {
	return sc.ctx
}

// Close closes the Redis client connection
func (sc *StreamClient) Close() error {
	if err := sc.client.Close(); err != nil {
		return fmt.Errorf("failed to close Redis client: %w", err)
	}
	// Note: Logging removed to avoid circular dependency with logger package
	return nil
}

// XReadGroupWithSpan performs XReadGroup with a meaningful OpenTelemetry span
func (sc *StreamClient) XReadGroupWithSpan(ctx context.Context, streamName, groupName, consumerName string, args *redis.XReadGroupArgs) ([]redis.XStream, error) {
	spanName := fmt.Sprintf("[stream] %s read", streamName)
	ctx, span := streamTracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("redis.stream.name", streamName),
			attribute.String("redis.stream.group", groupName),
			attribute.String("redis.stream.consumer", consumerName),
			attribute.String("redis.operation", "XREADGROUP"),
		),
	)
	defer span.End()

	client := sc.Client()
	result := client.XReadGroup(ctx, args)
	if result.Err() != nil {
		// redis.Nil is not an error - it means no messages available
		if result.Err() == redis.Nil {
			span.SetAttributes(attribute.Bool("redis.stream.empty", true))
			span.SetStatus(codes.Ok, "no messages")
			return nil, redis.Nil
		}
		span.RecordError(result.Err())
		span.SetStatus(codes.Error, result.Err().Error())
		return nil, result.Err()
	}

	streams, err := result.Result()
	if err != nil {
		// redis.Nil is not an error - it means no messages available
		if err == redis.Nil {
			span.SetAttributes(attribute.Bool("redis.stream.empty", true))
			span.SetStatus(codes.Ok, "no messages")
			return nil, redis.Nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.Int("redis.stream.messages.count", len(streams)))
	span.SetStatus(codes.Ok, "success")
	return streams, nil
}

// XAddWithSpan performs XAdd with a meaningful OpenTelemetry span
// eventType is optional and will be included in the span name if provided (e.g., "AUTHORIZATION_DEBITED")
func (sc *StreamClient) XAddWithSpan(ctx context.Context, streamName string, args *redis.XAddArgs, eventType ...string) (string, error) {
	spanName := fmt.Sprintf("[stream] %s publish", streamName)
	if len(eventType) > 0 && eventType[0] != "" {
		spanName = fmt.Sprintf("[stream] %s publish [%s]", streamName, eventType[0])
	}
	ctx, span := streamTracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("redis.stream.name", streamName),
			attribute.String("redis.operation", "XADD"),
		),
	)
	if len(eventType) > 0 && eventType[0] != "" {
		span.SetAttributes(attribute.String("redis.event.type", eventType[0]))
	}
	defer span.End()

	client := sc.Client()
	result := client.XAdd(ctx, args)
	if result.Err() != nil {
		span.RecordError(result.Err())
		span.SetStatus(codes.Error, result.Err().Error())
		return "", result.Err()
	}

	streamID := result.Val()
	span.SetAttributes(attribute.String("redis.stream.id", streamID))
	span.SetStatus(codes.Ok, "success")
	return streamID, nil
}

// XAckWithSpan performs XAck with a meaningful OpenTelemetry span
// If msg is provided, the event type will be extracted from it and included in the span name
// Alternatively, eventType can be passed directly as a variadic argument
func (sc *StreamClient) XAckWithSpan(ctx context.Context, streamName, groupName, messageID string, msg *redis.XMessage, eventType ...string) error {
	// Extract event type from message if provided
	var finalEventType string
	if msg != nil {
		finalEventType = extractEventTypeFromMessage(*msg)
	}
	// Override with explicit eventType if provided
	if len(eventType) > 0 && eventType[0] != "" {
		finalEventType = eventType[0]
	}
	spanName := fmt.Sprintf("[stream] %s ack", streamName)
	if finalEventType != "" {
		spanName = fmt.Sprintf("[stream] %s ack [%s]", streamName, finalEventType)
	}
	ctx, span := streamTracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("redis.stream.name", streamName),
			attribute.String("redis.stream.group", groupName),
			attribute.String("redis.stream.message.id", messageID),
			attribute.String("redis.operation", "XACK"),
		),
	)
	if finalEventType != "" {
		span.SetAttributes(attribute.String("redis.event.type", finalEventType))
	}
	defer span.End()

	client := sc.Client()
	result := client.XAck(ctx, streamName, groupName, messageID)
	if result.Err() != nil {
		span.RecordError(result.Err())
		span.SetStatus(codes.Error, result.Err().Error())
		return result.Err()
	}

	span.SetStatus(codes.Ok, "success")
	return nil
}

// XReadWithSpan performs XRead with a meaningful OpenTelemetry span
func (sc *StreamClient) XReadWithSpan(ctx context.Context, streamName string, args *redis.XReadArgs) ([]redis.XStream, error) {
	spanName := fmt.Sprintf("[stream] %s read", streamName)
	ctx, span := streamTracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("redis.stream.name", streamName),
			attribute.String("redis.operation", "XREAD"),
		),
	)
	defer span.End()

	client := sc.Client()
	result := client.XRead(ctx, args)
	if result.Err() != nil {
		// redis.Nil is not an error - it means no messages available
		if result.Err() == redis.Nil {
			span.SetAttributes(attribute.Bool("redis.stream.empty", true))
			span.SetStatus(codes.Ok, "no messages")
			return nil, redis.Nil
		}
		span.RecordError(result.Err())
		span.SetStatus(codes.Error, result.Err().Error())
		return nil, result.Err()
	}

	streams, err := result.Result()
	if err != nil {
		// redis.Nil is not an error - it means no messages available
		if err == redis.Nil {
			span.SetAttributes(attribute.Bool("redis.stream.empty", true))
			span.SetStatus(codes.Ok, "no messages")
			return nil, redis.Nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(attribute.Int("redis.stream.messages.count", len(streams)))
	span.SetStatus(codes.Ok, "success")
	return streams, nil
}

// XGroupCreateMkStreamWithSpan performs XGroupCreateMkStream with a meaningful OpenTelemetry span
func (sc *StreamClient) XGroupCreateMkStreamWithSpan(ctx context.Context, streamName, groupName, startID string) error {
	spanName := fmt.Sprintf("[stream] %s group.create", streamName)
	ctx, span := streamTracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("redis.stream.name", streamName),
			attribute.String("redis.stream.group", groupName),
			attribute.String("redis.operation", "XGROUPCREATE"),
		),
	)
	defer span.End()

	client := sc.Client()
	result := client.XGroupCreateMkStream(ctx, streamName, groupName, startID)
	if result.Err() != nil {
		// BUSYGROUP is not an error - it means the consumer group already exists (expected on restart)
		if result.Err().Error() == "BUSYGROUP Consumer Group name already exists" {
			span.SetAttributes(attribute.Bool("redis.stream.group.exists", true))
			span.SetStatus(codes.Ok, "consumer group already exists")
			return result.Err() // Still return the error so callers can handle it, but don't mark span as error
		}
		span.RecordError(result.Err())
		span.SetStatus(codes.Error, result.Err().Error())
		return result.Err()
	}

	span.SetStatus(codes.Ok, "success")
	return nil
}

// extractEventTypeFromMessage extracts the event type from a Redis stream message
// Returns empty string if extraction fails (non-fatal)
// Tries to extract a clean event type name like "AUTHORIZATION_DEBITED" from protobuf enum strings
func extractEventTypeFromMessage(msg redis.XMessage) string {
	eventJSON, ok := msg.Values["event"].(string)
	if !ok {
		return ""
	}

	// Try to extract type field from JSON without full unmarshaling
	var eventMap map[string]interface{}
	if err := json.Unmarshal([]byte(eventJSON), &eventMap); err != nil {
		return ""
	}

	// Try common event type field names
	var typeStr string
	if typeVal, ok := eventMap["type"].(string); ok {
		typeStr = typeVal
	} else if typeVal, ok := eventMap["Type"].(string); ok {
		typeStr = typeVal
	} else if typeVal, ok := eventMap["type"].(float64); ok {
		// For protobuf JSON, type might be a number
		return fmt.Sprintf("%.0f", typeVal)
	} else {
		return ""
	}

	// Clean up protobuf enum format: "LEDGER_EVENT_TYPE_AUTHORIZATION_DEBITED" -> "AUTHORIZATION_DEBITED"
	// Remove common prefixes
	prefixes := []string{
		"LEDGER_EVENT_TYPE_",
		"CONSUMPTION_EVENT_TYPE_",
		"DEVICE_EVENT_TYPE_",
		"LIGHTNING_EVENT_TYPE_",
	}
	for _, prefix := range prefixes {
		if len(typeStr) > len(prefix) && typeStr[:len(prefix)] == prefix {
			return typeStr[len(prefix):]
		}
	}

	return typeStr
}

// getEnv is a helper function to get environment variables with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
