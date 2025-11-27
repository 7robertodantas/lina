# Build UI
FROM node:22-alpine AS ui-build
    WORKDIR /app/ui
    ARG NEXT_PUBLIC_BACKEND_WS_URL=ws://localhost:8080/ws
    COPY smartmeter/ui/package.json smartmeter/ui/package-lock.json* ./
    RUN npm install
    COPY smartmeter/ui .
    ENV NEXT_PUBLIC_BACKEND_WS_URL=${NEXT_PUBLIC_BACKEND_WS_URL}
    RUN npm run build

# Build Core
FROM golang:1.25-alpine AS core-builder

    WORKDIR /workspace

    # Copy proto module
    COPY services/proto/go.mod ./services/proto/
    COPY services/proto/gen ./services/proto/gen

    # Copy core files
    COPY smartmeter/core/go.mod smartmeter/core/go.sum ./smartmeter/core/
    WORKDIR /workspace/smartmeter/core
    RUN go mod download

    # Copy source code
    COPY smartmeter/core/*.go ./

    # Build the application
    RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o core .

# Production
FROM alpine:latest
    RUN apk --no-cache add ca-certificates
    WORKDIR /app

    # Copy core binary
    COPY --from=core-builder /workspace/smartmeter/core/core .

    # Copy UI static files
    COPY --from=ui-build /app/ui/out ./public

    # Expose single port for both UI and WebSocket
    EXPOSE 8080

    # Start the application (serves UI and WebSocket on same port)
    CMD ["./core"]