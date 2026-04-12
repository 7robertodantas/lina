package internal

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// EastWestGRPCServerTLS returns a tls.Config for internal (east-west) gRPC servers that
// require clients to present a certificate signed by the given CA (mTLS).
func EastWestGRPCServerTLS(caCertPath, serverCertPath, serverKeyPath string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(serverCertPath, serverKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load server certificate: %w", err)
	}
	caPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("read CA certificate: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse CA certificate for client verification")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// EastWestGRPCClientTLSParams configures TLS for internal gRPC clients (e.g. device → ledger).
type EastWestGRPCClientTLSParams struct {
	CACertPath         string
	EdgeCertPath       string
	EdgeKeyPath        string
	ServerName         string
	InsecureSkipVerify bool
}

// EastWestGRPCClientTLS returns a tls.Config for mTLS gRPC clients using the edge client certificate.
func EastWestGRPCClientTLS(p *EastWestGRPCClientTLSParams) (*tls.Config, error) {
	if p == nil {
		return nil, fmt.Errorf("TLS params are nil")
	}
	cert, err := tls.LoadX509KeyPair(p.EdgeCertPath, p.EdgeKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load edge certificate: %w", err)
	}
	caPEM, err := os.ReadFile(p.CACertPath)
	if err != nil {
		return nil, fmt.Errorf("read CA certificate: %w", err)
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse CA certificate")
	}
	return &tls.Config{
		RootCAs:            rootCAs,
		Certificates:       []tls.Certificate{cert},
		ServerName:         p.ServerName,
		InsecureSkipVerify: p.InsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}, nil
}
