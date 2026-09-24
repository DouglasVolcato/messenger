# Local scalability stack

This directory contains the infrastructure used by `docker-compose.yml` for the architecture/scalability exercises.

## Repository layout

The HTTP application, WebSocket service and asynchronous workers live in independent Go subprojects so they can be built and scaled separately:

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

websocket_worker/
  Dockerfile
  go.mod
  cmd/
  internal/

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
| `websocket` | Authenticated WebSocket replicas with Redis session registration and a per-replica RabbitMQ delivery queue | internal `8080` |
| `publisher-worker` | Reads notification outbox rows and publishes them to shared notification work queues | internal `9090` |
| `websocket-worker` | Resolves active sessions in Redis and routes notifications to the correct WebSocket replica queue; metrics/readiness on the same internal listener | internal `9090` |
| `postgres` | Primary durable database with logical replication enabled | internal `5432` |
| `redis` | Application cache plus WebSocket connection/replica registry with TTL | internal `6379` |
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

The HTTP server and WebSocket frontend listen on internal port `8080` when started by Compose. The Nginx load balancer addresses them through Docker DNS, so host-side ports can change without changing the application listeners.

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

Compose builds the HTTP server from `./server`, the WebSocket service from `./websocket`, the publisher from `./publisher_worker` and the routing worker from `./websocket_worker`. Each Dockerfile is self-contained and may only copy files from its own build context.

The WebSocket runtime image only contains the compiled WebSocket binary. Views, migrations and static files belong to the HTTP server and are not copied into the WebSocket image.

## WebSocket lifecycle and routing

The WebSocket process exposes:

```text
GET /livez             (process liveness)
GET /readyz            (Redis + RabbitMQ delivery readiness)
GET /healthz           (Compose-compatible readiness alias)
GET /ws/notifications  (HTTP Upgrade)
```

`/ws/notifications` authenticates the existing `user` JWT cookie before upgrading the connection.

Each WebSocket replica has a replica ID. In Kubernetes the ID is injected from the Pod name through the downward API; in Docker Compose/local execution the process generates a UUID.

Every accepted connection is registered in Redis as:

```text
user:sessions:<user_id>
  <connection_id> -> <replica_id>:<expires_at>
```

The session expiration timestamp is refreshed during the WebSocket heartbeat. The Redis hash also receives a key TTL, so abandoned registry data cannot live forever after replica/process failures.

A live WebSocket replica also publishes its own short-lived presence key:

```text
websocket:replica:<replica_id> -> <rabbitmq_queue_name>
```

and owns one exclusive, auto-delete RabbitMQ queue:

```text
websocket.delivery.<replica_id>
```

bound to the durable direct exchange `websocket.delivery` with routing key `<replica_id>`.

Realtime delivery therefore has two RabbitMQ stages:

```text
server transaction
      |
      v
notification outbox
      |
      v
publisher-worker
      |
      v
notifications.direct_message
notifications.chat_message
notifications.company_membership
      |
      v
websocket-worker
      |
      +--> Redis: resolve active connection IDs + replica IDs
      |
      v
websocket.delivery exchange
      |
      +--> websocket.delivery.<replica-A>
      +--> websocket.delivery.<replica-B>
      +--> websocket.delivery.<replica-C>
                    |
                    v
             local sockets only
```

Multiple `websocket-worker` replicas compete for the shared notification work queues. A worker processes each notification once, resolves the user's active sessions in Redis, groups connection IDs by replica, and publishes one delivery command per target replica.

This keeps routing explicit without requiring direct Pod-to-Pod gRPC. When Kubernetes creates a new WebSocket Pod, that Pod gets a new replica ID, registers its Redis presence and declares its own RabbitMQ delivery queue. When it disappears, the exclusive queue is removed and its Redis presence/session entries expire through TTL.

The Nginx load balancer routes `/ws/` to WebSocket replicas using `least_conn`, disables proxy buffering, forwards the required upgrade headers and uses longer proxy read/send timeouts than normal HTTP traffic. Existing upgraded connections remain attached to the replica that accepted them until disconnect.

The WebSocket process handles SIGINT/SIGTERM, closes active upgraded connections and removes connection registry entries during graceful shutdown.

## Metrics

Prometheus uses Docker DNS service discovery for the scalable Go services:

- `server:8080/metrics`
- `websocket:8080/metrics`
- `publisher-worker:9090/metrics`
- `websocket-worker:9090/metrics`

This keeps each replica visible as an independent Prometheus target.

The first application metrics include HTTP request rate/latency, database-pool pressure, active WebSocket connections, slow-client disconnects, publisher/outbox pressure, Redis-resolved WebSocket sessions and per-replica RabbitMQ routing throughput.

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
docker compose up --build --scale server=3 --scale websocket=3 --scale publisher-worker=2 --scale websocket-worker=2
```

The Nginx load balancer uses Docker DNS and automatic upstream hostname re-resolution for both service pools. New replicas can therefore be discovered without hard-coding container IPs.

Server startup migrations are protected by a PostgreSQL transaction-level advisory lock. If several HTTP replicas start simultaneously, only one applies migrations while the others wait and then observe the already-applied migration records.

WebSocket replicas register their active sessions in Redis with TTL. The WebSocket workers consume the shared notification work queues, resolve the target user's live sessions, group them by replica ID and publish a targeted delivery command to that replica's exclusive RabbitMQ queue. This lets WebSocket and worker replicas scale independently without broadcasting every notification to every WebSocket replica.

## CDC

PostgreSQL starts with logical WAL enabled. Debezium Server uses `pgoutput`, maintains a replication slot named `messager_cdc`, and writes CDC records to Redis Streams.

RabbitMQ is deliberately not used for CDC in this first local stack. It remains the application's processing/event queue layer. This keeps the two flows explicit:

```text
Application events: server/outbox -> publisher workers -> RabbitMQ
Database CDC:       PostgreSQL WAL -> Debezium -> Redis Streams
```

For a production-like exercise, the CDC sink can later be split onto a dedicated streaming system or dedicated Redis instance so cache failures and CDC retention are not coupled.

## Resource limits

The base Compose file remains portable. Optional CPU, memory, PID and writable-layer storage limits live in `docker-compose.resources.yml`:

```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.resources.yml \
  up -d --build
```

`storage_opt.size` depends on Docker storage-driver quota support and limits the container writable layer, not named persistent volumes. If the host storage driver does not support writable-layer quotas, run without this override or configure storage quotas at the host/volume layer.

For a fully constrained load-test run, include the tester-specific storage override as well:

```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.resources.yml \
  -f docker-compose.test.yml \
  -f docker-compose.test.resources.yml \
  up --build load-tester
```

## Kubernetes lab

The `k8s/` directory contains the equivalent core architecture with Services for HTTP, WebSocket and WebSocket-worker metrics, CPU/memory/`ephemeral-storage` requests and limits, Pod-name WebSocket replica IDs, and a separate load-test Job. See `k8s/README.md`.

## Load testing

The load generator is isolated behind the Compose `test` profile, so normal application startup does not create test traffic.

Start the normal stack first:

```bash
docker compose up -d --build
```

Then run the load tester:

```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.test.yml \
  up --build load-tester
```

It gradually increases generated users, HTTP request rate and active WebSocket connections while Prometheus/Grafana collect both injected-load and system metrics.

For all parameters, generated routes and cleanup guidance, see `load_test/README.md`.

## Persistent volumes

PostgreSQL, Redis, RabbitMQ, Prometheus and Grafana use named volumes. To reset the entire lab:

```bash
docker compose down -v
```
