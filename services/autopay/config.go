package main

import (
	"log"

	"github.com/robertodantas/lnpay/internal"
)

type Config struct {
	// Receiver LND Configuration (the node that creates invoices)
	ReceiverLNDHost          string
	ReceiverLNDTLSCertHex    string
	ReceiverLNDTLSServerName string
	ReceiverLNDMacaroonHex   string

	// Payer LND Configuration (the node that pays invoices)
	PayerLNDHost          string
	PayerLNDTLSCertHex    string
	PayerLNDTLSServerName string
	PayerLNDMacaroonHex   string

	Network string
}

func LoadConfig() *Config {
	cfg := &Config{
		// Receiver LND (creates invoices) - defaults to same as lightning service
		ReceiverLNDHost:          internal.GetEnv("RECEIVER_LND_HOST", internal.GetEnv("LND_HOST", "localhost:10009")),
		ReceiverLNDTLSCertHex:    internal.GetEnv("RECEIVER_LND_TLS_CERT_HEX", internal.GetEnv("LND_TLS_CERT_HEX", "")),
		ReceiverLNDTLSServerName: internal.GetEnv("RECEIVER_LND_TLS_SERVER_NAME", internal.GetEnv("LND_TLS_SERVER_NAME", "localhost")),
		ReceiverLNDMacaroonHex:   internal.GetEnv("RECEIVER_LND_MACAROON_HEX", internal.GetEnv("LND_MACAROON_HEX", "")),

		// Payer LND (pays invoices) - can be same or different node
		PayerLNDHost:          internal.GetEnv("PAYER_LND_HOST", internal.GetEnv("LND_HOST", "localhost:10009")),
		PayerLNDTLSCertHex:    internal.GetEnv("PAYER_LND_TLS_CERT_HEX", internal.GetEnv("LND_TLS_CERT_HEX", "")),
		PayerLNDTLSServerName: internal.GetEnv("PAYER_LND_TLS_SERVER_NAME", internal.GetEnv("LND_TLS_SERVER_NAME", "localhost")),
		PayerLNDMacaroonHex:   internal.GetEnv("PAYER_LND_MACAROON_HEX", internal.GetEnv("LND_MACAROON_HEX", "")),

		Network: internal.GetEnv("NETWORK", "regtest"),
	}

	// Validate receiver LND configuration
	if cfg.ReceiverLNDHost == "" {
		log.Fatal("RECEIVER_LND_HOST or LND_HOST environment variable required")
	}
	if cfg.ReceiverLNDTLSCertHex == "" {
		log.Fatal("RECEIVER_LND_TLS_CERT_HEX or LND_TLS_CERT_HEX environment variable required")
	}
	if cfg.ReceiverLNDMacaroonHex == "" {
		log.Fatal("RECEIVER_LND_MACAROON_HEX or LND_MACAROON_HEX environment variable required")
	}

	// Validate payer LND configuration
	if cfg.PayerLNDHost == "" {
		log.Fatal("PAYER_LND_HOST or LND_HOST environment variable required")
	}
	if cfg.PayerLNDTLSCertHex == "" {
		log.Fatal("PAYER_LND_TLS_CERT_HEX or LND_TLS_CERT_HEX environment variable required")
	}
	if cfg.PayerLNDMacaroonHex == "" {
		log.Fatal("PAYER_LND_MACAROON_HEX or LND_MACAROON_HEX environment variable required")
	}

	return cfg
}
