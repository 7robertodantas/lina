package internal

import (
	"encoding/base64"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// MarshalStreamEvent serializes a protobuf message for Redis stream fields using
// protobuf wire bytes directly (Redis stream values are binary-safe).
func MarshalStreamEvent(m proto.Message) (string, error) {
	wire, err := proto.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal stream event: %w", err)
	}
	return string(wire), nil
}

// UnmarshalStreamEvent decodes a stream event:
// 1. protobuf wire bytes (current format),
// 2. base64-encoded protobuf wire (legacy format),
// 3. protojson (pre-binary legacy format).
func UnmarshalStreamEvent(data string, m proto.Message) error {
	if err := proto.Unmarshal([]byte(data), m); err == nil {
		return nil
	}
	if wire, err := base64.StdEncoding.DecodeString(data); err == nil {
		if err := proto.Unmarshal(wire, m); err == nil {
			return nil
		}
	}
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal([]byte(data), m); err != nil {
		return fmt.Errorf("unmarshal stream event: %w", err)
	}
	return nil
}
