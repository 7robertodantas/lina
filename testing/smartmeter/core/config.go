package main

import (
	"github.com/robertodantas/lina/internal"
	devicepkg "github.com/robertodantas/lina/testing/device"
)

// Config holds environment-driven settings for the simulator backend (HTTP, MQTT, usage mode).
// MQTT fields are loaded like services/device via testing/device.LoadConfig.
type Config struct {
	devicepkg.Config
	UsageMode string
}

// LoadConfig loads runtime configuration from environment variables.
func LoadConfig() Config {
	return Config{
		Config:    devicepkg.LoadConfig(),
		UsageMode: internal.GetEnv("USAGE_MODE", "simulation"),
	}
}
