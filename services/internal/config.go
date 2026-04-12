package internal

import (
	"os"
	"strconv"
)

// GetEnv retrieves an environment variable or returns the default value
func GetEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// IntEnv retrieves an integer environment variable or returns the default value
func IntEnv(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}

// ClampStreamReadCount bounds Redis XREADGROUP COUNT (batch size). Values below 1 become 1; above max become max.
func ClampStreamReadCount(n int) int {
	const maxStreamReadCount = 1000
	if n < 1 {
		return 1
	}
	if n > maxStreamReadCount {
		return maxStreamReadCount
	}
	return n
}

// BoolEnv retrieves a boolean environment variable or returns the default value
func BoolEnv(key string, defaultValue bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultValue
}

// Redis stream tuning (ledger, consumption, device):
// Primary env vars inside each container: STREAM_READ_COUNT, STREAM_PARALLELISM.
// Docker Compose / Ansible map per-service overrides, e.g. STREAM_READ_COUNT=${LEDGER_STREAM_READ_COUNT:-100}.
// Fallback order: STREAM_* → REDIS_STREAM_* (older name) → legacyKey (e.g. LEDGER_STREAM_READ_COUNT) → defaultVal.

// StreamReadCountFromEnv resolves XREADGROUP batch size.
func StreamReadCountFromEnv(legacyKey string, defaultVal int) int {
	if v := os.Getenv("STREAM_READ_COUNT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return ClampStreamReadCount(i)
		}
	}
	if v := os.Getenv("REDIS_STREAM_READ_COUNT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return ClampStreamReadCount(i)
		}
	}
	if legacyKey != "" {
		return ClampStreamReadCount(IntEnv(legacyKey, defaultVal))
	}
	return ClampStreamReadCount(defaultVal)
}

// StreamParallelismFromEnv resolves max concurrent handlers per stream batch.
func StreamParallelismFromEnv(legacyKey string, defaultVal int) int {
	if v := os.Getenv("STREAM_PARALLELISM"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return ClampConsumeParallelism(i)
		}
	}
	if v := os.Getenv("REDIS_STREAM_PARALLELISM"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return ClampConsumeParallelism(i)
		}
	}
	if legacyKey != "" {
		return ClampConsumeParallelism(IntEnv(legacyKey, defaultVal))
	}
	return ClampConsumeParallelism(defaultVal)
}
