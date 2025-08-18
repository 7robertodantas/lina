# Build Stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum first to leverage Docker's layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Build the Go application
# CGO_ENABLED=0 disables CGO, making the binary statically linked
# GOOS=linux ensures the binary is built for Linux
# -o myapp specifies the output executable name
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o myapp .

# Final Stage
FROM alpine:latest

# Set the working directory
WORKDIR /app

# Copy the compiled binary from the builder stage
COPY --from=builder /app/myapp .

# Expose the port your application listens on
EXPOSE 8080

# Command to run the application when the container starts
CMD ["./myapp"]