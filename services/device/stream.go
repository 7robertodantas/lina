package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"

	devicepb "github.com/robertodantas/lnpay/proto/gen/gen/iot/payperuse/edge/model/device"
	mqttpb "github.com/robertodantas/lnpay/proto/gen/gen/iot/payperuse/edge/model/mqtt"
)

// StreamClient wraps the Redis client for stream operations
type StreamClient struct {
	client *redis.Client
	ctx    context.Context
}

// NewStreamClient creates a new Redis stream client
func NewStreamClient() (*StreamClient, error) {
	host := getEnv("REDIS_HOST", "redis")
	port := getEnv("REDIS_PORT", "6379")
	password := getEnv("REDIS_PASSWORD", "")
	dbStr := getEnv("REDIS_DB", "0")

	db, err := strconv.Atoi(dbStr)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_DB value: %w", err)
	}

	addr := fmt.Sprintf("%s:%s", host, port)
	log.Printf("Connecting to Redis at %s (db: %d)...", addr, db)

	opts := &redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	}

	client := redis.NewClient(opts)
	ctx := context.Background()

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Println("Connected to Redis successfully")

	return &StreamClient{
		client: client,
		ctx:    ctx,
	}, nil
}

// convertReportingStrategy converts MQTT ReportingStrategy to device UsageReportingStrategy
func convertReportingStrategy(strategy mqttpb.ReportingStrategy) devicepb.UsageReportingStrategy {
	switch strategy {
	case mqttpb.ReportingStrategy_REPORTING_STRATEGY_INTERVAL:
		return devicepb.UsageReportingStrategy_USAGE_STRATEGY_INTERVAL
	case mqttpb.ReportingStrategy_REPORTING_STRATEGY_DELTA:
		return devicepb.UsageReportingStrategy_USAGE_STRATEGY_DELTA
	case mqttpb.ReportingStrategy_REPORTING_STRATEGY_TOTAL:
		return devicepb.UsageReportingStrategy_USAGE_STRATEGY_TOTAL
	default:
		return devicepb.UsageReportingStrategy_USAGE_STRATEGY_UNSPECIFIED
	}
}

// PublishDeviceUsageReportedEvent publishes a DeviceEvent containing DeviceUsageReportedEvent to the Redis stream
func (sc *StreamClient) PublishDeviceUsageReportedEvent(payload *mqttpb.UsagePayload) error {
	// Convert MQTT UsagePayload to device UsageRecord
	usageRecord := &devicepb.UsageRecord{
		DeviceId:  payload.GetDeviceId(),
		ReportId:  payload.GetReportId(),
		Strategy:  convertReportingStrategy(payload.GetStrategy()),
		Measure:   payload.GetMeasure(),
		Unit:      payload.GetUnit(),
		Timestamp: payload.GetTimestamp(),
	}

	// Create the DeviceUsageReportedEvent
	usageReportedEvent := &devicepb.DeviceUsageReportedEvent{
		Usage: usageRecord,
	}

	// Wrap in DeviceEvent envelope
	deviceEvent := &devicepb.DeviceEvent{
		Type: devicepb.DeviceEventType_DEVICE_EVENT_TYPE_USAGE_REPORTED,
		Payload: &devicepb.DeviceEvent_UsageReported{
			UsageReported: usageReportedEvent,
		},
	}

	// Serialize to JSON for Redis stream
	opts := protojson.MarshalOptions{UseProtoNames: true}
	jsonBytes, err := opts.Marshal(deviceEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal device event to JSON: %w", err)
	}

	// Publish to Redis stream "event.device"
	streamName := "event.device"
	values := map[string]interface{}{
		"event": string(jsonBytes),
		// Add timestamp for stream ordering
		"timestamp": time.Now().UnixMilli(),
	}

	// Use XADD to add entry to stream
	result := sc.client.XAdd(sc.ctx, &redis.XAddArgs{
		Stream: streamName,
		Values: values,
	})

	if result.Err() != nil {
		return fmt.Errorf("failed to publish to Redis stream %s: %w", streamName, result.Err())
	}

	log.Printf("Published DeviceEvent (usage reported) to stream %s (ID: %s)", streamName, result.Val())
	return nil
}

// Close closes the Redis client connection
func (sc *StreamClient) Close() error {
	if err := sc.client.Close(); err != nil {
		return fmt.Errorf("failed to close Redis client: %w", err)
	}
	log.Println("Redis client closed")
	return nil
}
