# Build Stage
FROM golang:1.25-alpine AS builder

WORKDIR /workspace

# Copy common proto3 definition files
COPY services/proto/go.mod ./services/proto/
COPY services/proto/gen ./services/proto/gen

# Copy shared library for common utilities
COPY services/internal/go.mod ./services/internal/
COPY services/internal/*.go ./services/internal/

# Copy autopay service files
COPY autopay/go.mod autopay/go.sum ./autopay/
WORKDIR /workspace/autopay
RUN go mod download

# Copy autopay service source code
COPY autopay ./

# Build the autopay service
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o autopay .

# Final Stage
FROM alpine:latest

ENV APP=autopay
WORKDIR /app

COPY --from=builder /workspace/autopay/autopay .

EXPOSE 8080

CMD ./${APP}

