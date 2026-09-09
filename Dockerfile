# =====================================================================
# STAGE 1: Build the Go binary
# =====================================================================
FROM golang:1.25-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy dependency manifests first to leverage Docker caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Build the binary statically (CGO disabled for compatibility with scratch)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /out/messager ./cmd/api

# =====================================================================
# STAGE 2: Run the binary in a secure, minimal container
# =====================================================================
FROM alpine:3.22

# Set working directory in the final isolated environment
WORKDIR /app

# Copy the pre-compiled binary from the builder stage
COPY --from=builder /out/messager ./messager
COPY internal/views ./internal/views
COPY migrations ./migrations
COPY static ./static

# Expose the application port (change 8080 to match your app)
EXPOSE 8080

# Run the binary
ENTRYPOINT ["./messager"]
