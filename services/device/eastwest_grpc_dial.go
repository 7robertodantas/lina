package main

import (
	"fmt"
	"time"

	internalpkg "github.com/robertodantas/lina/internal"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

func eastWestGRPCDialOptions(cfg Config, peerHost string) ([]grpc.DialOption, error) {
	keepaliveParams := keepalive.ClientParameters{
		Time:                30 * time.Second,
		Timeout:             10 * time.Second,
		PermitWithoutStream: true,
	}
	opts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepaliveParams),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(
			otelgrpc.WithPropagators(otel.GetTextMapPropagator()),
		)),
		grpc.WithUnaryInterceptor(internalpkg.LoggingUnaryClientInterceptor("device-service")),
	}

	if !cfg.GRPCUseTLS {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		return opts, nil
	}

	serverName := cfg.GRPCTLSServerName
	if serverName == "" {
		serverName = peerHost
	}
	tlsConf, err := internalpkg.EastWestGRPCClientTLS(&internalpkg.EastWestGRPCClientTLSParams{
		CACertPath:         cfg.GRPCTLSCACert,
		EdgeCertPath:       cfg.GRPCTLSEdgeCert,
		EdgeKeyPath:        cfg.GRPCTLSEdgeKey,
		ServerName:         serverName,
		InsecureSkipVerify: cfg.GRPCTLSSkipVerify,
	})
	if err != nil {
		return nil, fmt.Errorf("eastwest gRPC TLS: %w", err)
	}
	opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConf)))
	return opts, nil
}
