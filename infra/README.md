# Local scalability stack

This directory contains the infrastructure used by `docker-compose.yml` for the architecture/scalability exercises.

## Services

| Service | Purpose | Host port |
| --- | --- | --- |
| `load-balancer` | Nginx reverse proxy/load balancer for HTTP server replicas | `80` |
| `server` | Main Go application built from `server/Dockerfile` with `server/` as its Docker build context | internal `8080` |
| `postgres` | Primary durable database with logical replication enabled | `5432` |
| `redis` | Cache/connection-registry candidate and local CDC Redis Streams sink | `6379` |
| `rabbitmq` | Processing/event queues | `5672` |
| RabbitMQ management | Queue administration UI | `15672` |
| `cdc` | Debezium Server reading PostgreSQL logical replication | internal |
| `prometheus` | Metrics collection | `9090` |
| `grafana` | Dashboards | `3000` |

Exporter containers expose PostgreSQL, Redis and Nginx metrics to Prometheus. RabbitMQ exposes Prometheus metrics through its native plugin.

## Start

Create the repository-level environment file once:

```bash
cp .env.example .env
```

Docker Compose reads that file for variable interpolation and also injects it into the `server` process through `env_file`. The server container does not need the `.env` file mounted into its filesystem.

Compose intentionally overrides container-specific values that must use Docker networking:

- `PORT=8080` inside the `server` container
- `DB_URL` points to the `postgres` service instead of `localhost`
- `MIGRATIONS_DIR=migrations`, matching the path copied by `server/Dockerfile`

Then start the stack:

```bash
docker compose up --build
```

Open:

- Application through Nginx: `http://localhost`
- RabbitMQ management: `http://localhost:15672`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3000`

The default local Grafana credentials come from `GRAFANA_ADMIN_USER` and `GRAFANA_ADMIN_PASSWORD`.

## Running the server outside Compose

The Go server can still be run directly from its own module directory:

```bash
cd server
go run ./cmd/api
```

At startup it looks for `server/.env` first and then the repository-level `../.env`. If neither file exists, it simply uses environment variables already provided by the process/container.

## Server metrics

Prometheus is already configured to scrape:

```text
http://server:8080/metrics
```

The current Go application does not expose that endpoint yet, so the `server` Prometheus target will remain down until application instrumentation is added. Infrastructure metrics are available immediately.

Grafana automatically provisions the `Messenger - Local Architecture Overview` dashboard and the Prometheus datasource.

## Scaling the HTTP server

The `server` service intentionally has no `container_name`, so Compose can create multiple replicas:

```bash
docker compose up --build --scale server=3
```

Nginx uses Docker DNS and automatic upstream hostname re-resolution. New server replica addresses can therefore be discovered without hard-coding container IPs.

Server startup migrations use a PostgreSQL transaction-scoped advisory lock. If several replicas start together, only one applies schema migrations at a time; the others wait and then observe the recorded migration IDs.

The WebSocket service is intentionally not defined yet. When it exists, keep its source and Dockerfile under a separate `websocket/` directory, give it its own Compose service/build context, and keep its Nginx upstream and scaling policy separate from the HTTP server because long-lived connections have different resource characteristics.

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
