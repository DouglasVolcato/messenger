# =====================================================================
# STAGE 1: Build the Go binary
# =====================================================================
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy dependency manifests first to maximize Docker layer reuse.
COPY go.mod go.sum ./
RUN go mod download

# The Compose build context is ./server, so these paths are all local to
# the server project and remain independent from future sibling services.
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /out/websocket_worker ./cmd/worker

# =====================================================================
# STAGE 2: Runtime image
# =====================================================================
FROM alpine:3.22

WORKDIR /app

RUN addgroup -S app && adduser -S -G app app

COPY --from=builder --chown=app:app /out/websocket_worker ./websocket_worker

USER app

ENTRYPOINT ["./websocket_worker"]
