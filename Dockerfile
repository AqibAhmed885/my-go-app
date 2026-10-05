# Stage 1: Build the Go binary
FROM golang:alpine AS builder

WORKDIR /app

# Allow Go to automatically fetch newer toolchains if specified in go.mod
ENV GOTOOLCHAIN=auto

# Copy dependency manifests first for caching layers
COPY go.mod go.sum ./
RUN go mod download

# Copy application source code
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/api ./cmd/api

# Stage 2: Minimal runtime image
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for secure outbound network calls and timezone data
RUN apk --no-cache add ca-certificates tzdata

# Copy binary and migrations directory from builder
COPY --from=builder /app/api /app/api
COPY --from=builder /app/migrations /app/migrations

EXPOSE 8080

CMD ["/app/api"]