package main

import (
	"github.com/robertodantas/lnpay/library"
)

type Config struct {
	LNDHost       string
	LNDTLSCertHex string
	LNDMacaroonHex string
	Network       string
}

func LoadConfig() Config {
	return Config{
		LNDHost:        library.GetEnv("LND_HOST", ""),
		LNDTLSCertHex:   library.GetEnv("LND_TLS_CERT_HEX", ""),
		LNDMacaroonHex:  library.GetEnv("LND_MACAROON_HEX", ""),
		Network:         library.GetEnv("LND_NETWORK", "mainnet"),
	}
}

