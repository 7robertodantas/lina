package main

import (
	"github.com/robertodantas/lnpay/library"
)

type Config struct {
	// Database
	DBPath string

	// API
	APIAddr string

	// MQTT Configuration
	MQTTBroker             string
	MQTTUseTLS             bool
	MQTTPort               int
	MQTTTLSPort            int
	MQTTTLSProtocol        string
	MQTTClientID           string
	MQTTUsername           string
	MQTTPassword           string
	MQTTTLSSkipVerify      bool
	MQTTTLSServerName      string
	MQTTTLSCACert          string
	MQTTTLSRequireEdgeCert bool
	MQTTTLSEdgeCert        string
	MQTTTLSEdgeKey         string

	// MQTT Dynamic Security
	MQTTDynSecAdminUser     string
	MQTTDynSecAdminPassword string

	// Ledger gRPC
	LedgerGRPCHost string
	LedgerGRPCPort int
}

func LoadConfig() Config {
	return Config{
		// Database
		DBPath: library.GetEnv("DB_PATH", "devices.db"),

		// API
		APIAddr: library.GetEnv("API_ADDR", ":8080"),

		// MQTT Configuration
		MQTTBroker:             library.GetEnv("MQTT_BROKER", "mosquitto"),
		MQTTUseTLS:             library.BoolEnv("MQTT_USE_TLS", true),
		MQTTPort:               library.IntEnv("MQTT_PORT", 1883),
		MQTTTLSPort:            library.IntEnv("MQTT_TLS_PORT", 8883),
		MQTTTLSProtocol:        library.GetEnv("MQTT_TLS_PROTOCOL", "tls"),
		MQTTClientID:           library.GetEnv("MQTT_CLIENT_ID", "device-service"),
		MQTTUsername:           library.GetEnv("MQTT_USERNAME", ""),
		MQTTPassword:           library.GetEnv("MQTT_PASSWORD", ""),
		MQTTTLSSkipVerify:      library.BoolEnv("MQTT_TLS_SKIP_VERIFY", false),
		MQTTTLSServerName:      library.GetEnv("MQTT_TLS_SERVER_NAME", ""),
		MQTTTLSCACert:          library.GetEnv("MQTT_TLS_CA_CERT", "/certs/ca.crt"),
		MQTTTLSRequireEdgeCert: library.BoolEnv("MQTT_TLS_REQUIRE_EDGE_CERT", false),
		MQTTTLSEdgeCert:        library.GetEnv("MQTT_TLS_EDGE_CERT", ""),
		MQTTTLSEdgeKey:         library.GetEnv("MQTT_TLS_EDGE_KEY", ""),

		// MQTT Dynamic Security
		MQTTDynSecAdminUser:     library.GetEnv("MQTT_DYNSEC_ADMIN_USER", "admin"),
		MQTTDynSecAdminPassword: library.GetEnv("MQTT_DYNSEC_ADMIN_PASSWORD", "admin"),

		// Ledger gRPC
		LedgerGRPCHost: library.GetEnv("LEDGER_GRPC_HOST", "ledger"),
		LedgerGRPCPort: library.IntEnv("LEDGER_GRPC_PORT", 9090),
	}
}
