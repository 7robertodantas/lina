# Build Smart Meter UI
FROM node:22-alpine AS ui-build
    WORKDIR /app/ui
    COPY smartmeter/ui/package.json smartmeter/ui/package-lock.json* ./
    RUN npm install
    COPY smartmeter/ui .
    RUN npm run build

# Build Smart Meter Core
FROM golang:1.25-alpine AS core-builder

    WORKDIR /workspace

    # Copy common proto3 definition files
    COPY services/proto/go.mod ./services/proto/
    COPY services/proto/gen ./services/proto/gen

    # Copy shared library for common utilities
    COPY services/internal/go.mod ./services/internal/
    COPY services/internal/ *.go ./services/internal/

    # Copy Smart Meter Core files
    COPY smartmeter/core/go.mod smartmeter/core/go.sum ./smartmeter/core/
    WORKDIR /workspace/smartmeter/core
    RUN go mod download

    # Copy Smart Meter Core source code
    COPY smartmeter/core/*.go ./

    # Build Smart Meter Core application
    RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o smartmeter .

# Smart Meter Runtime
FROM alpine:latest
    RUN apk --no-cache add ca-certificates
    WORKDIR /app

    # Copy Smart Meter Core binary
    COPY --from=core-builder /workspace/smartmeter/core/smartmeter .

    # Copy Smart Meter UI static files
    COPY --from=ui-build /app/ui/out ./public

    # Expose single port for both Smart Meter UI and WebSocket
    EXPOSE 8080

    # Start the application (serves UI and WebSocket on same port)
    CMD ["./smartmeter"]