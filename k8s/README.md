# Kubernetes lab

These manifests reproduce the core Messenger runtime in Kubernetes while keeping the same load-test model used by Docker Compose.

> The Secret values in `messenger.yaml` are lab-only credentials for local k3d/k3s experiments. Replace them with proper secret management before using these manifests outside an isolated development environment.

## Architecture

```text
load tester / client
        |
        v
load-balancer Service -> Nginx edge
        |                  |
        | /                | /ws/*
        v                  v
server Service       websocket Service
        |                  |
 server replicas      WebSocket replicas
                           |
                   Redis session registry
                           ^
                           |
publisher-worker -> notification work queues
                           |
                           v
                   websocket-worker replicas
                           |
                           v
                 websocket.delivery exchange
                    /         |         \
                   v          v          v
             replica queue replica queue replica queue
                   |          |          |
                   v          v          v
               WS Pod A   WS Pod B   WS Pod C
```

The Kubernetes Services remain the stable load-balancing layer for HTTP and WebSocket traffic. The Nginx edge separates normal HTTP requests from WebSocket upgrades.

Each WebSocket Pod receives `WEBSOCKET_REPLICA_ID` from `metadata.name` using the downward API. Active connections are registered in Redis as `user:sessions:<user_id>` fields containing the connection ID, Pod/replica ID and an explicit expiration timestamp.

Each WebSocket Pod also creates one exclusive, auto-delete RabbitMQ queue named from its replica ID and refreshes a short-lived Redis replica-presence key.

The publisher worker writes notification events to the shared priority work queues. `websocket-worker` replicas compete for those queues, resolve the target user's current Redis sessions, group connections by replica ID and publish one targeted delivery command per WebSocket replica.

This avoids broadcasting every event to every WebSocket Pod while also avoiding direct Pod-to-Pod gRPC addressing.

## Build local images

For a local k3d/k3s lab:

```bash
docker build -t messenger-server:local ./server
docker build -t messenger-websocket:local ./websocket
docker build -t messenger-publisher-worker:local ./publisher_worker
docker build -t messenger-websocket-worker:local ./websocket_worker
docker build -t messenger-load-tester:local ./load_test
```

For k3d, import the images into the cluster:

```bash
k3d image import \
  messenger-server:local \
  messenger-websocket:local \
  messenger-publisher-worker:local \
  messenger-websocket-worker:local \
  messenger-load-tester:local \
  -c <cluster-name>
```

## Start the architecture

```bash
kubectl apply -f k8s/messenger.yaml
kubectl -n messenger get pods -w
```

The lab manifest starts with:

- 3 HTTP server replicas;
- 3 WebSocket replicas;
- 2 publisher-worker replicas;
- 2 websocket-worker replicas;
- PostgreSQL, Redis and RabbitMQ;
- a Kubernetes Service for each scalable component;
- an Nginx edge exposed through the `load-balancer` Service.

Resource requests and limits include CPU, memory and Kubernetes `ephemeral-storage`.

## Run the load tester

Wait until the application Pods are ready, then:

```bash
kubectl apply -f k8s/load-test.yaml
kubectl -n messenger logs -f job/messenger-load-tester
```

The `load-tester` Service exposes port `9091` while the Job Pod exists so Prometheus can scrape the generator metrics.

A Job Pod template is immutable. After changing test parameters in `k8s/load-test.yaml`, recreate it:

```bash
kubectl -n messenger delete job messenger-load-tester --ignore-not-found
kubectl apply -f k8s/load-test.yaml
```

## Change replica counts

```bash
kubectl -n messenger scale deployment/server --replicas=5
kubectl -n messenger scale deployment/websocket --replicas=5
kubectl -n messenger scale deployment/publisher-worker --replicas=3
kubectl -n messenger scale deployment/websocket-worker --replicas=4
```

Existing WebSocket connections remain attached to the Pod that accepted them. New connections are distributed through the `websocket` Service. Redis tracks which replica owns each live connection. WebSocket workers use that registry to route each notification only to the RabbitMQ queues of replicas that currently host the target user's connections.

## Resource experiments

Edit the `resources.requests` and `resources.limits` blocks in `messenger.yaml` to compare replica count versus per-Pod capacity.

Persistent PVC capacity is separate from `ephemeral-storage`: PostgreSQL, Redis and RabbitMQ data lives on PVCs, while `ephemeral-storage` limits the container writable layer, logs and other node-local temporary storage.
