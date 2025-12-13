# Build stage
FROM golang:1.25-alpine AS builder

    WORKDIR /workspace

    # Copy proto module
    COPY services/proto/go.mod ./services/proto/
    COPY services/proto/gen ./services/proto/gen

    # Copy core files
    COPY testing/smartmeter/core/go.mod testing/smartmeter/core/go.sum ./smartmeter/core/
    WORKDIR /workspace/smartmeter/core
    RUN go mod download

    # Copy source code
    COPY testing/smartmeter/core/*.go ./

    # Build the application
    RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o core .

# Runtime stage
FROM alpine:latest

    RUN apk --no-cache add ca-certificates

    WORKDIR /root/

    # Copy the binary from builder
    COPY --from=builder /workspace/smartmeter/core/core .

    # Expose port
    EXPOSE 8080

    # Set default environment variables
    ENV PORT=8080
    ENV DEVICE_ID=smart-meter-001
    ENV MQTT_BROKER_URL=tcp://mqtt:1883

    # Run the application
    CMD ["./core"]
