# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o trading-engine ./cmd/server

# Runtime stage
FROM alpine:latest

# Install runtime dependencies for health checks and CA certificates
RUN apk --no-cache add ca-certificates tzdata wget curl

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/trading-engine .
COPY --from=builder /app/config ./config

# Expose ports
EXPOSE 8080 8081 9090

# Run the application
CMD ["./trading-engine"]
