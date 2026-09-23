# Messenger — Architecture Challenge

Messenger is a deliberately small messaging product used as a laboratory for scalable and distributed-system architecture.

The application domain stays intentionally simple: users belong to companies, companies own chats, users may subscribe to those chats, users can also exchange direct messages, and messages can receive reactions and generate notifications.

The project is not intended to become complicated through business features. Its purpose is to keep a small domain while exposing the same architectural problems that appear in production messaging systems: concurrency, ordering, idempotency, fanout, queues, retries, cache consistency, connection routing, backpressure, partial failures and observability.

## Business model

The domain contains only these main entities:

```text
users
companies
company_users
chats
chat_users
messages
messages_reactions
user_notifications
```

The ownership graph is intentionally direct:

```text
users <------ company_users ------> companies
                                      |
                                      +------ chats
                                              |
                                              +------ chat_users ------> users
                                              |
                                              +------ messages

users <------ direct messages ------> users

messages <------ messages_reactions ------> users
users <------ user_notifications
```

### Users

A user has a single global account and may belong to multiple companies.

Authentication identifies the user. Company permissions are resolved from current application data rather than being treated as permanent authorization state in the authentication token.

### Companies

A company is the tenant and the main business boundary of the application.

A user only sees companies linked to their account through `company_users`.

Company memberships have two roles:

```text
ADMIN
USER
```

An administrator may:

- update company information;
- add or remove company users;
- promote or demote users;
- create company chats.

A normal user may:

- open companies they belong to;
- see company users;
- see company chats;
- subscribe or unsubscribe from chats;
- participate in subscribed chats;
- send direct messages to other users who share a company.

A company must always keep at least one administrator.

### Chats

Every chat belongs to exactly one company.

Chats are not automatically joined by every company user. Participation is represented by `chat_users`.

A user may subscribe to a chat:

```text
user + chat -> chat_users
```

and may later unsubscribe by removing that relationship.

Only subscribed company users may read or publish messages in that chat.

### Messages

A message always has one sender.

A message has exactly one destination mode.

#### Chat message

```text
sender_user_id -> user
chat_id        -> chat
recipient      -> null
direct         -> false
```

The sender must belong to the company and be subscribed to the chat.

#### Direct message

```text
sender_user_id    -> sender
recipient_user_id -> recipient
chat_id           -> null
direct            -> true
```

Direct messaging is allowed only when the two users share at least one active company.

Every message creation has a stable `client_message_id` so retries can be idempotent. A repeated request for the same logical send operation must not create a second message.

Chat messages also have an authoritative sequence inside the chat so concurrent sends converge to a deterministic order.

### Reactions

A user may react to a message.

The same reaction from the same user may exist only once for the same message.

```text
(message_id, user_id, reaction) -> unique
```

### Notifications

Notifications belong directly to users.

Examples include:

- being added to a company;
- receiving a direct message;
- receiving a message in a subscribed chat.

Notifications are derived from business activity. The authoritative message or membership remains the source of truth.

## Main application flows

### Company chat

```text
User opens company
        |
        v
Sees available chats
        |
        v
Subscribes to chat
        |
        v
chat_users relationship created
        |
        v
User can read and send messages
```

### Direct message

```text
User opens company
        |
        v
Sees another company user
        |
        v
Starts direct conversation
        |
        v
Message references sender + recipient directly
```

### Idempotent message creation

```text
Client creates client_message_id
        |
        v
Send request
        |
        v
Response may be lost
        |
        v
Client retries same client_message_id
        |
        v
Existing logical message is reused
```

## Architecture objective

The simplified domain is intentional. The project should become difficult through runtime behavior rather than through additional business modules.

The final architecture is expected to exercise:

- horizontal application replicas;
- load balancing;
- persistent realtime connections;
- database caching;
- transactional outbox;
- publisher workers;
- event queues with at-least-once delivery;
- RabbitMQ fanout to every live WebSocket replica;
- per-replica ephemeral consumer queues;
- retries and backoff;
- dead-letter queues;
- idempotent consumers;
- fanout;
- slow-client handling;
- reconnect storms;
- cursor-based recovery;
- event ordering;
- event-contract versioning;
- strong versus eventual consistency;
- graceful shutdown;
- observability and failure testing.

A useful conceptual flow is:

```text
Client
  |
Load Balancer
  |
Application Server
  |
  +---- normal business tables
  |
  +---- outbox events
             |
             v
      Publisher Workers
             |
             v
  RabbitMQ Fanout Exchange
             |
      +------+------+
      |      |      |
      v      v      v
     WS1    WS2    WS3
      |      |      |
      +-- local connection maps
             |
             v
           Client
```

The database remains the authoritative durable state. Realtime delivery exists to reduce latency, not to replace durable history.

## Important invariants

The system is incorrect if any of the following rules can be violated:

1. A user cannot access a company they are not linked to.
2. Only company admins may modify company data or memberships.
3. A company must retain at least one admin.
4. Every chat belongs to exactly one company.
5. A user cannot read or write a company chat without a chat subscription.
6. Direct-message participants must share a company.
7. A message has exactly one sender.
8. A message is either a chat message or a direct message, never both.
9. A repeated `client_message_id` cannot create duplicate messages.
10. Chat message ordering must remain deterministic under concurrent writes.
11. A deleted message must not reappear because of delayed processing.
12. The same user cannot create the same reaction twice on the same message.
13. Durable business data must remain recoverable after realtime connection loss.
14. Secondary failures must not silently corrupt authoritative business state.

## Architecture challenge scenarios

The project should eventually be tested under conditions such as:

- thousands of simultaneous clients;
- very large company chats;
- one company producing most of the system load;
- concurrent message creation;
- duplicate requests;
- duplicated events;
- out-of-order events;
- queue backlog;
- publisher failures;
- worker failures;
- slow WebSocket clients;
- WebSocket replica crashes;
- mass reconnections;
- stale cache entries;
- database latency;
- cache failures;
- retry storms;
- graceful deployments during active traffic.

The goal is not merely to make users exchange messages. The goal is to preserve the domain invariants while the surrounding distributed system is under pressure or partially failing.

## Completion criteria

Messenger reaches its objective when the product remains logically correct and observable while its architecture is deliberately stressed, degraded and recovered.

The business model should stay small. New complexity should only be introduced when it enables a meaningful architecture experiment.
