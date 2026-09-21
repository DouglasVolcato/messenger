FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /out/load-tester ./cmd/tester

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=builder --chown=app:app /out/load-tester ./load-tester
USER app

EXPOSE 9091
ENTRYPOINT ["./load-tester"]
