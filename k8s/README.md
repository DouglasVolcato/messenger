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
                    one exclusive RabbitMQ
                    fanout queue per replica
```

The Kubernetes Services are the stable load-balancing layer for the HTTP and WebSocket replica sets. The Nginx edge only separates normal HTTP traffic from WebSocket upgrades.

Realtime notifications are published once to the durable `notifications.realtime` fanout exchange. Every live WebSocket replica has its own exclusive, auto-delete queue. Each replica consumes every realtime event and only sends it to matching users connected locally.

## Build local images

For a local k3d/k3s lab:

```bash
docker build -t messenger-server:local ./server
docker build -t messenger-websocket:local ./websocket
docker build -t messenger-publisher-worker:local ./publisher_worker
docker build -t messenger-load-tester:local ./load_test
```

For k3d, import the images into the cluster:

```bash
k3d image import \
  messenger-server:local \
  messenger-websocket:local \
  messenger-publisher-worker:local \
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
```

Existing WebSocket connections remain attached to the Pod that accepted them. New connections are distributed through the `websocket` Service. RabbitMQ fanout means every currently live WebSocket Pod receives each realtime notification event, so a user may receive it on multiple devices/connections even when those connections live on different replicas.

## Resource experiments

Edit the `resources.requests` and `resources.limits` blocks in `messenger.yaml` to compare replica count versus per-Pod capacity.

Persistent PVC capacity is separate from `ephemeral-storage`: PostgreSQL, Redis and RabbitMQ data lives on PVCs, while `ephemeral-storage` limits the container writable layer, logs and other node-local temporary storage.
