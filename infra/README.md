# Local scalability stack

This directory contains the infrastructure used by `docker-compose.yml` for the architecture/scalability exercises.

## Repository layout

The main HTTP application now lives in its own Go subproject:

```text
server/
  Dockerfile
  go.mod
  cmd/
  internal/
  migrations/
  static/

infra/
  nginx/
  prometheus/
  grafana/
  cdc/

docker-compose.yml
.env
```

A future WebSocket service can follow the same pattern in a sibling `websocket/` directory with its own `go.mod` and Dockerfile.

## Services

| Service | Purpose | Host port |
| --- | --- | --- |
| `load-balancer` | Nginx reverse proxy/load balancer for HTTP server replicas | `80` |
| `server` | Main Go application built from `server/Dockerfile` | internal `8080` |
| `postgres` | Primary durable database with logical replication enabled | `5432` |
| `redis` | Cache/connection-registry candidate and local CDC Redis Streams sink | `6379` |
| `rabbitmq` | Processing/event queues | `5672` |
| RabbitMQ management | Queue administration UI | `15672` |
| `cdc` | Debezium Server reading PostgreSQL logical replication | internal |
| `prometheus` | Metrics collection | `9090` |
| `grafana` | Dashboards | `3000` |

Exporter containers expose PostgreSQL, Redis and Nginx metrics to Prometheus. RabbitMQ exposes Prometheus metrics through its native plugin, which Compose explicitly enables at startup.

## Environment handling

Create the local environment file once:

```bash
cp .env.example .env
```

Docker Compose uses the repository-root `.env` for interpolation and also injects it into the `server` container through `env_file`.

The application still supports loading `server/.env` when run directly for local development, but a `.env` file is no longer required inside the container. Docker-injected environment variables are sufficient.

`DB_URL` is deliberately overridden by Compose so the server connects to `postgres:5432` over the Docker network instead of using the host-local `localhost` value from `.env`.

The HTTP server always listens on internal port `8080` when started by Compose. Nginx and Prometheus therefore have a stable service address even if the host-side `.env` is later changed for direct local execution.

## Start

```bash
docker compose up --build
```

Open:

- Application through Nginx: `http://localhost`
- RabbitMQ management: `http://localhost:15672`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3000`

The default local Grafana credentials come from `GRAFANA_ADMIN_USER` and `GRAFANA_ADMIN_PASSWORD`.

## Server build

Compose builds the server using:

```yaml
build:
  context: ./server
  dockerfile: Dockerfile
```

That means `server/Dockerfile` is intentionally self-contained and only copies files from the server subproject. A dedicated `server/.dockerignore` excludes local build artifacts such as `server/tmp` from the Docker build context.

The resulting runtime image contains only the compiled binary plus the views, migrations and static files required at runtime.

## Server metrics

Prometheus is configured with Docker DNS service discovery for `server`, so when multiple replicas exist it can scrape each replica independently on port `8080`.

The current Go application does not expose `/metrics` yet, so the server targets will remain down until application instrumentation is added. Infrastructure metrics are available immediately.

Grafana automatically provisions the `Messenger - Local Architecture Overview` dashboard and the Prometheus datasource.

## Scaling the HTTP server

The `server` service intentionally has no `container_name`, so Compose can create multiple replicas:

```bash
docker compose up --build --scale server=3
```

Nginx uses Docker DNS and automatic upstream hostname re-resolution. New server replica addresses can therefore be discovered without hard-coding container IPs.

Server startup migrations are protected by a PostgreSQL transaction-level advisory lock. If several replicas start simultaneously, only one applies migrations while the others wait and then observe the already-applied migration records.

The WebSocket service is intentionally not defined yet. When it exists, keep its upstream and scaling policy separate from the HTTP server because long-lived connections have different resource characteristics.

## CDC

PostgreSQL starts with logical WAL enabled. Debezium Server uses `pgoutput`, maintains a replication slot named `messager_cdc`, and writes CDC records to Redis Streams.

RabbitMQ is deliberately not used for CDC in this first local stack. It remains the application's processing/event queue layer. This keeps the two flows explicit:

```text
Application events: server/outbox -> publisher workers -> RabbitMQ
Database CDC:       PostgreSQL WAL -> Debezium -> Redis Streams
```

For a production-like exercise, the CDC sink can later be split onto a dedicated streaming system or dedicated Redis instance so cache failures and CDC retention are not coupled.

## Persistent volumes

PostgreSQL, Redis, RabbitMQ, Prometheus and Grafana use named Docker volumes. To reset the entire lab:

```bash
docker compose down -v
```
