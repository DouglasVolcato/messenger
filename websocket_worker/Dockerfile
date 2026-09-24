FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /out/websocket_worker ./cmd/worker

FROM alpine:3.22

WORKDIR /app
RUN addgroup -S app && adduser -S -G app app

COPY --from=builder --chown=app:app /out/websocket_worker ./websocket_worker

USER app

EXPOSE 9090
ENTRYPOINT ["./websocket_worker"]
