# Local scalability stack

This directory contains the infrastructure used by `docker-compose.yml` for the architecture/scalability exercises.

## Repository layout

The HTTP application and WebSocket service live in independent Go subprojects so they can be built and scaled separately:

```text
server/
  Dockerfile
  go.mod
  cmd/
  internal/
  migrations/
  static/

websocket/
  Dockerfile
  go.mod
  cmd/
  cache/
  pkg/

infra/
  nginx/
  prometheus/
  grafana/
  cdc/

docker-compose.yml
.env
```

## Services and network exposure

The Nginx load balancer listens on the internal Compose network. In Coolify, its domain route targets container port `80`; application processes and infrastructure dependencies remain private.

| Service | Purpose | Network exposure |
| --- | --- | --- |
| `load-balancer` | Nginx reverse proxy/load balancer for HTTP and WebSocket replicas | internal `80` (Coolify domain target) |
| `server` | Main Go application | internal `8080` |
| `websocket` | Authenticated WebSocket replicas and Redis connection registry | internal `8080` |
| `postgres` | Primary durable database with logical replication enabled | internal `5432` |
| `redis` | Cache and WebSocket connection registry | internal `6379` |
| `rabbitmq` | Processing/event queues | internal `5672` |
| RabbitMQ management | Queue administration UI | internal `15672` |
| RabbitMQ metrics | Native Prometheus metrics | internal `15692` |
| `cdc` | Debezium Server reading PostgreSQL logical replication | internal |
| `prometheus` | Metrics collection | internal `9090` |
| `grafana` | Dashboards | internal `3000` |
| `cadvisor` | Per-container CPU, memory and network metrics | internal `8080` |
| `node-exporter` | Host resource metrics | internal `9100` |
| `load-tester` | Optional gradual traffic generator (`test` profile) | internal `9091` |

Exporter containers expose PostgreSQL, Redis and Nginx only inside the Compose network. RabbitMQ exposes Prometheus metrics through its native plugin, also only inside that network.

## Environment handling

Create the local environment file once:

```bash
cp .env.example .env
```

Docker Compose uses the repository-root `.env` for interpolation and injects it into the application services through `env_file`.

`DB_URL` is deliberately overridden by Compose so the HTTP server connects to `postgres:5432` over the Docker network instead of using the host-local `localhost` value from `.env`.

Both Go services listen on internal port `8080` when started by Compose. The Nginx load balancer addresses them through Docker DNS, so host-side ports can change without changing the application listeners.

For local direct access, add a temporary Compose override that publishes host port `8088` to container port `80`. In Coolify, do not publish a host port: configure the domain for `load-balancer` on internal port `80`.

For production TLS termination, use `wss://` at the external proxy/load balancer. If the browser application and WebSocket use different hostnames, configure a comma-separated allowlist:

```env
WEBSOCKET_ALLOWED_ORIGINS=https://app.example.com,https://admin.example.com
```

Same-host browser origins are accepted automatically.

## Start

```bash
docker compose up --build
```

With the default environment:

- HTTP application: through the configured Coolify domain
- WebSocket: `wss://<configured-domain>/ws/notifications`

RabbitMQ management, Prometheus, Grafana, PostgreSQL and Redis are intentionally not available directly from the host. Access them from inside the Docker network or add an explicit temporary/debug exposure when required.

The default Grafana credentials come from `GRAFANA_ADMIN_USER` and `GRAFANA_ADMIN_PASSWORD`.

## Application builds

Compose builds the HTTP server from `./server` and the WebSocket service from `./websocket`. Each Dockerfile is self-contained and may only copy files from its own build context.

The WebSocket runtime image only contains the compiled WebSocket binary. Views, migrations and static files belong to the HTTP server and are not copied into the WebSocket image.

## WebSocket lifecycle and routing

The WebSocket process exposes:

```text
GET /healthz
GET /ws/notifications   (HTTP Upgrade)
```

`/ws/notifications` authenticates the existing `user` JWT cookie before upgrading the connection. Each accepted connection receives a unique connection ID and is registered in Redis as:

```text
user:sessions:<user_id>
  <connection_id> -> <websocket_server_id>:<expires_at>
```

The connection registry is refreshed during the ping/pong heartbeat. The expiry timestamp allows future realtime workers to ignore stale connection records after a WebSocket replica crash.

The Nginx load balancer routes `/ws/` to WebSocket replicas using `least_conn`, disables proxy buffering, forwards the required upgrade headers and uses longer proxy read/send timeouts than normal HTTP traffic.

The WebSocket process handles SIGINT/SIGTERM, closes active upgraded connections and removes their Redis session records during graceful shutdown.

## Metrics

Prometheus uses Docker DNS service discovery for the scalable Go services:

- `server:8080/metrics`
- `websocket:8080/metrics`
- `publisher-worker:9090/metrics`
- `websocket-worker:9090/metrics`

This keeps each replica visible as an independent Prometheus target.

The first application metrics include HTTP request rate/latency, database-pool pressure, active WebSocket connections, slow-client disconnects, publisher/outbox pressure and WebSocket-worker throughput.

The Compose stack also includes:

- cAdvisor for per-container CPU, memory and network usage;
- node-exporter for host CPU/memory/filesystem/network metrics;
- PostgreSQL exporter;
- Redis exporter;
- Nginx exporter;
- RabbitMQ native Prometheus metrics.

Grafana automatically provisions both `Messenger - Local Architecture Overview` and `Messenger - Load Testing`, together with the Prometheus datasource.

The optional `load-tester` Compose service exposes its own metrics on port `9091` while a test is running. See `load_test/README.md` for usage.

## Scaling

The application services intentionally have no `container_name`, so Compose can create multiple replicas:

```bash
docker compose up --build --scale server=3 --scale websocket=3
```

The Nginx load balancer uses Docker DNS and automatic upstream hostname re-resolution for both service pools. New replicas can therefore be discovered without hard-coding container IPs.

Server startup migrations are protected by a PostgreSQL transaction-level advisory lock. If several HTTP replicas start simultaneously, only one applies migrations while the others wait and then observe the already-applied migration records.

The WebSocket worker consumes RabbitMQ queues with one loop per priority: direct messages are high priority, chat messages normal priority and company membership events low priority. For each event it reads the active sessions from Redis, resolves the matching WebSocket server's internal gRPC endpoint and calls it directly. The server delivers the notification only to the listed live connections.

## CDC

PostgreSQL starts with logical WAL enabled. Debezium Server uses `pgoutput`, maintains a replication slot named `messager_cdc`, and writes CDC records to Redis Streams.

RabbitMQ is deliberately not used for CDC in this first local stack. It remains the application's processing/event queue layer. This keeps the two flows explicit:

```text
Application events: server/outbox -> publisher workers -> RabbitMQ
Database CDC:       PostgreSQL WAL -> Debezium -> Redis Streams
```

For a production-like exercise, the CDC sink can later be split onto a dedicated streaming system or dedicated Redis instance so cache failures and CDC retention are not coupled.

## Load testing

The load generator is isolated behind the Compose `test` profile, so normal application startup does not create test traffic.

Start the normal stack first:

```bash
docker compose up -d --build
```

Then run the load tester:

```bash
docker compose --profile test up --build load-tester
```

It gradually increases generated users, HTTP request rate and active WebSocket connections while Prometheus/Grafana collect both injected-load and system metrics.

For all parameters, generated routes and cleanup guidance, see `load_test/README.md`.

## Persistent volumes

PostgreSQL, Redis, RabbitMQ, Prometheus and Grafana use named volumes. To reset the entire lab:

```bash
docker compose down -v
```
