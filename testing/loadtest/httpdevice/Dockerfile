# Build Stage
FROM golang:1.25-alpine AS builder

    WORKDIR /workspace

    # Copy common proto3 definition files
    COPY services/proto/go.mod ./services/proto/
    COPY services/proto/gen ./services/proto/gen

    # Copy internal module (needed by device package)
    COPY services/internal/go.mod services/internal/go.sum ./services/internal/
    COPY services/internal/*.go ./services/internal/

    # Copy device package
    COPY testing/device/go.mod testing/device/go.sum ./testing/device/
    COPY testing/device/*.go ./testing/device/

    # Copy httpdevice files
    COPY testing/loadtest/httpdevice/go.mod testing/loadtest/httpdevice/go.sum ./testing/loadtest/httpdevice/
    WORKDIR /workspace/testing/loadtest/httpdevice
    RUN go mod download

    # Copy httpdevice source code
    COPY testing/loadtest/httpdevice/*.go ./

    # Build the httpdevice application
    RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o httpdevice .

# Final Stage
FROM alpine:3.23

    ENV APP=httpdevice
    WORKDIR /app

    COPY --from=builder /workspace/testing/loadtest/httpdevice/httpdevice .

    EXPOSE 8080

    CMD ["./httpdevice"]

