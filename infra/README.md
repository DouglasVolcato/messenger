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
  websocket-nginx/
  prometheus/
  grafana/
  cdc/

docker-compose.yml
.env
```

## Services and network exposure

Only the HTTP Nginx and the dedicated WebSocket Nginx publish ports on the Docker host. Application processes and infrastructure dependencies stay reachable only inside the `messenger` Docker network.

| Service | Purpose | Network exposure |
| --- | --- | --- |
| `load-balancer` | Nginx reverse proxy/load balancer for HTTP server replicas | host `${NGINX_PORT:-80}` -> container `80` |
| `server` | Main Go application | internal `8080` |
| `websocket-load-balancer` | Dedicated Nginx for long-lived WebSocket connections | host `${WEBSOCKET_NGINX_PORT:-8081}` -> container `80` |
| `websocket` | Authenticated WebSocket replicas and Redis connection registry | internal `8080` |
| `postgres` | Primary durable database with logical replication enabled | internal `5432` |
| `redis` | Cache and WebSocket connection registry | internal `6379` |
| `rabbitmq` | Processing/event queues | internal `5672` |
| RabbitMQ management | Queue administration UI | internal `15672` |
| RabbitMQ metrics | Native Prometheus metrics | internal `15692` |
| `cdc` | Debezium Server reading PostgreSQL logical replication | internal |
| `prometheus` | Metrics collection | internal `9090` |
| `grafana` | Dashboards | internal `3000` |

Exporter containers expose PostgreSQL, Redis and both Nginx instances only inside the Compose network. RabbitMQ exposes Prometheus metrics through its native plugin, also only inside that network.

The WebSocket Nginx is intentionally separate from the HTTP Nginx because long-lived upgraded connections have different timeout, connection-count and scaling characteristics from normal HTTP traffic.

## Environment handling

Create the local environment file once:

```bash
cp .env.example .env
```

Docker Compose uses the repository-root `.env` for interpolation and injects it into the application services through `env_file`.

`DB_URL` is deliberately overridden by Compose so the HTTP server connects to `postgres:5432` over the Docker network instead of using the host-local `localhost` value from `.env`.

Both Go services listen on internal port `8080` when started by Compose. Their Nginx instances address them through Docker DNS, so host-side ports can change without changing the application listeners.

The two host entrypoints are controlled independently:

```env
NGINX_PORT=8088
WEBSOCKET_NGINX_PORT=8089
```

The browser WebSocket endpoint is then:

```text
ws://localhost:8089/ws/notifications
```

For production TLS termination, use `wss://` at the external proxy/load balancer. If the browser application and WebSocket use different hostnames, configure a comma-separated allowlist:

```env
WEBSOCKET_ALLOWED_ORIGINS=https://app.example.com,https://admin.example.com
```

Same-host browser origins are accepted automatically, including when the HTTP and WebSocket ports differ.

## Start

```bash
docker compose up --build
```

With the default environment:

- HTTP application: `http://localhost`
- WebSocket: `ws://localhost:8081/ws/notifications`

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

The dedicated WebSocket Nginx uses `least_conn`, disables proxy buffering, forwards the required WebSocket upgrade headers and uses longer proxy read/send timeouts than the HTTP proxy.

The WebSocket process handles SIGINT/SIGTERM, closes active upgraded connections and removes their Redis session records during graceful shutdown.

## Metrics

Prometheus is configured with Docker DNS service discovery for `server`, so when multiple HTTP replicas exist it can scrape each replica independently on port `8080`.

The current Go HTTP application does not expose `/metrics` yet, so those application targets remain down until instrumentation is added. Infrastructure metrics are available immediately.

The HTTP and WebSocket Nginx instances each have their own `nginx-prometheus-exporter`, allowing long-lived WebSocket proxy behavior to be observed independently from normal HTTP traffic.

Grafana automatically provisions the `Messenger - Local Architecture Overview` dashboard and the Prometheus datasource.

## Scaling

The application services intentionally have no `container_name`, so Compose can create multiple replicas:

```bash
docker compose up --build --scale server=3 --scale websocket=3
```

Both Nginx instances use Docker DNS and automatic upstream hostname re-resolution. New replicas can therefore be discovered without hard-coding container IPs.

The HTTP Nginx and WebSocket Nginx remain separate, so server replicas and persistent-connection replicas can be scaled according to different bottlenecks.

Server startup migrations are protected by a PostgreSQL transaction-level advisory lock. If several HTTP replicas start simultaneously, only one applies migrations while the others wait and then observe the already-applied migration records.

The realtime/WebSocket worker responsible for consuming events and routing notifications to the correct WebSocket replica remains a separate architecture step. The current WebSocket service establishes, authenticates and registers connections but does not replace that worker.

## CDC

PostgreSQL starts with logical WAL enabled. Debezium Server uses `pgoutput`, maintains a replication slot named `messager_cdc`, and writes CDC records to Redis Streams.

RabbitMQ is deliberately not used for CDC in this first local stack. It remains the application's processing/event queue layer. This keeps the two flows explicit:

```text
Application events: server/outbox -> publisher workers -> RabbitMQ
Database CDC:       PostgreSQL WAL -> Debezium -> Redis Streams
```

For a production-like exercise, the CDC sink can later be split onto a dedicated streaming system or dedicated Redis instance so cache failures and CDC retention are not coupled.

## Persistent volumes

PostgreSQL, Redis, RabbitMQ, Prometheus and Grafana use named volumes. To reset the entire lab:

```bash
docker compose down -v
```
