package internal

import "crypto/tls"

// ApplyNanomqMQTTTLSCompat configures a client tls.Config for NanoMQ brokers (mbedTLS).
// Go 1.22+ trims default TLS 1.2 cipher offerings; mbedTLS may not overlap, which surfaces
// as mbedTLS "Cryptographic error" during handshake. TLS 1.3 is not negotiated reliably with
// NanoMQ 0.24, so the profile is TLS 1.2-only with conservative ECDHE/RSA-AES suites.
func ApplyNanomqMQTTTLSCompat(cfg *tls.Config) {
	if cfg == nil {
		return
	}
	cfg.MinVersion = tls.VersionTLS12
	cfg.MaxVersion = tls.VersionTLS12
	// mbedTLS typically enables P-256/P-384 before X25519; include X25519 for Go/OpenSSL peers.
	cfg.CurvePreferences = []tls.CurveID{tls.CurveP256, tls.CurveP384, tls.X25519}
	cfg.CipherSuites = []uint16{
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		// Legacy RSA key exchange (some mbedTLS builds); requires GODEBUG=tlsrsakex=1 on Go 1.22+ if used.
		tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	}
}
