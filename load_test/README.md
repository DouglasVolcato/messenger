# Messenger load tester

The tester is a dedicated Go program for exercising the Messenger architecture with gradually increasing HTTP traffic, generated accounts and long-lived WebSocket connections.

It intentionally uses the public application path through Nginx instead of calling the HTTP server or WebSocket replicas directly.

> Run it only against an isolated test environment. It creates real users, a company, a chat, memberships, messages, reactions and notifications in the configured database.

## What it currently exercises

During setup and load phases the tester uses these application flows:

| Operation | Route |
| --- | --- |
| account registration | `POST /api/auth/register` |
| authentication | `POST /api/auth/login` |
| company creation | `POST /api/companies` |
| company membership | `POST /api/companies/{companyID}/members` |
| chat creation | `POST /api/companies/{companyID}/chats` |
| chat subscription | `POST /api/companies/{companyID}/chats/{chatID}/subscribe` |
| company list | `GET /companies` |
| chat read | `GET /companies/{companyID}/chats/{chatID}` |
| chat message | `POST /api/chats/{chatID}/messages` |
| direct message | `POST /api/messages/users/{userID}` |
| reaction | `POST /api/messages/reactions/{messageID}` |
| edit own chat message | `PATCH /api/messages/{messageID}` |
| delete own chat message | `DELETE /api/messages/{messageID}` |
| notification list | `GET /notifications` |
| mark notifications read | `POST /api/notifications/read-all` |
| WebSocket connection | `GET /ws/notifications` with HTTP Upgrade |

The initial workload does not mutate profile/security settings, remove company members or change admin roles. Those are low-frequency administrative operations and are intentionally left out of the first scalability workload.

## Generated topology

The first generated account becomes the test company administrator. The tester then creates one company and one chat.

Every additional generated account is:

1. registered through the application;
2. added to the generated company;
3. subscribed to the generated chat;
4. made available for random direct/chat traffic.

The first chat is deliberately shared by all generated users. This already creates a useful fanout/hot-chat workload without adding domain complexity.

## Gradual load model

The tester grows three independent dimensions on every phase:

- generated users;
- target HTTP requests per second;
- active WebSocket connections.

Defaults:

| Setting | Start | Step | Maximum |
| --- | ---: | ---: | ---: |
| users | 10 | +10 | 100 |
| HTTP target RPS | 5 | +5 | 50 |
| WebSockets | 5 | +10 | 100 |

Each phase lasts 30 seconds by default.

A phase therefore looks like:

```text
phase 0 ->  10 users,  5 RPS,   5 WebSockets
phase 1 ->  20 users, 10 RPS,  15 WebSockets
phase 2 ->  30 users, 15 RPS,  25 WebSockets
...
```

The test stops after all configured maxima have been exercised for one complete phase.

Account creation happens while moving into a phase. Normal mixed request traffic then runs for the configured phase duration.

## Mixed request distribution

The initial workload distribution is intentionally simple:

```text
40% chat messages
20% direct messages
10% login
 8% chat reads
 5% company-list reads
 4% notification reads
 3% mark notifications read
 5% reactions
 3% edits
 2% deletes
```

This is only the baseline. Later experiments can split these into dedicated scenarios such as hot-chat fanout, reconnect storms, login saturation, slow clients or cache stampedes.

## Running with Docker Compose

Start the normal architecture first:

```bash
docker compose up -d --build
```

Optionally scale the application before the test:

```bash
docker compose up -d \
  --scale server=3 \
  --scale websocket=3 \
  --scale publisher-worker=2 \
  --scale websocket-worker=2
```

Then start the tester profile:

```bash
docker compose --profile test up --build load-tester
```

The tester exits when the final phase finishes.

Prometheus and Grafana should already be running from the normal Compose stack. While the tester is alive Prometheus discovers it on port `9091`.

## Configuring the test

All common parameters can be set in the shell or repository `.env`:

```env
TEST_USER_PASSWORD=LoadTest123!

TEST_USERS_START=10
TEST_USERS_MAX=100
TEST_USERS_STEP=10

TEST_RPS_START=5
TEST_RPS_MAX=50
TEST_RPS_STEP=5

TEST_WS_START=5
TEST_WS_MAX=100
TEST_WS_STEP=10

TEST_PHASE_DURATION=30s
TEST_MAX_CONCURRENCY=256
```

Example of a larger run:

```bash
TEST_USERS_START=100 \
TEST_USERS_MAX=5000 \
TEST_USERS_STEP=500 \
TEST_RPS_START=50 \
TEST_RPS_MAX=1000 \
TEST_RPS_STEP=100 \
TEST_WS_START=100 \
TEST_WS_MAX=5000 \
TEST_WS_STEP=500 \
TEST_PHASE_DURATION=2m \
docker compose --profile test up --build load-tester
```

Increase these values gradually. The tester can become the bottleneck if `TEST_MAX_CONCURRENCY` or the machine running it is too small.

## Command-line flags

The same settings are available as flags:

```text
-base-url
-ws-url
-metrics-address
-password

-users-start
-users-max
-users-step

-rps-start
-rps-max
-rps-step

-ws-start
-ws-max
-ws-step

-phase-duration
-max-concurrency
-seed
```

For direct execution from this directory:

```bash
cd load_test
go run ./cmd/tester \
  -base-url http://localhost:8088 \
  -ws-url ws://localhost:8088/ws/notifications \
  -users-max 200 \
  -rps-max 100 \
  -ws-max 200
```

That requires Nginx to be published on the chosen host port. The default project Compose keeps infrastructure ports private, so running the tester as the Compose service is the normal path.

## Tester metrics

The tester exposes Prometheus metrics on port `9091`:

```text
load_tester_active_users
load_tester_target_rps
load_tester_phase

load_tester_http_requests_total
load_tester_http_request_duration_seconds

load_tester_websocket_connections_active
load_tester_websocket_connections_total
load_tester_websocket_connection_failures_total
load_tester_websocket_messages_received_total

load_tester_generated_messages_total
load_tester_errors_total
```

This is important because Grafana can correlate what the generator is sending with what the system is consuming.

For example:

```text
target RPS increases
        |
        +--> HTTP p95
        +--> server CPU
        +--> DB pool usage
        +--> outbox age
        +--> RabbitMQ backlog
        +--> worker throughput
```

## Grafana

A new provisioned dashboard is included:

```text
Messenger - Load Testing
```

It contains the initial views for:

- injected target RPS;
- generated users;
- tester and server WebSocket connection counts;
- healthy replica counts;
- request rate by operation;
- tester-observed HTTP p95;
- server-side HTTP p95 by route;
- WebSocket connections per replica;
- CPU by Compose service;
- memory by Compose service;
- network RX/TX by Compose service;
- publisher outbox backlog and oldest-event age;
- WebSocket worker throughput/failures;
- RabbitMQ backlog;
- server DB pool usage;
- host CPU.

## Resource collection

The Compose stack now includes:

- cAdvisor for per-container CPU, memory and network metrics;
- node-exporter for host metrics;
- PostgreSQL exporter;
- Redis exporter;
- Nginx exporter;
- RabbitMQ native Prometheus endpoint;
- application metrics from the Go services.

The application services are scraped separately through Docker DNS, so scaled replicas remain visible individually.

## Resetting test data

The generated accounts and messages are normal application data.

For a disposable local lab, the simplest full reset is:

```bash
docker compose down -v
docker compose up -d --build
```

This deletes all named volumes, including PostgreSQL, Redis, RabbitMQ, Prometheus and Grafana data.

Do not run that command against an environment whose persistent data you need.

## Current scope and next experiments

This first tester is intended to establish a repeatable baseline. It does not yet try to model every production behavior.

Useful next scenarios are:

- WebSocket reconnect storm with and without jitter;
- one very hot chat with thousands of subscribed users;
- slow WebSocket consumers;
- login-only CPU saturation;
- cache-hit/cache-miss comparison;
- RabbitMQ consumer slowdown;
- publisher outage and outbox accumulation;
- PostgreSQL connection exhaustion;
- replica removal during active traffic;
- end-to-end message delivery latency from HTTP send to WebSocket receipt.

Those should be added as explicit scenarios rather than making the baseline workload progressively harder to understand.
