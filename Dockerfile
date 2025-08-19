# Build Stage
FROM golang:1.25-alpine AS builder

# ARG for service folder (e.g. consumption, registry, ledger)
ARG SERVICE
WORKDIR /app

# Copy go.mod and go.sum first to leverage Docker's layer caching
COPY ${SERVICE}/go.mod ${SERVICE}/go.sum ./
RUN go mod download

# Copy the rest of the application source code
COPY ${SERVICE} ./

# Build the Go application
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o myapp .

# Final Stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/myapp .

EXPOSE 8080

CMD ["./myapp"]