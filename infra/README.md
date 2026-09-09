# Local scalability stack

This directory contains the infrastructure used by `docker-compose.yml` for the architecture/scalability exercises.

## Services

| Service | Purpose | Host port |
| --- | --- | --- |
| `load-balancer` | Nginx reverse proxy/load balancer for HTTP server replicas | `80` |
| `server` | Main Go application built from the repository Dockerfile | internal `8080` |
| `postgres` | Primary durable database with logical replication enabled | `5432` |
| `redis` | Cache/connection-registry candidate and local CDC Redis Streams sink | `6379` |
| `rabbitmq` | Processing/event queues | `5672` |
| RabbitMQ management | Queue administration UI | `15672` |
| `cdc` | Debezium Server reading PostgreSQL logical replication | internal |
| `prometheus` | Metrics collection | `9090` |
| `grafana` | Dashboards | `3000` |

Exporter containers expose PostgreSQL, Redis and Nginx metrics to Prometheus. RabbitMQ exposes Prometheus metrics through its native plugin.

## Start

Create the local environment file once:

```bash
cp .env.example .env
```

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
