package internal

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// SimplifyMethodName extracts a compact service/method identifier from a full gRPC method path.
// Example: /foo.bar.Service/Method -> Service/Method.
func SimplifyMethodName(method string) string {
	method = strings.TrimPrefix(method, "/")

	parts := strings.Split(method, "/")
	if len(parts) != 2 {
		return method
	}

	serviceParts := strings.Split(parts[0], ".")
	if len(serviceParts) == 0 {
		return method
	}

	serviceName := serviceParts[len(serviceParts)-1]
	return fmt.Sprintf("%s/%s", serviceName, parts[1])
}

// LoggingUnaryClientInterceptor logs outgoing unary gRPC calls, their responses, and durations.
func LoggingUnaryClientInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	start := time.Now()
	simpleMethod := SimplifyMethodName(method)

	log.Printf("[gRPC] Calling %s with request: %+v", simpleMethod, req)
	err := invoker(ctx, method, req, reply, cc, opts...)

	duration := time.Since(start)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			log.Printf("[gRPC] %s failed: code=%s, message=%s, duration=%v", simpleMethod, st.Code(), st.Message(), duration)
		} else {
			log.Printf("[gRPC] %s failed: error=%v, duration=%v", simpleMethod, err, duration)
		}
	} else {
		log.Printf("[gRPC] %s succeeded: response=%+v, duration=%v", simpleMethod, reply, duration)
	}

	return err
}
