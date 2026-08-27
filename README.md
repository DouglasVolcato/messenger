# Messenger

## Multi-Tenant Real-Time Messaging Platform

Messenger is a multi-tenant real-time communication platform designed to reproduce the kinds of problems that appear in large production messaging systems while keeping the product domain intentionally small.

From a user perspective, Messenger is straightforward: companies create workspaces, users join those workspaces, participate in channels or private chats, exchange messages, react to them, and receive notifications.

The challenge of the project does not come from having dozens of unrelated business modules. It comes from making a relatively small messaging domain continue to behave correctly when it is exposed to concurrency, high traffic, large fanout, retries, reconnections, partial failures, uneven load distribution, slow clients, duplicated operations, and eventually consistent data.

The project is therefore intended to be both a usable messaging product and a practical architecture exercise.

---

# 1. Project Goals

Messenger should support the core behavior expected from a modern communication platform:

- companies with multiple workspaces;
- users that may belong to multiple companies and workspaces;
- public and private channels;
- direct conversations and group chats;
- real-time message delivery;
- message history;
- reactions;
- notifications;
- unread message tracking;
- user access control;
- retry-safe message creation;
- deterministic message ordering;
- isolation between tenants;
- graceful behavior under partial failures;
- recovery after connection loss.

The system should remain logically correct even when operating under high concurrency and degraded conditions.

The most important objective is not only to make messaging work under normal conditions.

The objective is to make the messaging model remain correct when many things happen at the same time.

---

# 2. Domain Hierarchy

The main organizational hierarchy is:

```text
Company
  |
  +-- Workspace
        |
        +-- Channel
        |
        +-- Chat
              |
              +-- Message
                    |
                    +-- Reaction
```

Users participate at different levels through explicit relationship entities.

```text
User
  |
  +-- Company User
  |
  +-- Workspace User
  |
  +-- Channel User
  |
  +-- Chat User
```

This separation is intentional.

Being part of a company does not necessarily mean that a user has access to every workspace.

Being part of a workspace does not necessarily mean that a user belongs to every private channel.

Being part of a channel or chat represents a more specific access relationship.

---

# 3. Companies

A **Company** is the highest tenant-level organizational entity.

Examples:

```text
Acme Corporation
Northwind Labs
MegaCorp
```

A company may contain one or many workspaces.

Example:

```text
MegaCorp

├── Engineering
├── Product
├── Operations
└── Internal Events
```

Companies provide a logical ownership boundary for workspaces.

A company may have a very small number of users or may represent a major tenant responsible for a large percentage of the total platform traffic.

This uneven distribution is part of the expected behavior of the project.

---

# 4. Company Users

The relationship between a user and a company is represented by `company_users`.

Conceptually:

```text
User
  |
Company User
  |
Company
```

This relationship answers:

> Does this user belong to this company?

A user may belong to several companies.

Example:

```text
User 100

Company A
Company B
Company C
```

Company membership does not automatically imply access to every workspace.

Workspace access is represented separately.

---

# 5. Workspaces

A **Workspace** is the main collaboration environment inside a company.

Examples:

```text
Engineering
Finance
Product
Support
All Hands
```

Every workspace belongs to exactly one company.

A company may contain many workspaces.

A workspace owns the communication contexts that exist inside it, including channels and chats.

The workspace is also an important isolation boundary.

A user operating inside one workspace must never be able to access protected data from another workspace without a valid membership.

---

# 6. Workspace Users

The relationship between users and workspaces is represented by `workspace_users`.

Conceptually:

```text
User
  |
Workspace User
  |
Workspace
```

This relationship answers:

> Can this user participate in this workspace?

The same user may have access to some workspaces from a company while having no access to others.

Example:

```text
Company: MegaCorp

Douglas
├── Engineering     -> allowed
├── Product         -> allowed
├── Finance         -> not allowed
└── Legal           -> not allowed
```

Workspace membership is therefore more specific than company membership.

Access checks should always respect the current workspace context.

---

# 7. Channels

A **Channel** represents a named communication area inside a workspace.

Examples:

```text
#general
#backend
#incidents
#product
#random
#all-hands
```

Each channel belongs to exactly one workspace.

Channels may represent different visibility rules.

Typical conceptual types are:

```text
PUBLIC
PRIVATE
```

A public channel may be discoverable by workspace members.

A private channel should only be visible to users explicitly allowed to participate in it.

A channel must never expose messages to users who are not authorized to access it.

---

# 8. Channel Users

The relationship between users and channels is represented by `channel_users`.

Conceptually:

```text
User
  |
Channel User
  |
Channel
```

This relationship represents durable membership, not real-time presence.

It answers:

> Is this user a participant in this channel?

This distinction is important.

The fact that a user belongs to a channel is durable business data.

The fact that the same user is currently online is ephemeral operational state.

These two concepts must not be treated as the same thing.

When a user is removed from a private channel, that user must stop receiving new messages from that channel.

---

# 9. Chats

A **Chat** is the common messaging context of the application.

Messages are not divided into separate structures for channels, direct messages, and private groups.

All of them use the same chat abstraction.

Conceptual chat types are:

```text
DIRECT
GROUP
CHANNEL
```

A direct chat represents a conversation between individual users.

```text
Douglas <-> John
```

A group chat represents a private conversation between multiple participants.

```text
Douglas
John
Maria
Pedro
```

A channel chat represents the message stream associated with a channel.

```text
Workspace
  |
Channel
  |
Chat
  |
Messages
```

Every chat belongs to a workspace.

A chat may optionally be associated with a channel.

Conceptually:

```text
DIRECT
channel = none

GROUP
channel = none

CHANNEL
channel = required
```

This allows all messaging behavior to share the same business rules and the same message model.

---

# 10. Chat Users

The relationship between users and chats is represented by `chat_users`.

Conceptually:

```text
User
  |
Chat User
  |
Chat
```

This relationship defines participation in a conversation.

For example:

```text
Chat A
type = DIRECT

Participants:
- Douglas
- John
```

or:

```text
Chat B
type = GROUP

Participants:
- Douglas
- John
- Maria
- Pedro
```

The membership can also represent conversation-specific state such as the user's latest read position.

A user who is no longer allowed to participate in a chat must not continue receiving new messages from it.

---

# 11. Messages

The central business entity of Messenger is `chat_messages`.

Every message belongs to exactly one chat and has exactly one author.

A conceptual message contains information such as:

```text
id
chat_id
user_id
client_message_id
sequence
content
created_at
updated_at
edited_at
deleted_at
```

The message is durable business data.

Once the system confirms that a message has been accepted, that message must remain recoverable even if the real-time connection disappears immediately afterward.

Real-time delivery is a delivery mechanism.

It is not the source of truth for whether a message exists.

---

# 12. Message Idempotency

Every message creation operation should contain a client-generated identifier:

```text
client_message_id
```

This identifier represents the logical send operation.

Example:

```text
client_message_id = ABC
```

Suppose the client sends the message and the system successfully stores it, but the response is lost.

The client does not know whether the operation succeeded and retries:

```text
ABC
ABC
ABC
ABC
```

The result must still be exactly one message.

This rule is fundamental.

Retries are expected behavior and must not create duplicate business effects.

The same principle should be applied whenever an operation may be retried because its previous result is uncertain.

---

# 13. Message Ordering

Messages inside a chat need an official deterministic order.

Consider three users sending messages at nearly the same time:

```text
User A -> "A"
User B -> "B"
User C -> "C"
```

Different clients may observe the requests reaching the system at slightly different moments.

The system must still establish one official sequence.

Example:

```text
1001 -> A
1002 -> C
1003 -> B
```

The exact order is less important than having a single authoritative order.

Eventually, all clients must converge to that order.

The system should not depend only on local timestamps from users to define message ordering.

---

# 14. Message Editing

Users may edit messages according to the project's permission rules.

Example:

```text
Original:
"Deployment starts at 18:00"

Edited:
"Deployment starts at 19:00"
```

Connected participants should eventually receive the updated state.

An older update must never overwrite a newer message state.

For example:

```text
1. MESSAGE_CREATED
2. MESSAGE_EDITED
3. MESSAGE_DELETED
```

If an old edit event arrives after deletion, the message must remain deleted.

---

# 15. Message Deletion

Messages may be deleted by their author or by an authorized user.

Deletion is a state transition.

A deleted message must not reappear because of:

- delayed events;
- retries;
- stale cached data;
- reconnection;
- reprocessing;
- another instance processing older information.

The visible application may display:

```text
[Message removed]
```

or hide the content entirely.

The important invariant is that old information cannot resurrect a newer deleted state.

---

# 16. Reactions

Users may react to messages.

Examples:

```text
👍
❤️
😂
🚀
```

Reactions are represented by `chat_messages_reactions`.

Conceptually:

```text
User
  |
Reaction
  |
Message
```

The same user must not be able to create the exact same reaction more than once for the same message.

Invalid state:

```text
User 100
Message 500

👍
👍
👍
```

Correct state:

```text
User 100
Message 500

👍
```

A popular message may receive thousands of reactions concurrently.

The final state must remain correct even under heavy contention.

---

# 17. Notifications

Notifications are represented by `user_notifications`.

A notification belongs to a user and represents something that should be surfaced to that user.

Examples:

- a direct message was received;
- the user was mentioned;
- a relevant channel received an important message;
- the user was added to a conversation;
- an administrative action affected the user.

Notifications are secondary to message persistence.

If notification processing is temporarily unavailable, message creation should continue whenever the core message path is healthy.

The notification can be created later from the durable event that represents the original operation.

---

# 18. Data Model

The current project data model intentionally remains small.

The main entities are:

```text
users

companies
company_users

workspaces
workspace_users

channels
channel_users

chats
chat_users

chat_messages
chat_messages_reactions

user_notifications
```

The relationships can be summarized as:

```text
users
  |
  +---- company_users ---------- companies
  |
  +---- workspace_users -------- workspaces
  |
  +---- channel_users ---------- channels
  |
  +---- chat_users ------------- chats
  |
  +---- chat_messages
  |
  +---- chat_messages_reactions
  |
  +---- user_notifications
```

And the organizational hierarchy is:

```text
companies
   |
   +---- workspaces
            |
            +---- channels
            |
            +---- chats
                    |
                    +---- chat_messages
                              |
                              +---- chat_messages_reactions
```

A channel belongs to a workspace.

A chat belongs to a workspace.

A channel-based chat may also reference a channel.

This model makes it possible to support direct chats, group chats, and channel messaging without duplicating the message infrastructure.

---

# 19. Data Ownership

Every entity must have a clear ownership path.

For example:

```text
Message
  |
Chat
  |
Workspace
  |
Company
```

This hierarchy allows the system to determine the tenant context of every message.

The same principle applies to channels and conversation memberships.

Tenant isolation is a fundamental business invariant.

A request from one workspace must never accidentally access data from another workspace because of an incomplete filter or an incorrect relationship lookup.

---

# 20. Durable and Ephemeral State

Messenger deliberately contains two different classes of state.

## Durable state

Durable state represents business facts that must survive failures.

Examples:

```text
MESSAGE_CREATED
MESSAGE_EDITED
MESSAGE_DELETED
REACTION_ADDED
REACTION_REMOVED
USER_ADDED_TO_CHAT
USER_REMOVED_FROM_CHAT
USER_ADDED_TO_CHANNEL
USER_REMOVED_FROM_CHANNEL
```

These operations affect the real business state.

They must not disappear silently.

## Ephemeral state

Ephemeral state is useful only for a short period.

Examples:

```text
USER_IS_TYPING
USER_STOPPED_TYPING
USER_ONLINE
USER_AWAY
```

If one of these events is lost, the business history is still correct.

This difference is central to the project.

Not every event needs the same reliability guarantees.

---

# 21. Real-Time Delivery

Users should receive new activity with minimal delay while connected.

Examples:

- new messages;
- edits;
- deletions;
- reactions;
- notifications;
- typing indicators;
- membership changes.

However, a live connection is not considered permanent storage.

If the client disconnects, it must later be able to recover durable state from the authoritative history.

This leads to an important rule:

> Real-time delivery improves latency, but durable state guarantees correctness.

---

# 22. Reconnection

Clients may disconnect at any moment.

Example:

```text
Client disconnects.

During the next 20 seconds:

Message 100
Message 101
Message 102
Message 103

Client reconnects.
```

The client must be able to identify what changed while it was offline and converge to the latest valid state.

The system must not assume that every durable event has been delivered live exactly once.

Reconnection is therefore a normal workflow, not an exceptional situation.

---

# 23. Slow Clients and Backpressure

Different clients consume events at different speeds.

Example:

```text
Client A
500 events/second

Client B
3 events/second
```

The slow client must not force the entire platform to accumulate unlimited pending work.

Ephemeral information may be dropped when it becomes obsolete.

For example, if several typing events are queued, an old typing event may no longer matter.

Durable changes, however, must remain recoverable through synchronization.

This distinction allows the system to protect itself from slow consumers without losing important business state.

---

# 24. Fanout

A message may need to reach only one other user or tens of thousands of connected participants.

Example:

```text
Direct chat
2 users
```

Compared with:

```text
#global
500,000 members
100,000 online
```

The same messaging model must support both cases.

Large fanout must not make small conversations unusable.

A very large channel therefore becomes an intentional architectural stress case.

---

# 25. Uneven Traffic Distribution

Messenger should not assume that traffic is evenly distributed.

A realistic environment may look like:

```text
90% of chats
fewer than 20 participants

9% of chats
20 to 5,000 participants

0.9% of chats
5,000 to 100,000 participants

0.1% of chats
more than 100,000 participants
```

Likewise, one company may generate most of the traffic.

Example:

```text
MegaCorp = 60% of total traffic

Company A = 5%
Company B = 2%
Company C = 1%
```

A whale tenant must not make all smaller tenants unusable.

This unevenness intentionally creates hotspots.

---

# 26. Celebrity Channel

The project should include at least one extremely large channel.

Example:

```text
#global

500,000 members
100,000 connected users
```

A single message may trigger:

```text
100,000 real-time deliveries
50,000 reactions
10,000 replies
thousands of notification or unread updates
```

The rest of the system must remain usable while this happens.

This scenario exercises fanout, hotspots, contention, queue growth, load isolation, and backpressure simultaneously.

---

# 27. Retry Behavior

Retries are expected.

A client may retry because:

- a request timed out;
- the connection was interrupted;
- the response was lost;
- the client restarted;
- an intermediate component failed.

The server must treat retries according to the identity of the logical operation.

For message creation:

```text
client_message_id = ABC
```

must always represent the same send operation.

A retry must not create a new message merely because the transport request is new.

---

# 28. Duplicate Events

Distributed processing may produce duplicate delivery attempts.

A consumer must therefore be able to receive the same durable event multiple times without corrupting the final state.

Example:

```text
REACTION_ADDED
REACTION_ADDED
REACTION_ADDED
```

for the same logical reaction must not produce three identical reactions.

The same principle applies to message creation and other idempotent operations.

---

# 29. Out-of-Order Events

Events may occasionally be processed in a different order from the order in which they were originally generated.

Example:

```text
MESSAGE_CREATED
MESSAGE_EDITED
MESSAGE_DELETED
```

A consumer may temporarily observe:

```text
MESSAGE_CREATED
MESSAGE_DELETED
MESSAGE_EDITED
```

The final state must still be:

```text
DELETED
```

not:

```text
EDITED
```

The business model must protect itself from stale transitions.

---

# 30. Strong Consistency

Some operations require immediately trustworthy state from the user's point of view.

Examples:

- message creation confirmation;
- message deletion;
- removal from a private channel;
- removal from a chat;
- permission changes affecting access.

If the system confirms one of these operations, the user must not immediately observe a contradictory previous state through the authoritative path.

---

# 31. Eventual Consistency

Other information may converge with a small delay.

Examples:

- online user counts;
- unread counters;
- notification counts;
- analytics;
- activity dashboards;
- popularity rankings.

Temporary divergence is acceptable if the data eventually converges to the correct state.

Messenger should intentionally distinguish data that requires strict correctness from data that can tolerate delayed convergence.

---

# 32. Partial Failures

Not every subsystem has the same criticality.

For example:

```text
Notifications unavailable
-> messages should continue
```

```text
Analytics unavailable
-> messages should continue
```

```text
Presence unavailable
-> messages should continue
```

```text
Message persistence unavailable
-> the system must not claim that a message was successfully created
```

The platform should degrade according to the failed capability rather than treating every partial failure as a total outage.

---

# 33. Reprocessing

Some derived functionality may temporarily stop consuming events.

Example:

```text
Notification processing is unavailable for 30 minutes.
```

Once it returns, it may need to process durable historical events from that period.

Reprocessing must not duplicate the original business operation.

Replaying:

```text
MESSAGE_CREATED
```

must not create the message again.

The project should make a clear distinction between:

- creating the original business state;
- processing consequences derived from that state.

---

# 34. Rate Limiting and Abuse

The system must assume that some users or tenants may generate abusive traffic.

Examples:

- sending messages in a tight loop;
- repeatedly reconnecting;
- rapidly adding and removing reactions;
- issuing invalid operations;
- attempting unauthorized access;
- generating large bursts of requests.

The damage caused by one abusive user or tenant should be isolated as much as possible.

Rate limits may conceptually exist at different scopes:

```text
company
workspace
user
chat
operation type
```

The exact limits are not the important part.

The important part is that one source of abusive load should not automatically consume all available capacity.

---

# 35. Access Control Rules

Access control must follow the organizational hierarchy.

A user may interact with a workspace only if the user has the required workspace membership.

A user may interact with a protected channel only if the channel access rules allow it.

A user may interact with a chat only if the user participates in that chat or has an administrative permission that explicitly allows the operation.

Examples of invalid behavior:

```text
User from Workspace A
reads Chat from Workspace B
```

```text
User removed from private Channel X
continues receiving new messages from Channel X
```

```text
User outside a private group
retrieves its message history
```

These are not merely application bugs.

They violate the core tenant and privacy invariants of Messenger.

---

# 36. Notifications as Derived State

A notification should generally be treated as a consequence of another event.

Example:

```text
MESSAGE_CREATED
      |
      +-- message persisted
      |
      +-- realtime delivery
      |
      +-- notification may be generated
```

The notification is not more important than the original message.

If notification processing fails temporarily, the message remains valid.

This is one of the simplest places in the project to practice separation between the critical path and derived asynchronous work.

---

# 37. Logical Architecture

Messenger can be understood as several logical responsibilities.

No specific technology is required to understand this architecture.

## Client interaction

Receives user commands such as:

```text
send message
edit message
delete message
react
join chat
leave chat
```

## Core domain

Validates:

```text
tenant ownership
membership
permissions
idempotency
message ordering
business invariants
```

## Durable state

Stores the authoritative business state:

```text
companies
workspaces
channels
chats
messages
reactions
notifications
memberships
```

## Real-time distribution

Propagates changes to currently connected clients.

## Event processing

Handles secondary consequences such as:

```text
notifications
derived counters
search indexing
analytics
auditing
```

## Recovery

Allows clients and consumers to catch up after missing durable events.

These responsibilities may operate independently while still participating in the same end-to-end message journey.

---

# 38. Critical Path vs Secondary Work

One of the most important architectural decisions in Messenger is identifying what must complete before a message can be confirmed.

The critical business fact is:

```text
The message exists in the authoritative state.
```

Other actions may be consequences:

```text
deliver to connected clients
create notifications
update counters
update analytics
update search
```

A failure in a secondary consequence must not retroactively invalidate a message that was successfully accepted.

This allows the project to exercise failure isolation and asynchronous processing without adding unrelated business domains.

---

# 39. Message Journey

A typical successful message journey is conceptually:

```text
User sends command
      |
validate identity
      |
validate workspace/chat access
      |
validate idempotency
      |
assign official ordering
      |
persist message
      |
confirm authoritative creation
      |
publish consequences
      |
deliver to connected participants
      |
update derived state
```

A retry may enter the same journey again.

If the original operation was already completed, the system must return the existing result instead of creating another message.

---

# 40. Failure During Message Creation

Consider:

```text
1. User sends message.
2. Message is successfully accepted.
3. Client connection disappears before confirmation arrives.
4. Client reconnects.
5. Client retries the same client_message_id.
```

Expected result:

```text
Exactly one message exists.
```

The client should eventually discover that the original operation succeeded.

This is one of the central failure scenarios of the project.

---

# 41. Failure During Fanout

Consider:

```text
Message is stored successfully.
```

Then real-time distribution partially fails.

Some connected users receive the message immediately.

Others do not.

The message itself is still valid.

Clients that missed the live event must recover it through synchronization or history.

The system must never equate:

```text
not delivered live
```

with:

```text
message does not exist
```

---

# 42. Failure During Notification Processing

Consider:

```text
Message is stored.
Realtime delivery works.
Notification processor is unavailable.
```

The conversation must continue working.

Once notification processing returns, it can derive the missed notifications from durable events when appropriate.

This scenario trains recovery and reprocessing without affecting the core message path.

---

# 43. Mass Reconnection Scenario

A large group of clients may disconnect at once.

Example:

```text
20,000 clients disconnect.
```

Seconds later:

```text
20,000 clients attempt to reconnect.
```

At the same time:

```text
new messages continue arriving.
```

The recovery process must not create a failure larger than the original disconnect.

This scenario exercises thundering-herd behavior and load isolation.

---

# 44. Slow Consumer Scenario

Consider a highly active chat.

```text
Fast client:
500 events/second

Slow client:
2 events/second
```

The slow client must not create an unbounded backlog inside the live delivery path.

Durable messages remain available through history.

Ephemeral events may be dropped when they are no longer useful.

---

# 45. High-Contention Reaction Scenario

A viral message receives:

```text
50,000 reactions
```

within a few seconds.

Many users may react at almost the same time.

The system must preserve:

- reaction uniqueness;
- correct message ownership;
- tenant isolation;
- eventual counter correctness.

Other chats must remain responsive.

---

# 46. Whale Tenant Scenario

A single company may generate a disproportionate amount of traffic.

Example:

```text
MegaCorp
60% of total platform traffic
```

At the same time, many smaller companies continue using the platform.

The large tenant must not permanently starve smaller tenants.

This scenario forces the architecture to deal with unfair load distribution instead of only balanced synthetic traffic.

---

# 47. Combined Stress Scenario

The final project scenario should combine several problems.

Example:

```text
100,000 connected clients

MegaCorp generates 60% of traffic

#global contains 500,000 members

one message becomes viral

50,000 reactions arrive

10,000 messages are created in a short interval

20,000 clients reconnect

some clients are extremely slow

duplicate requests appear

some events arrive out of order

notification processing is delayed

another secondary consumer is temporarily unavailable
```

While all of this happens:

```text
direct chats must still work
private channels must remain private
message order must remain deterministic
retries must remain idempotent
deleted messages must remain deleted
tenant isolation must remain intact
```

This is the scenario that defines whether the project has reached its architectural objective.

---

# 48. Core Business Invariants

The following rules are the laws of Messenger.

If one of them is violated, the system is incorrect.

1. The same logical message operation must not create more than one message.
2. Every message belongs to exactly one chat.
3. Every chat belongs to exactly one workspace.
4. Every workspace belongs to exactly one company.
5. A user must not access protected data from a workspace without authorization.
6. A user removed from a private channel must not continue receiving new protected messages from it.
7. A user removed from a private chat must not continue receiving new messages from it.
8. Message ordering inside a chat must have one authoritative sequence.
9. All clients must eventually converge to the same authoritative message order.
10. An old edit must never overwrite a newer message state.
11. A deleted message must not reappear because of stale or delayed processing.
12. The same user must not create the same reaction more than once for the same message.
13. Durable business events must not disappear silently.
14. Ephemeral events may be lost without corrupting durable business state.
15. A slow client must not degrade all fast clients indefinitely.
16. A missed live event must remain recoverable when it represents durable state.
17. Reprocessing must not duplicate the original business operation.
18. Secondary subsystem failures must not unnecessarily invalidate successful message creation.
19. A large tenant must not permanently monopolize the entire platform.
20. Derived data may be eventually consistent but must converge to a valid state.
21. A message must never be confirmed as successfully created if the authoritative state did not accept it.
22. Retry behavior must be safe even when the result of a previous attempt is unknown.
23. Tenant ownership must remain traceable through the complete data hierarchy.
24. Membership changes affecting access must eventually invalidate stale authorization state.
25. High concurrency must not break uniqueness rules.

---

# 49. Concepts Practiced by the Project

Messenger is intentionally designed to exercise the following architecture concepts in one product.

## Multi-tenancy

Companies and workspaces create isolation boundaries with uneven tenant sizes.

## Persistent real-time connections

Users remain connected while receiving messages and events.

## Fanout

One message may need to reach tens of thousands of clients.

## Backpressure

Slow clients must not create unlimited queues.

## Idempotency

Retries must not duplicate messages or other business effects.

## Ordering

Concurrent message creation must converge to an authoritative sequence.

## Strong consistency

Critical access and message operations require trustworthy authoritative state.

## Eventual consistency

Counters, presence, analytics, and similar derived information may converge later.

## Hotspots

Large channels and whale tenants create intentionally uneven traffic.

## Load isolation

Heavy or abusive actors must not automatically affect every other tenant.

## Durable vs ephemeral events

Different event classes require different reliability guarantees.

## Partial failure

Secondary capabilities may fail without stopping core messaging.

## Retry safety

Unknown operation outcomes must remain safe to retry.

## Reconnection recovery

Clients must recover missed durable state after losing the live connection.

## Duplicate processing

Consumers must tolerate duplicate event delivery.

## Out-of-order processing

Stale events must not corrupt newer state.

## Reprocessing

Derived consumers must be able to catch up from historical durable events.

## High contention

Viral messages may create large concurrent reaction and delivery workloads.

## Observability

A message journey should be traceable across validation, persistence, event propagation, real-time distribution, and derived processing.

## Capacity and scaling decisions

The same product must support both tiny conversations and extremely large communication spaces.

---

# 50. What Makes the Project Challenging

Messenger is intentionally small from a product perspective.

The difficulty does not come from implementing dozens of business modules.

The difficulty comes from answering questions such as:

- What happens if the response to a successful message creation is lost?
- What happens if two users send messages at the same time?
- What happens if 100,000 users need the same event?
- What happens if one connected client stops consuming data?
- What happens if a large number of clients reconnect simultaneously?
- What happens if the same event is processed twice?
- What happens if an older event is processed after a newer event?
- What happens if notifications are unavailable?
- What happens if one company generates most of the platform traffic?
- What happens if authorization changes while a user is still connected?
- What happens if a client misses real-time delivery?
- What happens if a viral message receives tens of thousands of concurrent reactions?

These are the problems the project is intended to train.

---

# 51. Completion Criteria

Messenger should not be considered complete merely because two users can exchange messages.

The project reaches its objective when the business invariants continue to hold under scenarios involving:

- high concurrent message volume;
- many simultaneous connected users;
- giant channels;
- whale tenants;
- duplicate requests;
- delayed responses;
- retries;
- slow clients;
- mass reconnections;
- duplicated events;
- out-of-order events;
- partial subsystem failures;
- delayed secondary processing;
- reprocessing;
- high reaction contention;
- permission changes;
- private channels;
- direct conversations;
- uneven traffic distribution.

The final system should be simple to understand as a product but difficult to break as a distributed system.

That is the central purpose of Messenger.
