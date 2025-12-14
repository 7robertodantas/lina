package main

import (
	"os"
	"strconv"
)

// Config holds environment-driven settings for the HTTP device service runtime (MQTT and HTTP).
// Distinct from ProtoConfig which is the device state configuration received over MQTT.
type Config struct {
	HTTPPort          string
	MQTTBroker        string
	MQTTUseTLS        bool
	MQTTPort          int
	MQTTTLSPort       int
	MQTTTLSCACert     string
	MQTTTLSSkipVerify bool
	MQTTTLSServerName string
}

// LoadConfig loads runtime configuration from environment variables.
func LoadConfig() *Config {
	return &Config{
		HTTPPort:          getEnvCfg("PORT", "8080"),
		MQTTBroker:        getEnvCfg("MQTT_BROKER", "mosquitto"),
		MQTTUseTLS:        boolEnvCfg("MQTT_USE_TLS", true),
		MQTTPort:          intEnvCfg("MQTT_PORT", 1883),
		MQTTTLSPort:       intEnvCfg("MQTT_TLS_PORT", 8883),
		MQTTTLSCACert:     getEnvCfg("MQTT_TLS_CA_CERT", "/certs/ca.crt"),
		MQTTTLSSkipVerify: boolEnvCfg("MQTT_TLS_SKIP_VERIFY", false),
		MQTTTLSServerName: getEnvCfg("MQTT_TLS_SERVER_NAME", "mosquitto"),
	}
}

func getEnvCfg(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func intEnvCfg(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func boolEnvCfg(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

