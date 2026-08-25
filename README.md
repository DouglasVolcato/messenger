# Messager — Multi-Tenant Real-Time Messaging Platform

## Overview

**Messager** is a multi-tenant real-time messaging platform inspired by products like Slack, Discord and WhatsApp.

The objective of the project is to allow training, in a single system that is relatively simple to implement, of the main problems encountered in distributed production applications:

- large-scale persistent connections;
- fanout;
- backpressure;
- idempotence;
- ordering of events;
- concurrency;
- distributed sessions;
- strong and eventual consistency;
- cache;
- hotspots;
- multi-tenancy;
- asynchronous processing;
- reprocessing;
- rate limiting;
- load isolation;
- partial failures;
- reconnection;
- observability;
- high concurrency;
- ephemeral and durable events.

The domain must be purposely smaller than that of a logistics or financial platform.

The main complexity should arise from scale, concurrency and distribution, not the number of business rules.

The entire system must be able to exist in a local environment, without relying on external APIs or commercial services.

---

# 1. Functional objective

Messager should allow companies to create workspaces where users can:

- participate in channels;
- chat individually;
- create private groups;
- send messages;
- edit messages;
- delete messages;
- react to messages;
- respond in threads;
- track unread messages;
- view presence;
- receive notifications;
- use multiple devices simultaneously;
- track delivery and reading confirmations;
- reconnect and recover lost events;
- search for messages;
- browse history;
- manage members and channels.

The system must remain functional even when subjected to:

- high load;
- large number of simultaneous connections;
- slow clients;
- unavailable components;
- delayed processing;
- retries;
- duplicate events;
- events out of order;
- bulk reconnections;
- workspaces of extremely different sizes;
- channels with hundreds of thousands of participants.

---

# 2. Multi-tenancy

The main unit of system isolation is the **Workspace**.

Examples:

```text
Empresa A
Empresa B
Startup X
MegaCorp
```

Each workspace has:

- members;
- channels;
- conversations;
- messages;
- permissions;
- notifications;
- settings;
- metrics;
- own limits.

A user can join multiple workspaces.

Example:

```text
User 10

Workspace A → OWNER
Workspace B → MEMBER
Workspace C → GUEST
```

Isolation is mandatory.

A workspace can never:

- view members of another workspace without authorization;
- consult messages from another workspace;
- discover private channels from another workspace;
- access user sessions from another workspace;
- interfere with the operational processing of another workspace beyond normal infrastructure sharing.

---

# 3. Users

The user represents a global identity on the platform.

A user can join multiple workspaces.

Conceptual data:

```text
id
name
username
status
created_at
```

Possible presence states:

```text
ONLINE
AWAY
OFFLINE
```

Presence is an operational data and may have eventual consistency.

A user who has just gone offline may continue to appear as online for a short interval.

This is acceptable as long as the state subsequently converges.

---

# 4. Memberships

The relationship between a user and a workspace must be represented separately.

Conceptually:

```text
User
 ↓
WorkspaceMembership
 ↓
Workspace
```

Membership defines the user's context within that workspace.

Possible roles:

```text
OWNER
ADMIN
MEMBER
GUEST
```

The same user can have different roles in different workspaces.

Example:

```text
User 100

MegaCorp   → ADMIN
Startup X  → MEMBER
Empresa Y  → GUEST
```

Removing a membership should prevent the user from accessing new data from that workspace.

---

# 5. Workspaces

Each workspace represents an independent organization.

Possible conceptual attributes:

```text
id
name
slug
status
plan
created_at
```

Possible states:

```text
ACTIVE
SUSPENDED
DISABLED
```

A suspended workspace must not allow new business operations until it is reactivated.

---

# 6. Plans and limits

Each workspace has a dummy plan.

Example:

```text
FREE
PRO
ENTERPRISE
```

Plans may establish limits for:

- messages per minute;
- users;
- channels;
- simultaneous connections;
- search calls;
- history size;
- creation of conversations;
- reactions;
- reconnections;
- administrative operations.

Example:

```text
FREE
100 messages/minute

PRO
10.000 messages/minute

ENTERPRISE
1.000.000 messages/minute
```

Excessive use must primarily affect the responsible workspace.

---

# 7. Sessions

A user can have multiple simultaneous sessions.

Example:

```text
User 100

Chrome
Android
Desktop
Tablet
```

Each session must have its own identity.

Conceptually:

```text
UserSession

id
user_id
device
status
created_at
last_seen_at
revoked_at
```

States:

```text
ACTIVE
REVOKED
EXPIRED
```

If a session is revoked, they should quickly lose access.

---

# 8. Multiple devices

The same user can be logged in on more than one device.

When he performs an action on one device, the others should eventually reflect the same change.

Example:

```text
Android
 ↓
sends message
 ↓
Chrome receives update
Desktop receives update
Tablet receives update
```

The system must consider sessions from the same user as independent clients.

---

# 9. Channels

Workspaces can have channels.

Types:

```text
PUBLIC
PRIVATE
```

Examples:

```text
#geral
#backend
#produto
#incidentes
#random
```

Public channels are discoverable by authorized workspace members.

Private channels can only be viewed by their members.

---

# 10. Channel Membership

Participation in private channels must be controlled.

Conceptually:

```text
Channel
 ↓
ChannelMembership
 ↓
WorkspaceMembership
```

Possible states:

```text
ACTIVE
REMOVED
```

A user removed from a private channel cannot continue receiving new messages from that channel.

---

# 11. Direct conversations

Users can also have private conversations.

Types:

```text
DIRECT
GROUP
```

### Direct conversation

```text
Douglas ↔ John
```

### Group chat

```text
Douglas
John
Maria
Pedro
```

A private conversation has an explicit list of participants.

---

# 12. Conversations as a common concept

Channels and private conversations can be conceptually treated as contexts in which messages are published.

A message must belong to exactly one logical conversation.

This conversation can represent:

- public channel;
- private channel;
- direct conversation;
- private group.

---

# 13. Messages

The central entity of Messager is the **Message**.

Conceptually:

```text
Message

id
conversation_id
author_membership_id
client_message_id
type
content
created_at
edited_at
deleted_at
```

Initial types:

```text
TEXT
SYSTEM
```

The system may later support other types, but the core of the project must work only with textual messages.

---

# 14. Message status

A message can have conceptual states:

```text
CREATED
EDITED
DELETED
```

The current state does not necessarily eliminate the history.

A deleted message may continue to exist for auditing purposes.

---

# 15. Send idempotency

Every message sent must have an identifier generated by the client:

```text
client_message_id
```

Example:

```text
client_message_id = 019A82F...
```

If the customer repeats the same operation:

```text
send message
client_message_id = ABC
```

ten times, the result should still be a single message.

This rule applies even if:

- the first request has been processed;
- the answer has been lost;
- the client has expired its timeout;
- the client has reconnected;
- the same operation is carried out by different instances of the application.

---

# 16. Local Message State

The client may present temporary statuses before definitive confirmation.

Example:

```text
ENVIANDO
 ↓
ENVIADA
```

or:

```text
ENVIANDO
 ↓
FALHA
```

A message presented locally should not be considered officially created until the server confirms the operation.

---

# 17. Ordering messages

Every conversation must have an official message order.

Example:

```text
User A sends "A"
User B sends "B"
User C sends "C"
```

almost simultaneously.

All clients must eventually converge to a single order:

```text
1001 A
1002 C
1003 B
```

It is not mandatory that the official order corresponds exactly to each customer's temporal perception.

It is mandatory that there is a deterministic order accepted by the system.

---

# 18. Delayed events

An old event cannot overwrite a newer state.

Example:

```text
1 MESSAGE_CREATED
2 MESSAGE_EDITED
3 MESSAGE_DELETED
```

If a consumer processes again:

```text
MESSAGE_EDITED
```

after:

```text
MESSAGE_DELETED
```

the message cannot reappear.

---

# 19. Editing messages

The author can edit messages when they have permission.

Example:

```text
Original:
"deploy will occur at 18"

Edited:
"deploy will occur at 19"
```

All interested customers should eventually view the current version.

An old version can never replace a newer version.

---

# 20. Version history

The system can preserve message versions.

Example:

```text
Message 100

Version 1
"deploy at 18"

Version 2
"deploy at 19"

Version 3
"deploy at 20"
```

This allows auditing and training with event history.

---

# 21. Deleting messages

A message can be deleted.

Conceptual types:

```text
DELETED_BY_AUTHOR
DELETED_BY_ADMIN
```

After deletion, the average user can see:

```text
[Message removed]
```

or simply stop viewing the content.

A deleted message cannot be reappeared due to:

- retry;
- delayed event;
- reconnection;
- reprocessing;
- outdated cache.

---

# 22. Reactions

Users can react to messages.

Examples:

```text
👍
❤️
😂
🚀
```

The same person cannot have two identical reactions to the same message.

Invalid example:

```text
User 10
Message 100

👍
👍
👍
```

The correct state is a single reaction of that type.

---

# 23. Competition in reactions

Popular posts can receive many reactions simultaneously.

Example:

```text
50,000 reactions in 10 seconds
```

Counters must remain correct.

Example:

```text
👍 31.028
❤️ 13.283
🚀 5.689
```

The system must avoid:

- duplicity;
- loss of reactions;
- negative counters;
- counters incompatible with the real state.

---

# 24. Threads

A message can start a thread.

Example:

```text
Main message
   ├─ resposta A
   ├─ resposta B
   └─ resposta C
```

The main context can display:

```text
42 responses
last reply 10 seconds
```

without necessarily transmitting all responses to all customers.

---

# 25. Mentions

Messages can mention users or groups.

Examples:

```text
@douglas
@backend
@everyone
```

Mentions can generate notifications.

A mention to a large number of users cannot prevent confirmation of the original message.

---

# 26. Durable Events

Some events represent real changes to the domain and cannot be silently lost.

Examples:

```text
MESSAGE_CREATED
MESSAGE_EDITED
MESSAGE_DELETED
REACTION_ADDED
REACTION_REMOVED
MEMBER_ADDED
MEMBER_REMOVED
CHANNEL_CREATED
CHANNEL_DELETED
```

These events need to be able to be retrieved or reprocessed later.

---

# 27. Ephemeral events

Other events have value only in the moment.

Examples:

```text
TYPING_STARTED
TYPING_STOPPED
USER_ONLINE
USER_AWAY
USER_OFFLINE
```

The occasional loss of these events does not compromise the integrity of the system.

They do not necessarily need to be stored historically.

---

# 28. Typing indicators

When a user starts typing:

```text
Douglas is typing...
```

other relevant participants can receive this information.

The event quickly ceases to be important.

There should be no requirement to later recover this historical state.

---

# 29. Presence

Users may appear as:

```text
ONLINE
AWAY
OFFLINE
```

Attendance should reflect recent activity and connected sessions.

If a user has an active session, they can still be considered online.

Presence is eventual and does not need to be transactionally consistent.

---

# 30. Fanout

A message may need to be delivered to few or many customers.

Simple example:

```text
DM

2 participantes
```

Extreme example:

```text
#global

500.000 members
100.000 connected users
```

A single message may need to reach tens of thousands of connections.

The system must support both extremes.

---

# 31. Celebrity Channel

The environment must purposely contain an extremely large channel.

Example:

```text
#global
```

Possible features:

```text
500.000 members
100.000 conectados simultaneamente
40% of platform reads
```

This channel should generate natural hotspots.

---

# 32. Whale Workspace

There must also be a workspace much larger than the others.

Example:

```text
MegaCorp
```

With:

```text
1.000.000 users
```

While others may possess:

```text
20 users
100 users
1.000 users
5.000 users
```

MegaCorp can represent:

```text
60% of traffic total
```

Even so, other workspaces must continue to operate.

---

# 33. Backpressure

Not all clients process events at the same speed.

Example:

```text
Client A
1.000 events/s

Client B
5 events/s
```

The slow client cannot cause unlimited memory growth or harm fast clients.

Ephemeral events can lose relevance and be discarded.

Durable events must remain recoverable later.

---

# 34. Reconnection

Customers may lose connectivity.

Example:

```text
Client disconnects for 20 seconds.
```

During this period, the following were created:

```text
Message 100
Message 101
Message 102
Message 103
```

When reconnecting, the client needs to be able to identify and recover the state they lost.

The system cannot rely exclusively on real-time delivery.

---

# 35. Recovery after reconnection

After returning, the client should be able to converge to the current state.

This includes:

- new messages;
- edited messages;
- deleted messages;
- new reactions;
- removal of channels;
- change of membership;
- access revocation.

---

# 36. Message delivery

A message can have conceptual states from the point of view of each recipient.

Example:

```text
SENT
DELIVERED
READ
```

These states are relative to the participant.

Example:

```text
Message 100

Douglas → READ
Maria   → READ
John    → DELIVERED
Pedro   → SENT
```

There is not necessarily a single global delivery status.

---

# 37. Read positions

In large conversations, it is not necessary to individually register a read mark for each message.

There may be a concept of reading position.

Example:

```text
last_read_message = 93829
```

Then:

```text
<= 93829
lida

> 93829
unread
```

The position should never regress incorrectly.

---

# 38. Unread counters

The interface may present:

```text
#backend        12
#geral           3
John             1
Incidentes      98
```

These counters must update quickly.

They can have eventual consistency, as long as they converge to the correct value.

---

# 39. Notifications

The system can generate notifications when:

- user is mentioned;
- receive direct message;
- monitored thread receives response;
- is added to channel;
- receives relevant administrative action.

Temporary failure of notification processing should not prevent message creation.

---

# 40. Search

Users can search for messages using:

```text
termo
autor
canal
conversa
data
workspace
```

A workspace can have hundreds of millions of messages.

Search does not need to be a critical part of the submission path.

If search is unavailable, messaging should continue to work.

---

# 41. History

A conversation can have millions of messages.

The user must navigate:

```text
messages recent
 ↓
messages older
 ↓
messages ainda older
```

While new messages continue to be created.

Navigation should not duplicate or skip messages just because new records were added.

---

# 42. Independent Messages and Consumers

A successfully created message can feed multiple streams.

Example:

```text
MESSAGE_CREATED
      |
      ├─ realtime
      ├─ notifications
      ├─ unread counters
      ├─ search
      ├─ analytics
      └─ audit
```

These flows have different criticalities.

Failure in analytics cannot invalidate the message.

Failure to search cannot prevent delivery.

---

# 43. Reprocessing

A processor may be unavailable for a certain period of time.

Example:

```text
Analytics unavailable for 1 hora.
```

After returning, you should be able to rebuild:

```text
messages/minute
users ativos
reactions/minute
channels ativos
```

using historical events.

Reprocessing cannot repeat business effects.

---

# 44. Rate limiting

Limits may exist by:

- workspace;
- user;
- session;
- operation;
- channel.

Examples:

```text
messages/minute
reactions/minute
reconnections/minute
channel creation/minute
buscas/minuto
```

A client exceeding limits must be limited without compromising the entire platform.

---

# 45. Spam and abuse

Customers may behave abusively.

Examples:

- send messages in a loop;
- open many connections;
- continually reconnect;
- generate high frequency reactions;
- perform repetitive searches;
- try to access channels without authorization;
- send invalid payloads.

These behaviors must primarily affect the person responsible.

---

# 46. Workspace administration

Admins can:

- create channels;
- delete channels;
- make channels private;
- add users;
- remove users;
- change roles;
- delete messages;
- block members;
- view audit.

Critical administrative actions need to be auditable.

---

# 47. User blocking

When a user is blocked within a workspace:

```text
Membership = BLOCKED
```

he should lose access quickly.

This includes already connected sessions.

The system must prevent:

- new messages;
- new readings;
- new reactions;
- new real-time events from that workspace.

---

# 48. Session revocation

It should also be possible to revoke a specific session or all sessions for a user.

Revoked sessions cannot continue to operate indefinitely.

---

# 49. Audit logs

Important administrative actions must generate records.

Example:

```text
15:42:19

actor:
User 100

workspace:
MegaCorp

action:
DELETE_MESSAGE

message:
938292
```

The audit must identify:

- who performed it;
- in which workspace;
- what action;
- which entity;
- when it occurred.

Regular users cannot edit audit records.

---

# 50. Strong consistency

Some operations require strong coherence from the user's point of view.

Examples:

```text
newly sent message
deleted message
membership removida
blocked user
canal privado removido
```

After a critical operation is committed, the user should not immediately observe a previous incompatible state.

---

# 51. Eventual consistency

Other data may converge with a small delay.

Examples:

```text
online users
contadores
analytics
rankings
statistics
```

Small temporary divergences are acceptable.

---

# 52. Hotspots

Traffic distribution must be intentionally uneven.

Example:

```text
Canal A
4 users

Canal B
30 users

Canal C
400 users

Canal D
5.000 users

#global
500.000 users
```

The same goes for workspaces.

---

# 53. Hot data

Some data is accessed much more than others.

Examples:

- workspace configuration;
- memberships;
- permissions;
- user data;
- last accessed channels;
- recent messages;
- unread counters;
- presence.

The system must consider this asymmetry as natural behavior.

---

# 54. Channel with high writing

In addition to channels with high reading, there must be a channel that receives a large number of messages.

Example:

```text
#live-event

10.000 messages/minute
```

This scenario should coexist with small talk.

---

# 55. Viral message

A message in a large channel can receive:

```text
50.000 reactions
20.000 responses
```

in a few seconds.

The other channels should not stop working because of this.

---

# 56. Workspace Dashboard

Admins can view operational indicators.

Examples:

```text
online users
messages/s
channels ativos
reactions/s
novos members
connections
```

These indicators may have eventual consistency.

---

# 57. Global Dashboard

Platform administrators can view:

```text
workspaces ativos
connected users
messages/s
events/s
connections
erros
backlog
```

Ordinary tenants cannot access global platform information.

---

# 58. Partial failure

Not all components have the same criticality.

Example:

## Analytics unavailable

```text
messages continue
```

## Search unavailable

```text
messages continue
```

## Presence unavailable

```text
messages continue
```

## Notifications unavailable

```text
messages continue
```

## Message persistence unavailable

```text
the system cannot claim that the message was created
```

---

# 59. Operating modes

The system may have operational states.

## NORMAL

Everything available.

## DEGRADED

Messages continue, but secondary functions may be delayed.

## READ_ONLY

New messages cannot be securely accepted, but existing history may remain available.

## CRITICAL

Not even reliable reading can be guaranteed.

The interface must reflect the actual state of the platform.

---

# 60. Peak Scenario — All Hands

A large internal event can generate a huge burden.

Example:

```text
20:00
Company All Hands starts.
```

In a few seconds:

```text
100.000 users connect
```

After:

```text
CEO sends message no #global
```

Then:

```text
100.000 clients receive the message
40.000 reagem
20,000 start typing
5.000 respondem
```

The system should continue to process small conversations normally.

---

# 61. Mass reconnection scenario

During the event:

```text
an instance disappears
```

and:

```text
20,000 clients disconnect
```

A few seconds later:

```text
20,000 clients attempt to reconnect
```

While new messages continue to be created.

The system needs to avoid turning the reconnection into a second, larger failure.

---

# 62. Slow client scenario

During a very active channel:

```text
Client A
processa 500 events/s

Client B
processa 2 events/s
```

Customer B cannot force the system to maintain an infinite queue.

Ephemeral events can be discarded.

Durable events must remain recoverable after synchronization.

---

# 63. Out of order event scenario

Consider:

```text
1 MESSAGE_CREATED
2 MESSAGE_EDITED
3 MESSAGE_DELETED
```

A consumer may temporarily observe:

```text
1
3
2
```

Even so, the end state must continue:

```text
DELETED
```

and no:

```text
EDITED
```

---

# 64. Retry scenario

A customer sends:

```text
MESSAGE_CREATE
client_message_id = ABC
```

The server processes the operation, but the response does not arrive.

The client repeats:

```text
ABC
ABC
ABC
ABC
```

The system must continue to have exactly one message associated with the operation.

---

# 65. Reprocessing scenario

Analytics are unavailable for a period of time.

It is then reactivated and reprocesses old events.

The system must rebuild statistics without:

- create duplicate messages;
- repeat notifications incorrectly;
- duplicate reactions;
- changing final states incorrectly.

---

# 66. Whale workspace scenario

During a spike:

```text
MegaCorp = 60% of traffic
```

At the same time:

```text
Workspace A = 2%
Workspace B = 1%
Workspace C = 0.5%
```

MegaCorp cannot prevent small workspaces from sending and receiving messages.

---

# 67. Celebrity Channel Scenario

```text
#global
500.000 members
100.000 online
```

A message is sent.

In the next few seconds:

```text
100.000 real-time deliveries
50.000 reactions
10.000 responses
thousands of unread updates
```

The remainder of the platform must remain operational.

---

# 68. Chaos scenario

During a spike:

```text
a component becomes slow
```

After:

```text
a processor stops
```

After:

```text
an instance disappears
```

After:

```text
thousands of clients reconnect
```

While:

```text
messages continue arriving
```

Business rules must continue to be respected.

---

# 69. Fundamental invariants

These rules represent the laws of Messager.

If any of these are broken, the system must be considered incorrect.

1. The same idempotent operation can never create two messages.
2. All clients must eventually converge on the same official message order.
3. A deleted message cannot reappear due to old event.
4. An old version can never overwrite a newer version.
5. A workspace can never access data from another workspace without authorization.
6. A user removed from a private channel cannot continue receiving new messages from that channel.
7. A blocked user should quickly lose access.
8. Durable events cannot disappear silently.
9. Ephemeral events can be discarded without compromising the domain.
10. A slow customer cannot harm fast customers.
11. Reprocessing cannot duplicate business effects.
12. The same reaction from the same user to the same message cannot exist twice.
13. Derived counters must eventually converge to the correct state.
14. Minor failures must not prevent message creation.
15. Confirmed messages should remain retrievable after reconnection.
16. A giant workspace cannot monopolize all resources indefinitely.
17. A reading position cannot regress incorrectly.
18. A revoked session cannot continue to operate indefinitely.
19. Users without permission cannot access private channels.
20. The official ordering of a conversation must remain deterministic.
21. An old event cannot replace a newer incompatible state.
22. An analytics failure cannot invalidate existing messages.
23. A search failure cannot prevent sending.
24. A critical administrative operation must be auditable.
25. A message should never be confirmed to the client if its creation has not actually been accepted.

---# 70. Initial conceptual model

The data core can be conceptually understood as:

```text
Users
  |
  +---- UserSessions
  |
  +---- WorkspaceMemberships
               |
               v
           Workspaces
               |
        +------+------+
        |             |
     Channels     Conversations
        |             |
        +------ Messages
                  |
          +-------+--------+
          |       |        |
      Reactions Versions Threads
          |
      ReadPositions
```

Other important conceptual entities:

```text
ChannelMemberships
ConversationMembers
Notifications
AuditLogs
MessageDeliveryState
```

---

# 71. Main entities

##Users

Global identity.

## UserSessions

Active sessions and devices.

## Workspaces

Platform Tenants.

## WorkspaceMemberships

Relationship between user and workspace.

##Channels

Public and private channels.

## ChannelMemberships

Participation in private channels.

##Conversations

Logical message context.

## ConversationMembers

Participants in private conversations.

## Messages

Messages sent.

## MessageVersions

Change history.

##Reactions

Reactions per user.

## ReadPositions

Last position read by participant.

## Notifications

Notifications generated by events.

## AuditLogs

Administrative audit.

---

# 72. Conceptual scale

The domain must assume that the platform can reach:

```text
millions of users
millions of sessions
hundreds of thousands of channels
hundreds of millions of messages
billions of events
hundreds of thousands of simultaneous connections
```

It is not necessary to maintain this volume permanently during development.

The objective is that the rules do not assume a small system.

---

# 73. Expected load distribution

The distribution should not be uniform.

Example:

```text
90% of channels
fewer than 50 users

9% of channels
50 to 5,000 users

0.9% of channels
5,000 to 100,000 users

0.1% of channels
more than 100.000 users
```

Likewise, a few workspaces can account for a large portion of your traffic.

---

#74. Architectural Problems Exercised

| Problem | Demonstration at Messager |
|---|---|
| Many connections | customers connected in real time |
| Bank pool | high volume of messages and sessions |
| N+1 | channels, members, messages and reactions |
| Cache | memberships, channels, users and unread |
| Thundering herd | reconnections and giant channels |
| Distributed session | multiple devices |
| Balancing | persistent connections |
| Fanout | messages in large channels |
| Backpressure | slow customers |
| Idempotence | sending and retries |
| Replication lag | reading after sending |
| Containment | reactions, counters, memberships |
| Hotspots | #global and MegaCorp |
| Status + events | message creation |
| Cascade failure | search, notifications, presence |
| Restart during traffic | connections and reconnections |
| Flood | spam and reconnect storms |
| Observability | journey of a message |
| Replay | analytics and reconstruction |
| Eventual consistency | presence, counters and dashboards |
| Strong consistency | upload, deletion and permissions |
| Ordering | official thread by conversation |

---

# 75. Completion criteria

The project should not be considered completed just because users are able to exchange messages.

It must continue to respect the invariants when simultaneously subjected to:

- many connections;
- high message rate;
- giant channels;
- giant workspaces;
- slow clients;
- duplicate events;
- retries;
- bulk reconnections;
- events out of order;
- unavailable components;
- late workers;
- reprocessing;
- spam;
- permission changes;
- locks;
- concurrent editing and deletion;
- high rate of reactions.

The ultimate goal is to have a relatively simple system from a functional point of view, but capable of reproducing real problems of scalability, concurrency and distributed architecture.